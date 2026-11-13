"""Actual offline experiments, followed by separate controlled gate diagnostics."""

import asyncio
import copy
import json
from pathlib import Path

from .compare_cli import persist, read_run
from .comparison import compare
from .dataset import ROOT, load
from .gate_diagnostics import diagnostics
from .runner import run
from .store import Store


async def execute(directory):
    directory.mkdir(parents=True, exist_ok=True)
    runs = {}
    for name, options in (
        ("baseline", {}),
        ("repeat", {}),
        ("context1", {"context_limit": 1}),
        ("k1", {"k": 1}),
        ("missing_usage", {"execution_failure": "missing_usage"}),
    ):
        runs[name] = await run(
            k=options.pop("k", 3),
            metric_k=3,
            context_limit=options.pop("context_limit", 2),
            **options,
        )
        (directory / f"{name}.json").write_text(json.dumps(runs[name], indent=2))
        print(name, runs[name]["run_id"], flush=True)
    offline = json.loads((ROOT / "policies/offline-v1.json").read_text())
    live = json.loads((ROOT / "policies/live-v1.json").read_text())
    strict = copy.deepcopy(offline)
    strict["version"] = "v2-strict"
    strict["rules"][1]["threshold"] = "0.99"
    cost = {
        "policy_id": "cost-evidence",
        "version": "v1",
        "scope": "offline fixture cost coverage only",
        "rules": [
            {
                "id": "complete-price",
                "op": "max",
                "metric": "cost_nano_usd",
                "unit": "nano-USD",
                "threshold": "10000000",
                "mandatory": True,
            }
        ],
    }
    store = Store()
    store.migrate()
    reports = {}
    try:
        for name, candidate, policy in (
            ("identical", "repeat", offline),
            ("context_regression", "context1", offline),
            ("retrieval_change", "k1", offline),
            ("policy_v2", "repeat", strict),
            ("production_profile", "repeat", live),
            ("missing_cost", "missing_usage", cost),
        ):
            report = persist(
                store,
                compare(
                    read_run(store, runs["baseline"]["run_id"]),
                    read_run(store, runs[candidate]["run_id"]),
                ),
                policy,
            )
            reports[name] = report
            (directory / f"{name}.json").write_text(json.dumps(report, indent=2))
            assert (
                persist(store, report["comparison"], policy)["decision_id"] == report["decision_id"]
            )
        assert reports["identical"]["decision"]["outcome"] == "PASS"
        assert reports["context_regression"]["decision"]["outcome"] == "FAIL"
        assert reports["policy_v2"]["decision"]["outcome"] == "FAIL"
        assert reports["production_profile"]["decision"]["outcome"] == "INCONCLUSIVE"
        assert reports["missing_cost"]["decision"]["outcome"] == "INCONCLUSIVE"
        assert all(
            reports["identical"]["comparison"]["metrics"][k]["absolute_delta"] == 0
            for k in ("recall", "correctness", "rr", "groundedness")
        )
        historical = directory.parent / "session08/baseline.json"
        if historical.exists():
            old_id = json.loads(historical.read_text())["run_id"]
            legacy = persist(
                store,
                compare(read_run(store, old_id), read_run(store, runs["baseline"]["run_id"])),
                offline,
            )
            reports["legacy_reuse"] = legacy
            (directory / "legacy_reuse.json").write_text(json.dumps(legacy, indent=2))
        dataset, _, _ = load()
        labels = {c["case_id"]: c for c in dataset["cases"]}
        failures = [
            {
                "case_id": r["case_id"],
                "expected_facts": labels[r["case_id"]]["expected_facts"],
                "relevant_ids": labels[r["case_id"]]["relevant_document_ids"],
                "retrieved_ids": r["retrieved_document_ids"],
                "supplied_ids": r["context_document_ids"],
                "output": r["evaluated_output"],
                "metrics": r["metrics"],
            }
            for r in runs["baseline"]["results"]
            if r["metrics"]["correctness"]["value"] == 0
        ]
        summary = {
            name: {
                "comparison_id": r["comparison_id"],
                "decision_id": r["decision_id"],
                "outcome": r["decision"]["outcome"],
            }
            for name, r in reports.items()
        }
        summary["run_ids"] = {k: v["run_id"] for k, v in runs.items()}
        summary["baseline_failures"] = failures
        (directory / "gate-fixtures.json").write_text(
            json.dumps(diagnostics(runs["baseline"]), indent=2)
        )
        (directory / "evidence.json").write_text(json.dumps(summary, indent=2))
        print(json.dumps(summary), flush=True)
    finally:
        store.close()


if __name__ == "__main__":
    import argparse

    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, default=Path("../../artifacts/session09"))
    args = parser.parse_args()
    asyncio.run(execute(args.output))
