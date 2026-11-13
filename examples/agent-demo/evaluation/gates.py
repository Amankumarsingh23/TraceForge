import re
from decimal import Decimal, InvalidOperation

from .comparison import UNITS
from .dataset import checksum

OPS = {
    "min",
    "max",
    "max_degradation",
    "max_relative_increase",
    "max_relative_degradation",
    "min_coverage",
    "min_matched",
    "max_failure_rate",
    "no_critical_regression",
    "require_mode",
    "require_semantic_review",
}


def validate(policy):
    if (
        not isinstance(policy, dict)
        or not isinstance(policy.get("rules"), list)
        or not 1 <= len(policy["rules"]) <= 100
        or any(not isinstance(r, dict) for r in policy["rules"])
    ):
        raise ValueError("bounded rule list required")
    if set(policy) - {
        "policy_id",
        "version",
        "rules",
        "critical_cases",
        "critical_rationale",
        "scope",
    }:
        raise ValueError("unknown policy field")
    if any(
        not isinstance(policy.get(k), str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", policy[k])
        for k in ("policy_id", "version")
    ):
        raise ValueError("invalid policy identity")
    critical = policy.get("critical_cases", [])
    if (
        not isinstance(critical, list)
        or len(critical) > 50
        or any(
            not isinstance(c, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,64}", c) for c in critical
        )
        or len(set(critical)) != len(critical)
    ):
        raise ValueError("invalid critical cases")
    if (
        not policy.get("policy_id")
        or not policy.get("version")
        or not policy.get("rules")
        or not any(r.get("mandatory") is True for r in policy["rules"])
    ):
        raise ValueError("mandatory policy required")
    seen = set()
    for rule in policy["rules"]:
        if not isinstance(rule.get("id"), str) or not re.fullmatch(
            r"[A-Za-z0-9_-]{1,64}", rule["id"]
        ):
            raise ValueError("invalid rule ID")
        if set(rule) - {"id", "op", "metric", "unit", "threshold", "mandatory", "allow_na"}:
            raise ValueError("unknown rule field")
        if rule.get("id") in seen or not rule.get("id") or type(rule.get("mandatory")) is not bool:
            raise ValueError("invalid rule identity")
        seen.add(rule["id"])
        op = rule.get("op")
        if op not in OPS:
            raise ValueError("unsupported gate operator")
        if "allow_na" in rule and type(rule["allow_na"]) is not bool:
            raise ValueError("invalid N/A policy")
        if op in {"require_mode", "require_semantic_review"}:
            if set(rule) & {"metric", "unit", "allow_na"}:
                raise ValueError("unsupported evidence rule fields")
            if rule.get("threshold") not in (
                {"live", "fixture"} if op == "require_mode" else {"independent_verified"}
            ):
                raise ValueError("invalid evidence threshold")
            continue
        if op == "no_critical_regression":
            if set(rule) & {"metric", "unit", "threshold", "allow_na"}:
                raise ValueError("unsupported critical rule fields")
            continue
        metric = rule.get("metric")
        if metric not in UNITS:
            raise ValueError("unknown metric")
        unit = (
            "fraction"
            if op
            in {
                "min_coverage",
                "max_failure_rate",
                "max_relative_increase",
                "max_relative_degradation",
            }
            else "count"
            if op == "min_matched"
            else UNITS[metric]
        )
        if rule.get("unit") != unit:
            raise ValueError("inconsistent units")
        try:
            n = Decimal(str(rule["threshold"]))
        except (InvalidOperation, KeyError):
            raise ValueError("invalid threshold") from None
        if (
            not n.is_finite()
            or n < 0
            or unit == "fraction"
            and n > 1
            or op == "min_matched"
            and (n < 1 or n != int(n))
        ):
            raise ValueError("invalid threshold range")
        if op == "max_relative_increase" and UNITS[metric] == "fraction":
            raise ValueError("relative increase only supports lower-is-better metrics")
        if op == "max_relative_degradation" and UNITS[metric] != "fraction":
            raise ValueError("relative quality degradation requires higher-is-better metric")
        if op == "max_failure_rate" and metric != "execution_success":
            raise ValueError("execution failure rate metric required")
    return checksum(policy)


def decide(comparison, policy):
    digest = validate(policy)
    outcomes = []
    critical = set(policy.get("critical_cases", []))
    cases = {c["case_id"]: c for c in comparison["cases"]}
    for rule in policy["rules"]:
        op = rule["op"]
        status = "INCONCLUSIVE"
        reason = "required evidence unavailable"
        observed = None
        if op == "require_mode":
            observed = comparison["mode"]
            status = "PASS" if observed == rule["threshold"] else "INCONCLUSIVE"
            reason = "evidence mode scope"
        elif op == "require_semantic_review":
            observed = comparison.get("human_review")
            status = "PASS" if observed == rule["threshold"] else "INCONCLUSIVE"
            reason = "independent semantic validation required"
        elif op == "no_critical_regression":
            bad = [
                i
                for i in sorted(critical)
                if i in cases
                and (
                    cases[i]["transition"] == "regressed"
                    or cases[i]["execution_transition"] == "new_failure"
                    or cases[i]["new_unsupported"]
                    or cases[i]["new_contradictions"]
                )
            ]
            missing = [
                i
                for i in sorted(critical)
                if i not in cases or cases[i]["transition"] == "missing_ineligible"
            ]
            status = "FAIL" if bad else "INCONCLUSIVE" if missing or not critical else "PASS"
            reason = "protected cases: " + str({"regressions": bad, "missing": missing})
            observed = bad
        else:
            metric = comparison["metrics"][rule["metric"]]
            counts = metric["candidate_counts"]
            if op == "min_coverage":
                observed = counts["coverage"]
            elif op == "min_matched":
                observed = metric["matched"]
            elif op == "max_failure_rate":
                observed = (
                    counts["errors"] / len(comparison["cases"]) if comparison["cases"] else None
                )
            elif metric["status"] == "scored":
                if op in {"min", "max"}:
                    observed = metric["candidate"]
                elif op == "max_relative_increase":
                    observed = metric["relative_delta"]
                elif op == "max_relative_degradation":
                    observed = (
                        -Decimal(str(metric["relative_delta"]))
                        if metric["relative_delta"] is not None
                        else None
                    )
                elif op == "max_degradation":
                    observed = (
                        -Decimal(str(metric["absolute_delta"]))
                        if metric["direction"] == "higher"
                        else metric["absolute_delta"]
                    )
            if any(reason != "cost_coverage_incomplete" for reason in metric["reasons"]):
                observed = None
            if observed is not None:
                test = Decimal(str(observed))
                threshold = Decimal(str(rule["threshold"]))
                passed = (
                    test >= threshold
                    if op in {"min", "min_coverage", "min_matched"}
                    else test <= threshold
                )
                status = (
                    "PASS"
                    if passed
                    else "INCONCLUSIVE"
                    if op in {"min_coverage", "min_matched"}
                    else "FAIL"
                )
                reason = f"{op}: {test} vs {threshold} {rule['unit']}"
            elif rule.get("allow_na") and counts["eligible"] == 0:
                status = "N/A"
                reason = "policy explicitly permits no applicable cases"
        outcomes.append(
            {
                **rule,
                "outcome": status,
                "observed": str(observed) if isinstance(observed, Decimal) else observed,
                "reason": reason,
            }
        )
    required = [r for r in outcomes if r["mandatory"]]
    outcome = (
        "INCONCLUSIVE"
        if not comparison["compatible"]
        else "FAIL"
        if any(r["outcome"] == "FAIL" for r in required)
        else "INCONCLUSIVE"
        if any(r["outcome"] == "INCONCLUSIVE" for r in required)
        else "PASS"
    )
    return {
        "gate_engine_version": "gate-v1",
        "outcome": outcome,
        "policy_id": policy["policy_id"],
        "policy_version": policy["version"],
        "policy_checksum": digest,
        "policy": policy,
        "rules": outcomes,
        "release_allowed": outcome == "PASS",
        "scope": policy.get("scope", "offline diagnostic only"),
        "reason": "incompatible comparison"
        if not comparison["compatible"]
        else "all individual mandatory rule outcomes retained; only PASS satisfies this policy; no deployment",
    }
