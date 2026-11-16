import argparse
import asyncio
import copy
import hashlib
import json
import platform
import subprocess
import time
import uuid

import httpx

from agent_demo.constrained import render
from agent_demo.retrieval import DATA, PROMPT_VERSION, RETRIEVER_VERSION

from .dataset import ROOT, checksum, load
from .manifest import freeze
from .metrics import aggregate, retrieval, score
from .scoring import QUALITY, VERSION, evaluate
from .store import Store

FAULTS = (
    "incorrect_fact",
    "unsupported_claim",
    "missing_fact",
    "wrong_citation",
    "invalid_citation",
    "missing_citation",
    "inappropriate_abstention",
)


def transform(output, fault):
    output = copy.deepcopy(output)
    if not output or not output["claims"]:
        raise ValueError("fault diagnostic needs a nonempty constrained answer")
    claims = output["claims"]
    if fault == "incorrect_fact":
        claims[0]["value"] = "NOT " + claims[0]["value"]
    elif fault == "unsupported_claim":
        claims.append(
            {
                "key": "invented_guarantee",
                "value": "Refunds always arrive within 24 hours",
                "citations": ["D01"],
            }
        )
    elif fault == "missing_fact":
        claims.pop()
    elif fault == "wrong_citation":
        claims[0]["citations"] = ["D12"]
    elif fault == "invalid_citation":
        claims[0]["citations"] = ["D99"]
    elif fault == "missing_citation":
        claims[0]["citations"] = []
    elif fault == "inappropriate_abstention":
        claims.clear()
    else:
        raise ValueError("unknown diagnostic")
    output["abstained"] = not claims
    output["answer"] = render(claims, output["abstained"])
    return output


def failure_metrics(case):
    metrics, _ = retrieval([], [], case["relevant_document_ids"], 3)
    for name in QUALITY:
        metrics[name] = score(0, 0, "execution_failed")
    for name, m in metrics.items():
        if name not in QUALITY and case["relevant_document_ids"]:
            metrics[name] = score(0, 0, "execution_failed")
    return metrics


async def run(
    k=3,
    context_limit=2,
    case_id=None,
    fault=None,
    execution_failure=None,
    trace_unavailable=False,
    allow_live=False,
    allow_billable=False,
    metric_k=None,
):
    metric_k = k if metric_k is None else metric_k
    if type(metric_k) is not int or not 1 <= metric_k <= 5:
        raise ValueError("invalid evaluation cutoff")
    if type(k) is not int or not 1 <= k <= 5 or not 1 <= context_limit <= 3:
        raise ValueError("invalid execution bounds")
    dataset, corpus, digest = load()
    cases = [c for c in dataset["cases"] if case_id is None or c["case_id"] == case_id]
    if not cases:
        raise ValueError("unknown case")
    run_id = uuid.uuid4().hex
    results = []
    persisted = []
    async with httpx.AsyncClient(
        base_url="http://127.0.0.1:8084", timeout=25, trust_env=False
    ) as client:
        health = await client.get("/healthz")
        health.raise_for_status()
        mode = health.json()["mode"]
        if mode != "fixture" and not (allow_live and allow_billable):
            raise ValueError("live execution requires explicit live and billable opt-ins")
        if mode != "fixture" and (fault or execution_failure):
            raise ValueError("fixture diagnostics only")
        commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
        dirty = bool(
            subprocess.check_output(
                ["git", "status", "--porcelain", "--untracked-files=no"], cwd=ROOT, text=True
            ).strip()
        )
        sources = {
            str(p.relative_to(ROOT.parent)): hashlib.sha256(p.read_bytes()).hexdigest()
            for folder in (ROOT, ROOT.parent / "agent_demo")
            for p in sorted(folder.rglob("*.py"))
        }
        config = {
            "metric_k": metric_k,
            "measurement_version": "http-wall-ms-v1",
            "scoring_definition": "constrained-evidence-v1:macro-v1:precision-denominator-K",
            "pricing_version": "hypothetical-2026-11-13" if mode == "fixture" else "unknown",
            "currency": "USD" if mode == "fixture" else "unknown",
            "generation": {
                "max_completion_tokens": 256,
                "sampling": "provider_default",
                "tools": "read_only_allowlist; disabled in eval",
            },
            "runtime": {
                "python": platform.python_version(),
                "platform": platform.system(),
                "http_timeout_seconds": 25,
                "agent_deadline_seconds": 20,
                "warmup": "none",
                "latency_scope": "HTTP request only; scoring/export lookup excluded",
            },
            "provider": health.json().get("provider"),
            "requested_model": health.json().get("requested_model"),
            "pricing_checksum": hashlib.sha256((DATA / "pricing.json").read_bytes()).hexdigest(),
            "tool_data_checksum": hashlib.sha256((DATA / "orders.json").read_bytes()).hexdigest(),
            "dataset_version": dataset["version"],
            "dataset_checksum": digest,
            "corpus_version": corpus["version"],
            "corpus_checksum": checksum(corpus),
            "prompt_version": PROMPT_VERSION,
            "prompt_checksum": hashlib.sha256((DATA / "prompt-v2.txt").read_bytes()).hexdigest(),
            "retriever_version": RETRIEVER_VERSION,
            "evaluator_version": VERSION,
            "evaluator_type": "constrained_exact_evidence",
            "human_review": "independent_review_pending",
            "judge": "not_configured",
            "mode": mode,
            "git_commit": commit,
            "git_dirty": dirty,
            "source_checksum": checksum(sources),
            "k": k,
            "context_limit": context_limit,
            "concurrency": 1,
            "random_seed": None,
            "output_format": "structured",
            "case_ids": [c["case_id"] for c in cases],
            "output_transform": fault,
            "execution_failure": execution_failure,
            "trace_lookup_mode": "controlled_unavailable_loopback"
            if trace_unavailable
            else "normal",
            "max_attempts": 1,
            "max_model_calls": 1,
            "max_tool_calls": 0,
        }
        config["configuration_checksum"] = checksum(config)
        config["manifest"] = freeze(config)
        store = Store()
        store.migrate()
        store.start(dataset, digest, run_id, config)
        try:
            for case in cases:
                payload = {
                    "question": case["question"],
                    "top_k": k,
                    "context_limit": context_limit,
                    "output_format": "structured",
                    "evaluation_run_id": run_id,
                    "evaluation_case_id": case["case_id"],
                    "max_attempts": 1,
                    "max_model_calls": 1,
                    "max_tool_calls": 0,
                }
                if execution_failure:
                    payload["scenario"] = execution_failure
                started = time.perf_counter()
                result = {
                    "case_id": case["case_id"],
                    "status": "execution_failed",
                    "semantic_status": "execution_failed",
                    "metrics": failure_metrics(case),
                    "judgments": [],
                    "trace_visibility": "pending",
                    "error": "application_unavailable",
                    "retrieved_document_ids": [],
                    "context_document_ids": [],
                    "supplied_document_hashes": {},
                    "evaluated_output": None,
                    "original_output": None,
                    "output_transform": fault,
                    "usage": None,
                    "estimated_cost": None,
                }
                try:
                    response = await client.post("/ask", json=payload)
                    value = response.json()
                    result.update(
                        trace_id=value.get("trace_id"), application_run_id=value.get("run_id")
                    )
                    if response.status_code != 200:
                        result["error"] = value.get("error", {}).get("code", "application_failed")
                    else:
                        actual = value.get("structured_output")
                        transformed = transform(actual, fault) if fault else actual
                        content = json.dumps(transformed) if transformed else value["answer"]
                        supplied = value["context_document_ids"]
                        # Verify evidence hashes against the frozen actual corpus before scoring.
                        hashes = {
                            d["id"]: hashlib.sha256(d["text"].encode()).hexdigest()
                            for d in corpus["documents"]
                            if d["id"] in supplied
                        }
                        if hashes != value["supplied_document_hashes"]:
                            raise ValueError("supplied corpus hash mismatch")
                        metrics, anomalies = retrieval(
                            value["retrieved_document_ids"],
                            supplied,
                            case["relevant_document_ids"],
                            metric_k,
                        )
                        semantic, judgments, status = evaluate(
                            content,
                            supplied,
                            case,
                            dataset["facts"],
                            {d["id"] for d in corpus["documents"]},
                        )
                        metrics.update(semantic)
                        result.update(
                            status="succeeded",
                            semantic_status=status,
                            metrics=metrics,
                            judgments=judgments,
                            error=None,
                            retrieved_document_ids=value["retrieved_document_ids"],
                            context_document_ids=supplied,
                            supplied_document_hashes=hashes,
                            evaluated_output=transformed or value["answer"],
                            original_output=actual or value["answer"],
                            retrieval_anomalies=anomalies,
                            provider=value["provider"],
                            requested_model=value["requested_model"],
                            response_model=value["response_model"],
                        )
                except (httpx.HTTPError, ValueError, KeyError, TypeError):
                    result["error"] = "execution_or_contract_failed"
                result["latency_ms"] = (time.perf_counter() - started) * 1000
                results.append(result)
            # Export is asynchronous: bounded lookups enrich results without requiring a visible trace.
            async with httpx.AsyncClient(
                base_url="http://127.0.0.1:1" if trace_unavailable else "http://127.0.0.1:8082",
                timeout=3,
                trust_env=False,
            ) as api:
                await asyncio.sleep(1.2)
                for result in results:
                    if not result.get("trace_id"):
                        continue
                    try:
                        response = await api.get(
                            f"/api/v1/traces/{result['trace_id']}/agent-summary"
                        )
                        if response.status_code == 200:
                            runs = response.json()["runs"]
                            observed = next(
                                (r for r in runs if r["run_id"] == result["application_run_id"]),
                                None,
                            )
                            if observed:
                                result.update(
                                    trace_visibility="visible",
                                    usage={
                                        key: observed[key]
                                        for key in (
                                            "known_input_tokens",
                                            "known_output_tokens",
                                            "usage_coverage",
                                            "provider_attempts",
                                        )
                                    },
                                    estimated_cost={
                                        "nano_usd": observed["estimated_nano_usd"],
                                        "known_nano_usd": observed["known_estimated_nano_usd"],
                                        "coverage": observed["cost_coverage"],
                                        "mode": mode,
                                    },
                                )
                    except (httpx.HTTPError, ValueError, KeyError):
                        pass
                for case, result in zip(cases, results, strict=True):
                    store.save(run_id, dataset, case, result)
                    persisted.append(result)
            aggregates = aggregate(results)
            store.finish(run_id, aggregates)
            return {
                "run_id": run_id,
                "configuration": config,
                "aggregates": aggregates,
                "results": results,
            }
        except BaseException:
            store.finish(run_id, aggregate(persisted), "interrupted")
            raise
        finally:
            store.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--k", type=int, default=3)
    parser.add_argument("--context-limit", type=int, default=2)
    parser.add_argument("--metric-k", type=int)
    parser.add_argument("--case-id")
    parser.add_argument("--fault", choices=FAULTS)
    parser.add_argument("--execution-failure", choices=["timeout", "permanent_error"])
    parser.add_argument("--trace-unavailable", action="store_true")
    parser.add_argument("--allow-live", action="store_true")
    parser.add_argument("--allow-billable", action="store_true")
    parser.add_argument("--report")
    args = vars(parser.parse_args())
    report = args.pop("report")
    try:
        output = asyncio.run(run(**args))
    except Exception:
        raise SystemExit(
            "Evaluation failed; inspect service readiness/configuration. Credentials and raw exceptions are suppressed."
        ) from None
    if report:
        from pathlib import Path

        path = Path(report)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(output, indent=2), encoding="utf-8")
    print(
        json.dumps(
            {
                "run_id": output["run_id"],
                "cases": len(output["results"]),
                "aggregates": output["aggregates"],
            }
        )
    )


if __name__ == "__main__":
    main()
