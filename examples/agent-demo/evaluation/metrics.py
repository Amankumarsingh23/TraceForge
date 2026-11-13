def score(numerator, denominator, status=None):
    return {
        "value": numerator / denominator if denominator else None,
        "numerator": numerator,
        "denominator": denominator,
        "status": status or ("scored" if denominator else "not_applicable"),
    }


def retrieval(ranking, supplied, relevant, k):
    if type(k) is not int or not 1 <= k <= 5:
        raise ValueError("K must be 1..5")
    relevant = set(relevant)
    top = ranking[:k]  # Preserve original positions; duplicates consume slots.
    recovered = relevant.intersection(top)
    first = next((i for i, d in enumerate(top, 1) if d in relevant), None)
    metrics = {
        "recall": score(len(recovered), len(relevant)),
        "precision": score(len(recovered), k) if relevant else score(0, 0),
        "hit": score(int(bool(recovered)), 1) if relevant else score(0, 0),
        "rr": score(1, first) if first else score(0, 1) if relevant else score(0, 0),
        "context_coverage": score(len(relevant.intersection(supplied)), len(relevant)),
        "context_retention": score(len(recovered.intersection(supplied)), len(recovered)),
    }
    return metrics, {
        "duplicate_ids": sorted({d for d in ranking if ranking.count(d) > 1}),
        "short_ranking": len(top) < k,
    }


def aggregate(results):
    names = sorted({name for r in results for name in r["metrics"]})
    output = {}
    for name in names:
        metrics = [r["metrics"].get(name, {"status": "pending", "value": None}) for r in results]
        scored = [m for m in metrics if m["status"] == "scored"]
        excluded = sum(m["status"] == "not_applicable" for m in metrics)
        failed = sum(r["status"] == "execution_failed" for r in results)
        eligible = len(results) - excluded
        output[name] = {
            "value": sum(m["value"] for m in scored) / len(scored) if scored else None,
            "macro_numerator": sum(m["value"] for m in scored),
            "macro_denominator": len(scored),
            "eligible": eligible,
            "scored": len(scored),
            "excluded": excluded,
            "execution_failures": failed,
            "pending": eligible - len(scored),
            "coverage": len(scored) / eligible if eligible else None,
        }
    return output
