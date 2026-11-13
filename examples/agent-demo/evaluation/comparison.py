"""Pure paired comparison of frozen observations; no provider/database calls."""

import math
import random
import statistics
from decimal import Decimal

from .manifest import freeze

VERSION = "paired-comparison-v1"
QUALITY = (
    "recall",
    "rr",
    "context_coverage",
    "context_retention",
    "correctness",
    "groundedness",
    "citation_correctness",
    "citation_coverage",
    "required_fact_coverage",
    "abstention",
)
UNITS = {
    **{key: "fraction" for key in QUALITY},
    "execution_success": "fraction",
    "latency_ms": "ms",
    "latency_p95_ms": "ms",
    "cost_nano_usd": "nano-USD",
}


def quantile(values, p):
    if not values:
        return None
    ordered = sorted(values)
    h = (len(ordered) - 1) * p
    i = int(h)
    return ordered[i] + (ordered[min(i + 1, len(ordered) - 1)] - ordered[i]) * (h - i)


def stats(values):
    return {
        "n": len(values),
        "mean": statistics.mean(values) if values else None,
        "p50": quantile(values, 0.5),
        "p95": quantile(values, 0.95),
        "p99": quantile(values, 0.99),
        "quantile_method": "linear R7: (n-1)*p",
        "warning": "small sample; local offline workload, not production tail latency"
        if len(values) < 100
        else "local workload only",
    }


def bootstrap(differences, seed=9, resamples=1000):
    if len(differences) < 5:
        return {
            "status": "insufficient_samples",
            "n": len(differences),
            "seed": seed,
            "resamples": resamples,
            "interval": None,
        }
    rng = random.Random(seed)
    means = [
        sum(rng.choices(differences, k=len(differences))) / len(differences)
        for _ in range(resamples)
    ]
    return {
        "status": "degenerate" if len(set(differences)) == 1 else "diagnostic_only",
        "n": len(differences),
        "seed": seed,
        "resamples": resamples,
        "method": "paired percentile 95%; linear R7",
        "interval": [quantile(means, 0.025), quantile(means, 0.975)],
        "warning": "synthetic correlated cases; no production significance claim",
    }


def value(case, metric):
    if case is None:
        return None
    if metric == "execution_success":
        return (
            1.0
            if case.get("status") == "succeeded"
            else 0.0
            if case.get("status") == "execution_failed"
            else None
        )
    if metric.startswith("latency"):
        n = case.get("latency_ms")
        return (
            n
            if case.get("status") == "succeeded"
            and type(n) in (int, float)
            and math.isfinite(n)
            and n >= 0
            else None
        )
    if metric == "cost_nano_usd":
        c = case.get("estimated_cost") or {}
        n = c.get("nano_usd")
        return (
            int(n)
            if c.get("coverage") == "complete_for_observed_attempts"
            and isinstance(n, str)
            and n.isdigit()
            else None
        )
    if case.get("status") != "succeeded":
        return None
    m = case.get("metrics", {}).get(metric, {})
    n = m.get("value")
    return (
        n
        if m.get("status") == "scored"
        and type(n) in (int, float)
        and math.isfinite(n)
        and 0 <= n <= 1
        else None
    )


def mean(values, money=False):
    if not values:
        return None
    return str(sum(Decimal(v) for v in values) / len(values)) if money else statistics.mean(values)


def known_cost(case):
    cost = case.get("estimated_cost") or {}
    v = cost.get("known_nano_usd", cost.get("nano_usd"))
    return int(v) if isinstance(v, str) and v.isdigit() else 0


def delta(a, b, money=False):
    if a is None or b is None:
        return {"absolute_delta": None, "relative_delta": None, "percentage_point_delta": None}
    if money:
        a, b = Decimal(a), Decimal(b)
        d = b - a
        return {
            "absolute_delta": str(d),
            "relative_delta": str(d / a) if a else None,
            "percentage_point_delta": None,
        }
    return {
        "absolute_delta": b - a,
        "relative_delta": (b - a) / a if a else None,
        "percentage_point_delta": None,
    }


def compare(baseline, candidate):
    a, b = baseline["configuration"], candidate["configuration"]
    differences = {
        key: {"baseline": a.get(key), "candidate": b.get(key)}
        for key in sorted(set(a) | set(b))
        if a.get(key) != b.get(key) and key != "manifest"
    }
    errors = []
    for config in (a, b):
        if config.get("manifest"):
            try:
                expected = freeze(config)
                if config["manifest"] != expected:
                    errors.append("manifest_snapshot_mismatch")
            except (ValueError, KeyError, TypeError):
                errors.append("invalid_manifest")
    for key in (
        "dataset_checksum",
        "dataset_version",
        "corpus_checksum",
        "evaluator_version",
        "evaluator_type",
        "mode",
    ):
        if a.get(key) != b.get(key):
            errors.append("incompatible_" + key)
    for key in ("scoring_definition", "measurement_version"):
        legacy = {
            "scoring_definition": "constrained-evidence-v1:macro-v1:precision-denominator-K",
            "measurement_version": "http-wall-ms-v1",
        }[key]
        if a.get(key, legacy) != b.get(key, legacy):
            errors.append("incompatible_" + key)
    if set(a.get("case_ids", [])) != set(b.get("case_ids", [])):
        errors.append("case_membership_mismatch")
    if not a.get("case_ids") or not b.get("case_ids"):
        errors.append("empty_case_membership")
    if (
        baseline.get("status", "completed") != "completed"
        or candidate.get("status", "completed") != "completed"
    ):
        errors.append("run_not_completed")
    maps = []
    for side in (baseline, candidate):
        rows = side["results"]
        ids = [r["case_id"] for r in rows]
        if len(ids) != len(set(ids)):
            errors.append("duplicate_case_ids")
        if set(ids) != set(side["configuration"].get("case_ids", [])):
            errors.append("missing_or_unexpected_case_results")
        maps.append({r["case_id"]: r for r in rows})
    left, right = maps
    ids = sorted(set(a.get("case_ids", [])) | set(b.get("case_ids", [])) | set(left) | set(right))
    cases = []
    transitions = {
        key: 0
        for key in ("both_correct", "both_incorrect", "improved", "regressed", "missing_ineligible")
    }
    for case_id in ids:
        x, y = left.get(case_id), right.get(case_id)
        old, new = value(x, "correctness"), value(y, "correctness")
        transition = (
            "missing_ineligible"
            if old is None or new is None
            else "both_correct"
            if old == new == 1
            else "both_incorrect"
            if old == new == 0
            else "improved"
            if new > old
            else "regressed"
        )
        transitions[transition] += 1
        sx, sy = value(x, "execution_success"), value(y, "execution_success")
        execution = (
            "missing"
            if sx is None or sy is None
            else "new_failure"
            if sx == 1 and sy == 0
            else "recovered_failure"
            if sx == 0 and sy == 1
            else "both_failed"
            if sx == sy == 0
            else "both_success"
        )

        def claims(r):
            j = (r or {}).get("judgments") or {}
            return j if isinstance(j, dict) else {}

        jx, jy = claims(x), claims(y)
        cases.append(
            {
                "case_id": case_id,
                "transition": transition,
                "execution_transition": execution,
                "baseline_correctness": old,
                "candidate_correctness": new,
                "new_unsupported": jy.get("unsupported_claims", 0)
                > jx.get("unsupported_claims", 0),
                "new_contradictions": jy.get("contradictions", 0) > jx.get("contradictions", 0),
                "baseline_trace_id": (x or {}).get("trace_id"),
                "candidate_trace_id": (y or {}).get("trace_id"),
                "baseline_trace_visibility": (x or {}).get("trace_visibility", "unknown"),
                "candidate_trace_visibility": (y or {}).get("trace_visibility", "unknown"),
                "baseline_run_id": baseline["run_id"],
                "candidate_run_id": candidate["run_id"],
                "baseline_retrieved": (x or {}).get("retrieved_document_ids", []),
                "candidate_retrieved": (y or {}).get("retrieved_document_ids", []),
                "baseline_supplied": (x or {}).get("context_document_ids", []),
                "candidate_supplied": (y or {}).get("context_document_ids", []),
            }
        )
    metrics = {}
    cutoff_a = a.get("metric_k", a.get("k"))
    cutoff_b = b.get("metric_k", b.get("k"))
    pricing_ok = (
        all(
            a.get(k) is not None and a.get(k) == b.get(k)
            for k in ("pricing_checksum", "pricing_version", "currency")
        )
        and a.get("pricing_version") != "unknown"
    )
    for metric, unit in UNITS.items():
        money = metric == "cost_nano_usd"
        av = [value(left.get(i), metric) for i in ids]
        bv = [value(right.get(i), metric) for i in ids]
        pairs = [(x, y) for x, y in zip(av, bv, strict=True) if x is not None and y is not None]
        pa, pb = [p[0] for p in pairs], [p[1] for p in pairs]
        va, vb = mean(pa, money), mean(pb, money)
        if metric == "latency_p95_ms":
            va, vb = quantile(pa, 0.95), quantile(pb, 0.95)
        reasons = list(errors)
        if metric in {"recall", "rr", "context_retention"} and cutoff_a != cutoff_b:
            reasons.append("metric_cutoff_mismatch")
        if money and not pricing_ok:
            reasons.append("pricing_incompatible_or_unknown")

        def counts(rows, values):
            na = sum(
                (rows.get(i) or {}).get("metrics", {}).get(metric, {}).get("status")
                == "not_applicable"
                for i in ids
            )
            eligible = len(ids) - na
            scored = sum(x is not None for x in values)
            return {
                "eligible": eligible,
                "scored": scored,
                "not_applicable": na,
                "missing": sum(i not in rows for i in ids),
                "errors": sum((rows.get(i) or {}).get("status") == "execution_failed" for i in ids),
                "coverage": scored / eligible if eligible else None,
            }

        ca, cb = counts(left, av), counts(right, bv)
        if money and len(pairs) != len(ids):
            reasons.append("cost_coverage_incomplete")
        change = delta(va, vb, money) if not reasons else delta(None, None, money)
        if unit == "fraction" and change["absolute_delta"] is not None:
            change["percentage_point_delta"] = 100 * change["absolute_delta"]
        metrics[metric] = {
            "unit": unit,
            "direction": "lower" if unit != "fraction" else "higher",
            "status": "incompatible" if reasons else "scored" if pairs else "unscored",
            "reasons": reasons,
            "baseline": va if not reasons else None,
            "candidate": vb if not reasons else None,
            **change,
            "matched": len(pairs),
            "baseline_counts": ca,
            "candidate_counts": cb,
            "baseline_full_mean": mean([x for x in av if x is not None], money),
            "candidate_full_mean": mean([x for x in bv if x is not None], money),
            "bootstrap": bootstrap([y - x for x, y in pairs])
            if metric in QUALITY and not reasons
            else None,
        }
        if money:
            metrics[metric].update(
                baseline_known_total_nano_usd=str(sum(known_cost(r) for r in left.values())),
                candidate_known_total_nano_usd=str(sum(known_cost(r) for r in right.values())),
                baseline_usage_complete_cases=sum(
                    (r.get("usage") or {}).get("usage_coverage") == "complete_for_observed_attempts"
                    for r in left.values()
                ),
                candidate_usage_complete_cases=sum(
                    (r.get("usage") or {}).get("usage_coverage") == "complete_for_observed_attempts"
                    for r in right.values()
                ),
                baseline_total_nano_usd=str(sum(av))
                if all(x is not None for x in av) and pricing_ok and not errors
                else None,
                candidate_total_nano_usd=str(sum(bv))
                if all(x is not None for x in bv) and pricing_ok and not errors
                else None,
                pricing_version=a.get("pricing_version"),
                currency=a.get("currency"),
                provenance="synthetic_fixture"
                if a.get("mode") == "fixture"
                else "provider_reported",
                baseline_input_known=str(
                    sum(
                        int((r.get("usage") or {}).get("known_input_tokens", "0"))
                        for r in left.values()
                    )
                ),
                candidate_input_known=str(
                    sum(
                        int((r.get("usage") or {}).get("known_input_tokens", "0"))
                        for r in right.values()
                    )
                ),
                baseline_output_known=str(
                    sum(
                        int((r.get("usage") or {}).get("known_output_tokens", "0"))
                        for r in left.values()
                    )
                ),
                candidate_output_known=str(
                    sum(
                        int((r.get("usage") or {}).get("known_output_tokens", "0"))
                        for r in right.values()
                    )
                ),
            )
    latency = {}
    for name, rows in (("baseline", left), ("candidate", right)):
        latency[name] = {
            group: stats(
                [
                    r["latency_ms"]
                    for r in rows.values()
                    if type(r.get("latency_ms")) in (int, float)
                    and math.isfinite(r["latency_ms"])
                    and (
                        group == "all_attempts"
                        or (r.get("status") == "succeeded") == (group == "successful")
                    )
                ]
            )
            for group in ("all_attempts", "successful", "failed")
        }
    return {
        "engine_version": VERSION,
        "manifest_provenance": {
            "baseline": "hashed_manifest"
            if a.get("manifest")
            else "legacy_v1_frozen_snapshot; no invented manifest",
            "candidate": "hashed_manifest"
            if b.get("manifest")
            else "legacy_v1_frozen_snapshot; no invented manifest",
        },
        "baseline_run_id": baseline["run_id"],
        "candidate_run_id": candidate["run_id"],
        "baseline_configuration": a,
        "candidate_configuration": b,
        "mode": a.get("mode"),
        "human_review": b.get("human_review", "pending"),
        "compatible": not errors,
        "compatibility_errors": sorted(set(errors)),
        "configuration_diff": differences,
        "metric_cutoff": {"baseline": cutoff_a, "candidate": cutoff_b},
        "metrics": metrics,
        "transitions": transitions,
        "cases": cases,
        "latency": latency,
        "release_scope": "offline fixture evidence only; no deployment"
        if a.get("mode") == "fixture"
        else "live evidence; semantic validation must be checked separately",
    }
