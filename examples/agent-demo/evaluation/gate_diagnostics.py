"""Controlled evaluator fixtures. Never written as measured application runs."""

import copy

from .comparison import compare
from .gates import decide


def diagnostics(actual):
    baseline = copy.deepcopy(actual)
    baseline["run_id"] = "controlled-fixture-baseline"
    baseline["configuration"].pop("manifest", None)
    baseline["configuration"]["case_ids"] = ["protected", "improve-a", "improve-b"]
    baseline["results"] = [
        {
            "case_id": key,
            "status": "succeeded",
            "metrics": {"correctness": {"status": "scored", "value": value}},
            "judgments": {},
            "latency_ms": 1,
        }
        for key, value in zip(baseline["configuration"]["case_ids"], [1, 0, 0], strict=True)
    ]
    candidate = copy.deepcopy(baseline)
    candidate["run_id"] = "controlled-fixture-candidate"
    for r, v in zip(candidate["results"], [0, 1, 1], strict=True):
        r["metrics"]["correctness"]["value"] = v
    policy = {
        "policy_id": "controlled-critical",
        "version": "v1",
        "critical_cases": ["protected"],
        "scope": "CONTROLLED GATE TEST; NOT MEASURED APPLICATION EVIDENCE",
        "rules": [{"id": "critical", "op": "no_critical_regression", "mandatory": True}],
    }
    comparison = compare(baseline, candidate)
    decision = decide(comparison, policy)
    assert (
        comparison["metrics"]["correctness"]["absolute_delta"] > 0 and decision["outcome"] == "FAIL"
    )
    incompatible = copy.deepcopy(candidate)
    incompatible["configuration"]["dataset_checksum"] = "f" * 64
    invalid = compare(baseline, incompatible)
    invalid_decision = decide(invalid, policy)
    assert invalid_decision["outcome"] == "INCONCLUSIVE"
    return {
        "provenance": "CONTROLLED TEST FIXTURES, not persisted as measured runs or linked to fabricated traces",
        "critical_despite_mean_improvement": {"comparison": comparison, "decision": decision},
        "incompatible": {"comparison": invalid, "decision": invalid_decision},
    }
