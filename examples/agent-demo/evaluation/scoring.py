from agent_demo.constrained import decode

from .metrics import score

QUALITY = (
    "groundedness",
    "correctness",
    "required_fact_coverage",
    "citation_validity",
    "citation_availability",
    "citation_correctness",
    "citation_coverage",
    "abstention",
)
VERSION = "constrained-evidence-v1"


def evaluate(content, supplied, case, facts, corpus_ids):
    output = decode(content)
    if output is None:
        return (
            {name: score(0, 0, "semantic_review_pending") for name in QUALITY},
            [],
            "semantic_review_pending",
        )
    claims = output["claims"]
    supplied = set(supplied)
    judgments, covered = [], set()
    valid = available = supported_citations = citations = grounded = contradictions = 0
    for claim in claims:
        fact = facts.get(claim["key"])
        known = fact is not None and claim["value"].casefold() in {
            v.casefold() for v in fact["alternatives"]
        }
        supports = set(fact["documents"]) if known else set()
        is_grounded = bool(supports & supplied)
        grounded += is_grounded
        contradiction = fact is not None and not known
        contradictions += contradiction
        if known:
            covered.add(claim["key"])
        links = []
        for doc in claim["citations"]:
            citations += 1
            exists = doc in corpus_ids
            present = doc in supplied
            support = present and doc in supports
            valid += exists
            available += present
            supported_citations += support
            links.append(
                {"document_id": doc, "valid": exists, "available": present, "supports": support}
            )
        judgments.append(
            {
                "claim": claim,
                "correct_value": known,
                "grounded": is_grounded,
                "contradiction": contradiction,
                "supporting_document_ids": sorted(supports),
                "citations": links,
                "citation_supported": any(c["supports"] for c in links),
            }
        )
    required = set(case["expected_facts"])
    coverage = len(required & covered)
    abstained = output["abstained"]
    appropriate = abstained == (not case["answerable"])
    citation_required = case["citations_required"] and not abstained
    metrics = {
        "groundedness": score(grounded, len(claims)),
        "required_fact_coverage": score(coverage, len(required)),
        "correctness": score(
            int(
                appropriate
                and (
                    not case["answerable"]
                    or coverage == len(required)
                    and contradictions == 0
                    and all(j["correct_value"] for j in judgments)
                )
            ),
            1,
        ),
        "citation_validity": score(valid, citations)
        if citations
        else score(0, 1)
        if citation_required
        else score(0, 0),
        "citation_availability": score(available, citations)
        if citations
        else score(0, 1)
        if citation_required
        else score(0, 0),
        "citation_correctness": score(supported_citations, citations)
        if citations
        else score(0, 1)
        if citation_required
        else score(0, 0),
        "citation_coverage": score(sum(j["citation_supported"] for j in judgments), len(claims))
        if citation_required and claims
        else score(0, 1)
        if case["answerable"] and abstained
        else score(0, 0),
        "abstention": score(int(appropriate), 1),
    }
    # Empty/abstaining output receives no automatic perfect semantic/citation scores.
    if case["answerable"] and abstained:
        metrics["groundedness"] = score(0, 1)
    return (
        metrics,
        {
            "claims": judgments,
            "unsupported_claims": len(claims) - grounded,
            "contradictions": contradictions,
            "abstained": abstained,
        },
        "constrained_scored",
    )
