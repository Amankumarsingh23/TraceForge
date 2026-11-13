import json

import pytest

from evaluation.comparison import bootstrap, compare, quantile
from evaluation.dataset import ROOT
from evaluation.gates import decide, validate
from evaluation.manifest import freeze


def run(values=(1, 0, 1), mode="fixture"):
    config = {
        "dataset_checksum": "a" * 64,
        "corpus_checksum": "b" * 64,
        "source_checksum": "c" * 64,
        "dataset_version": "v1",
        "evaluator_version": "rubric-v1",
        "evaluator_type": "exact",
        "mode": mode,
        "k": 3,
        "metric_k": 3,
        "context_limit": 2,
        "case_ids": [f"c{i}" for i in range(len(values))],
        "pricing_version": "hypothetical-v1",
        "pricing_checksum": "d" * 64,
        "currency": "USD",
    }
    return {
        "run_id": "1" * 32,
        "configuration": config,
        "status": "completed",
        "results": [
            {
                "case_id": f"c{i}",
                "status": "succeeded",
                "metrics": {"correctness": {"value": v, "status": "scored"}},
                "latency_ms": i + 1,
                "estimated_cost": {"nano_usd": "100", "coverage": "complete_for_observed_attempts"},
                "judgments": {"unsupported_claims": 0, "contradictions": 0},
            }
            for i, v in enumerate(values)
        ],
    }


def policy(op="max_degradation", threshold="0", metric="correctness", unit="fraction"):
    return {
        "policy_id": "test",
        "version": "v1",
        "rules": [
            {
                "id": "r1",
                "op": op,
                "metric": metric,
                "threshold": threshold,
                "unit": unit,
                "mandatory": True,
            }
        ],
    }


def test_canonical_manifest_independence():
    c = run()["configuration"]
    m = freeze(c)
    assert freeze(dict(reversed(list(c.items())))) == m
    c["case_ids"].reverse()
    assert freeze(c) == m
    c["context_limit"] = 1
    assert freeze(c)["checksum"] != m["checksum"]
    c["password"] = "SECRET"
    with pytest.raises(ValueError):
        freeze(c)


def test_pair_by_id_and_transitions():
    a = run()
    b = run((0, 1, 1))
    b["results"].reverse()
    result = compare(a, b)
    assert result["metrics"]["correctness"]["absolute_delta"] == 0
    assert result["transitions"] == {
        "both_correct": 1,
        "both_incorrect": 0,
        "improved": 1,
        "regressed": 1,
        "missing_ineligible": 0,
    }
    assert result["metrics"]["cost_nano_usd"]["baseline_total_nano_usd"] == "300"


@pytest.mark.parametrize("mutation", ["missing", "duplicate", "dataset", "mode", "incomplete"])
def test_incompatibility(mutation):
    a, b = run(), run()
    if mutation == "missing":
        b["results"].pop()
    if mutation == "duplicate":
        b["results"].append(b["results"][0])
    if mutation == "dataset":
        b["configuration"]["dataset_checksum"] = "e" * 64
    if mutation == "mode":
        b["configuration"]["mode"] = "live"
    if mutation == "incomplete":
        b["status"] = "running"
    c = compare(a, b)
    assert not c["compatible"] and decide(c, policy())["outcome"] == "INCONCLUSIVE"


def test_missing_cost_and_zero_baseline():
    a, b = run(), run()
    b["results"][0]["estimated_cost"] = None
    c = compare(a, b)
    assert c["metrics"]["cost_nano_usd"]["candidate_total_nano_usd"] is None
    assert (
        decide(c, policy("max", "1000", "cost_nano_usd", "nano-USD"))["outcome"] == "INCONCLUSIVE"
    )
    a = run((0, 0, 0))
    b = run((1, 1, 1))
    assert compare(a, b)["metrics"]["correctness"]["relative_delta"] is None


def test_cutoffs_only_invalidate_retrieval_and_price_only_cost():
    a, b = run(), run()
    b["configuration"]["metric_k"] = 5
    b["configuration"]["pricing_checksum"] = "e" * 64
    c = compare(a, b)
    assert c["compatible"]
    assert (
        c["metrics"]["recall"]["status"] == "incompatible"
        and c["metrics"]["correctness"]["status"] == "scored"
    )
    assert c["metrics"]["cost_nano_usd"]["status"] == "incompatible"


def test_failed_candidate_is_retained_not_zero_quality():
    a, b = run(), run()
    b["results"][0]["status"] = "execution_failed"
    b["results"][0]["metrics"] = {}
    b["results"][0]["estimated_cost"] = None
    c = compare(a, b)
    assert c["metrics"]["correctness"]["matched"] == 2
    assert c["metrics"]["correctness"]["candidate_counts"]["coverage"] == pytest.approx(2 / 3)
    assert c["metrics"]["execution_success"]["absolute_delta"] == pytest.approx(-1 / 3)
    assert c["cases"][0]["execution_transition"] == "new_failure"


def test_quantile_and_seeded_paired_bootstrap():
    assert quantile([0, 10], 0.95) == 9.5 and quantile([], 0.95) is None
    assert bootstrap([1, 2])["status"] == "insufficient_samples"
    assert bootstrap([0] * 10)["interval"] == [0, 0]
    assert bootstrap([-1, 0, 0, 1, 1] * 3) == bootstrap([-1, 0, 0, 1, 1] * 3)


def test_gate_precedence_critical_regression_despite_average_improvement():
    a, b = run((1, 0, 0)), run((0, 1, 1))
    c = compare(a, b)
    p = {
        "policy_id": "critical-test",
        "version": "v1",
        "critical_cases": ["c0"],
        "rules": [
            {"id": "protected", "op": "no_critical_regression", "mandatory": True},
            {"id": "live", "op": "require_mode", "threshold": "live", "mandatory": True},
        ],
    }
    assert c["metrics"]["correctness"]["absolute_delta"] > 0
    assert decide(c, p)["outcome"] == "FAIL"  # confirmed failure precedes missing live evidence
    assert decide(c, {**p, "rules": [p["rules"][1]]})["outcome"] == "INCONCLUSIVE"
    assert decide(compare(a, a), policy())["outcome"] == "PASS"


@pytest.mark.parametrize(
    "change",
    [
        {"op": "execute_shell"},
        {"metric": "unknown"},
        {"unit": "USD"},
        {"threshold": "NaN"},
        {"threshold": "-1"},
        {"threshold": "1.1"},
    ],
)
def test_policy_validation(change):
    p = policy()
    p["rules"][0].update(change)
    with pytest.raises(ValueError):
        validate(p)
    with pytest.raises(ValueError):
        validate({"rules": []})


def test_all_profiles_valid_and_production_requires_live():
    for path in (ROOT / "policies").glob("*.json"):
        validate(json.loads(path.read_text()))
    p = json.loads((ROOT / "policies/live-v1.json").read_text())
    assert decide(compare(run(), run()), p)["outcome"] == "INCONCLUSIVE"


def test_tampered_manifest_and_evidence_only_gates():
    a, b = run(), run()
    a["configuration"]["manifest"] = freeze(a["configuration"])
    b["configuration"]["manifest"] = freeze(b["configuration"])
    b["configuration"]["context_limit"] = 1
    assert "manifest_snapshot_mismatch" in compare(a, b)["compatibility_errors"]
    a, b = run(), run()
    b["results"][0]["metrics"] = {}
    assert decide(compare(a, b), policy("min_coverage", "1"))["outcome"] == "INCONCLUSIVE"
    c = compare(run((1, 1, 1)), run((0, 1, 1)))
    assert decide(c, policy("max_relative_degradation", "0.1"))["outcome"] == "FAIL"
    c["metrics"]["correctness"]["candidate"] = 1
    assert decide(c, policy("min", "1"))["outcome"] == "PASS"
