import json
import math
import re
from collections import Counter
from dataclasses import dataclass
from pathlib import Path

DATA = Path(__file__).resolve().parent.parent / "data"
RETRIEVER_VERSION = "bm25-v1"
PROMPT_VERSION = "support-v1"


@dataclass(frozen=True)
class Document:
    id: str
    title: str
    text: str


@dataclass(frozen=True)
class Hit:
    document: Document
    score: float


def terms(text: str) -> list[str]:
    return re.findall(r"[a-z0-9]+", text.lower())


class Retriever:
    def __init__(self, path: Path = DATA / "corpus.json"):
        corpus = json.loads(path.read_text(encoding="utf-8"))
        self.version = corpus["version"]
        self.documents = [Document(**d) for d in corpus["documents"]]
        if not 1 <= len(self.documents) <= 20 or len({d.id for d in self.documents}) != len(
            self.documents
        ):
            raise ValueError("invalid corpus bounds or duplicate IDs")
        if any(not re.fullmatch(r"D\d{2}", d.id) or len(d.text) > 2000 for d in self.documents):
            raise ValueError("invalid corpus document")
        self.tokens = [Counter(terms(d.title + " " + d.text)) for d in self.documents]
        self.lengths = [sum(t.values()) for t in self.tokens]
        self.average = sum(self.lengths) / len(self.lengths)

    def search(self, question: str, top_k: int) -> list[Hit]:
        if not 1 <= top_k <= 5 or len(question) > 1000:
            raise ValueError("retrieval bounds exceeded")
        query = sorted(set(terms(question)))
        scored = []
        n = len(self.documents)
        for document, frequencies, length in zip(
            self.documents, self.tokens, self.lengths, strict=True
        ):
            score = 0.0
            for word in query:
                frequency = frequencies[word]
                if not frequency:
                    continue
                df = sum(word in t for t in self.tokens)
                inverse = math.log(1 + (n - df + 0.5) / (df + 0.5))
                score += (
                    inverse
                    * frequency
                    * 2.2
                    / (frequency + 1.2 * (0.25 + 0.75 * length / self.average))
                )
            if score > 0:
                scored.append(Hit(document, score))
        return sorted(scored, key=lambda h: (-h.score, h.document.id))[:top_k]

    @staticmethod
    def supplied(hits: list[Hit], max_documents: int = 2) -> list[Document]:
        if not 1 <= max_documents <= 3:
            raise ValueError("context document bound exceeded")
        return [h.document for h in hits[:max_documents]]
