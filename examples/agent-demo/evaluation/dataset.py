import hashlib
import json
from pathlib import Path

from agent_demo.retrieval import DATA

ROOT = Path(__file__).resolve().parent


def checksum(value):
    return hashlib.sha256(
        json.dumps(value, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    ).hexdigest()


def load(path=ROOT / "data/support-eval-v1.json"):
    dataset = json.loads(Path(path).read_text(encoding="utf-8"))
    corpus = json.loads((DATA / "corpus.json").read_text(encoding="utf-8"))
    documents = {d["id"]: d for d in corpus["documents"]}
    facts = dataset["facts"]
    if dataset["corpus_version"] != corpus["version"] or dataset["corpus_checksum"] != checksum(
        corpus
    ):
        raise ValueError("corpus identity mismatch")
    if not 30 <= len(dataset["cases"]) <= 50 or len(
        {c["case_id"] for c in dataset["cases"]}
    ) != len(dataset["cases"]):
        raise ValueError("invalid case count/identity")
    for fact in facts.values():
        if (
            not fact["alternatives"]
            or not fact["documents"]
            or any(d not in documents for d in fact["documents"])
        ):
            raise ValueError("invalid fact labels")
        if not any(
            fact["evidence_quote"].casefold() in documents[d]["text"].casefold()
            for d in fact["documents"]
        ):
            raise ValueError("evidence quote missing")
    for case in dataset["cases"]:
        if any(
            not set(facts[f]["documents"]) & set(case["relevant_document_ids"])
            for f in case["expected_facts"]
            if f in facts
        ):
            raise ValueError("expected fact missing relevance evidence")
        if (
            not 1 <= len(case["question"]) <= 1000
            or not set(case["relevant_document_ids"]) <= documents.keys()
            or not set(case["expected_facts"]) <= facts.keys()
        ):
            raise ValueError("invalid case labels")
        if case["answerable"] != bool(case["expected_facts"]):
            raise ValueError("answerability mismatch")
        if case["review_status"] not in {"author_reviewed", "uncertain", "unreviewed"}:
            raise ValueError("invalid review provenance")
    return dataset, corpus, checksum(dataset)
