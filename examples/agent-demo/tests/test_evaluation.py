import copy
import json

import pytest

from agent_demo.constrained import CONTRACT, decode, generate, render
from evaluation.dataset import checksum, load
from evaluation.metrics import aggregate, retrieval
from evaluation.runner import failure_metrics, transform
from evaluation.scoring import evaluate


def output(claims, abstained=False):
    return json.dumps(
        {
            "contract": CONTRACT,
            "claims": claims,
            "abstained": abstained,
            "answer": render(claims, abstained),
        }
    )


def test_hand_arithmetic_and_rank_positions():
    m, a = retrieval(["D1", "D2", "D3", "D4", "D5"], ["D1", "D2"], ["D2", "D4", "D6"], 3)
    assert m["recall"]["value"] == pytest.approx(1 / 3)
    assert m["precision"]["value"] == pytest.approx(1 / 3)
    assert m["rr"]["value"] == 0.5
    five, _ = retrieval(["D1", "D2", "D3", "D4", "D5"], ["D1", "D2"], ["D2", "D4", "D6"], 5)
    assert five["recall"]["value"] == pytest.approx(2 / 3)
    assert (
        five["precision"]["value"] == 0.4
        and five["hit"]["value"] == 1
        and five["rr"]["value"] == 0.5
    )
    assert a["duplicate_ids"] == []


def test_duplicate_short_and_empty_ranking():
    m, a = retrieval(["D9", "D9", "D1"], ["D1"], ["D1"], 5)
    assert m["rr"]["value"] == pytest.approx(1 / 3)
    assert m["precision"]["value"] == 0.2
    assert a == {"duplicate_ids": ["D9"], "short_ranking": True}
    m, _ = retrieval([], [], ["D1"], 3)
    assert all(m[k]["value"] == 0 for k in ("recall", "precision", "hit", "rr", "context_coverage"))
    assert m["context_retention"]["status"] == "not_applicable"
    m, _ = retrieval(["D1"], [], [], 3)
    assert all(v["value"] is None for v in m.values())


@pytest.mark.parametrize("k", [0, -1, 6, 1.5, True, "3"])
def test_invalid_k(k):
    with pytest.raises(ValueError):
        retrieval([], [], [], k)


def test_context_loss():
    m, _ = retrieval(["D1", "D4", "D2", "D5", "D3"], ["D1", "D4"], ["D1", "D2", "D3"], 5)
    assert m["recall"]["value"] == 1
    assert m["context_coverage"]["value"] == pytest.approx(1 / 3)
    assert m["context_retention"]["value"] == pytest.approx(1 / 3)


def test_independent_dataset_hash_and_evidence():
    d, c, h = load()
    assert len(d["cases"]) == 35 and len(d["facts"]) == 12
    assert checksum(dict(reversed(list(d.items())))) == h
    changed = copy.deepcopy(d)
    changed["cases"][0]["question"] += "?"
    assert checksum(changed) != h
    assert d["corpus_checksum"] == checksum(c)
    assert sum(bool(case["relevant_document_ids"]) for case in d["cases"]) == 30


def score_claims(claims, supplied=("D01",), case=None):
    d, c, _ = load()
    return evaluate(
        output(claims),
        supplied,
        case or d["cases"][0],
        d["facts"],
        {v["id"] for v in c["documents"]},
    )


@pytest.mark.parametrize(
    "doc,valid,available,support", [("D01", 1, 1, 1), ("D12", 1, 0, 0), ("D99", 0, 0, 0)]
)
def test_citation_id_is_not_support(doc, valid, available, support):
    m, j, status = score_claims([{"key": "refund_window", "value": "30 days", "citations": [doc]}])
    assert m["groundedness"]["value"] == 1
    assert m["citation_validity"]["value"] == valid
    assert m["citation_availability"]["value"] == available
    assert m["citation_correctness"]["value"] == support
    assert status == "constrained_scored" and j["claims"][0]["correct_value"]


def test_correct_but_unsupplied_and_multiple_citations():
    claim = {"key": "refund_window", "value": "30 days", "citations": ["D01", "D12"]}
    m, _, _ = score_claims([claim], supplied=["D12"])
    assert m["correctness"]["value"] == 1 and m["groundedness"]["value"] == 0
    assert m["citation_correctness"]["value"] == 0
    m, _, _ = score_claims([claim], supplied=["D01", "D12"])
    assert m["citation_correctness"]["value"] == 0.5 and m["citation_coverage"]["value"] == 1


def test_missing_citation_reduces_coverage_not_other_citations_support():
    claims = [
        {"key": "refund_window", "value": "30 days", "citations": ["D01"]},
        {"key": "damage_report_window", "value": "7 days", "citations": []},
    ]
    m, _, _ = score_claims(claims, supplied=["D01", "D02"])
    assert m["citation_correctness"]["value"] == 1
    assert m["citation_coverage"]["value"] == 0.5


def test_negation_unsupported_missing_fact_and_missing_citation():
    m, j, _ = score_claims([{"key": "refund_window", "value": "NOT 30 days", "citations": ["D01"]}])
    assert (
        m["groundedness"]["value"] == 0
        and m["correctness"]["value"] == 0
        and j["contradictions"] == 1
    )
    m, j, _ = score_claims(
        [
            {"key": "refund_window", "value": "30 days", "citations": []},
            {"key": "invented", "value": "24 hours", "citations": ["D01"]},
        ]
    )
    assert m["groundedness"]["value"] == 0.5 and m["required_fact_coverage"]["value"] == 1
    assert m["correctness"]["value"] == 0 and m["citation_coverage"]["value"] == 0
    assert j["unsupported_claims"] == 1


def test_abstention_and_uninterpretable_output_no_fake_scores():
    d, c, _ = load()
    unknown = d["cases"][-1]
    m, _, _ = evaluate(output([], True), [], unknown, d["facts"], {v["id"] for v in c["documents"]})
    assert m["correctness"]["value"] == 1 and m["groundedness"]["value"] is None
    m, _, _ = evaluate(output([], True), [], d["cases"][0], d["facts"], set())
    assert m["correctness"]["value"] == 0 and m["groundedness"]["value"] == 0
    m, _, status = evaluate(
        "Free form, cannot evaluate automatically", [], unknown, d["facts"], set()
    )
    assert status == "semantic_review_pending" and all(v["value"] is None for v in m.values())
    bad = json.loads(output([{"key": "refund_window", "value": "30 days", "citations": ["D01"]}]))
    bad["answer"] = "Refunds are NOT allowed"
    assert decode(json.dumps(bad)) is None


def test_fixture_reads_actual_context_not_labels():
    d, c, _ = load()
    result = generate(d["cases"][0]["question"], [c["documents"][0]])
    assert result["claims"][0]["value"] == "30 days"
    assert generate(d["cases"][0]["question"], [])["abstained"]
    changed = copy.deepcopy(c["documents"][0])
    changed["text"] = changed["text"].replace("30 days", "99 days")
    assert generate(d["cases"][0]["question"], [changed])["abstained"]


def test_aggregate_failure_and_na_coverage():
    d, _, _ = load()
    success, _ = retrieval(["D01"], ["D01"], ["D01"], 3)
    na, _ = retrieval([], [], [], 3)
    totals = aggregate(
        [
            {"status": "succeeded", "metrics": success},
            {"status": "execution_failed", "metrics": failure_metrics(d["cases"][0])},
            {"status": "succeeded", "metrics": na},
        ]
    )
    assert totals["recall"] == {
        "value": 1,
        "macro_numerator": 1,
        "macro_denominator": 1,
        "eligible": 2,
        "scored": 1,
        "excluded": 1,
        "execution_failures": 1,
        "pending": 1,
        "coverage": 0.5,
    }


@pytest.mark.parametrize(
    "fault",
    [
        "incorrect_fact",
        "unsupported_claim",
        "missing_fact",
        "wrong_citation",
        "invalid_citation",
        "missing_citation",
        "inappropriate_abstention",
    ],
)
def test_controlled_output_transforms_are_explicit_and_rendered(fault):
    actual = generate("refund eligibility", [{"id": "D01", "text": "within 30 days of delivery"}])
    changed = transform(actual, fault)
    assert decode(json.dumps(changed)) == changed
    assert actual["claims"][0]["value"] == "30 days"
