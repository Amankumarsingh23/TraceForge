"""Explicit offline diagnostic suite. No cross-run promotion or deployment gates."""

import asyncio
import json
from pathlib import Path

from .runner import FAULTS, run


async def experiments(directory):
    directory.mkdir(parents=True, exist_ok=True)
    reports = {}

    async def execute(name, **options):
        report = await run(**options)
        (directory / f"{name}.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
        reports[name] = report
        print(f"{name}: {report['run_id']} ({len(report['results'])} cases)", flush=True)
        return report

    baseline = await execute("baseline", k=3, context_limit=2)
    await execute("k1", k=1, context_limit=2)
    await execute("k5", k=5, context_limit=2)
    repeat = await execute("repeat", k=3, context_limit=2)

    def deterministic(report):
        return [
            {
                k: r[k]
                for k in (
                    "case_id",
                    "status",
                    "metrics",
                    "judgments",
                    "retrieved_document_ids",
                    "context_document_ids",
                    "supplied_document_hashes",
                    "evaluated_output",
                )
            }
            for r in report["results"]
        ]

    assert deterministic(baseline) == deterministic(repeat), "deterministic case results changed"
    assert (
        baseline["configuration"]["configuration_checksum"]
        == repeat["configuration"]["configuration_checksum"]
    )
    assert baseline["run_id"] != repeat["run_id"]
    assert not {r["trace_id"] for r in baseline["results"]} & {
        r["trace_id"] for r in repeat["results"]
    }
    context = await execute("context_loss", k=5, context_limit=1, case_id="support-030")
    c = context["results"][0]["metrics"]
    assert c["recall"]["value"] > c["context_coverage"]["value"], (
        "expected actual context truncation loss"
    )
    control = await execute("fault_control", k=5, context_limit=3, case_id="support-027")
    control_metrics = control["results"][0]["metrics"]
    assert (
        control_metrics["correctness"]["value"] == 1
        and control_metrics["required_fact_coverage"]["value"] == 1
    )
    for fault in FAULTS:
        report = await execute(fault, k=5, context_limit=3, case_id="support-027", fault=fault)
        result = report["results"][0]
        if fault == "incorrect_fact":
            assert result["judgments"]["contradictions"] > 0
        if fault == "unsupported_claim":
            assert result["metrics"]["groundedness"]["value"] < 1
        if fault == "missing_fact":
            assert result["metrics"]["required_fact_coverage"]["value"] < 1
        if fault in {"wrong_citation", "invalid_citation"}:
            assert result["metrics"]["citation_correctness"]["value"] < 1
        if fault == "missing_citation":
            assert result["metrics"]["citation_coverage"]["value"] < 1
        if fault == "inappropriate_abstention":
            assert result["metrics"]["abstention"]["value"] == 0
    failure = await execute("execution_failure", case_id="support-001", execution_failure="timeout")
    assert failure["results"][0]["status"] == "execution_failed"
    assert (
        failure["aggregates"]["recall"]["value"] is None
        and failure["aggregates"]["recall"]["coverage"] == 0
    )
    pending = await execute("trace_pending", case_id="support-001", trace_unavailable=True)
    assert (
        pending["results"][0]["trace_visibility"] == "pending" and pending["results"][0]["trace_id"]
    )
    summary = {
        name: {
            "run_id": report["run_id"],
            "configuration_checksum": report["configuration"]["configuration_checksum"],
            "aggregates": report["aggregates"],
        }
        for name, report in reports.items()
    }
    summary["reproducibility"] = {
        "identical_deterministic_results": True,
        "distinct_run_and_trace_ids": True,
        "baseline_cases": len(baseline["results"]),
    }
    (directory / "experiments.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")
    return summary


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=Path("../../artifacts/session08"))
    args = parser.parse_args()
    asyncio.run(experiments(args.output))
