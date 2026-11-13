# Session 8 metric contract — constrained-evidence-v1

All inputs are actual application outputs compared with independently authored synthetic labels. Metrics return value, numerator, denominator and explicit status; unknown values are null. Run aggregates are **macro means of scored per-case values**, not micro-pooled token/document ratios. Each aggregate includes sum of scored values, scored-case denominator, eligible/scored/excluded counts, execution failures, pending count and coverage (scored/eligible). Execution failures do not become numerical zeros or disappear from coverage. A completed evaluation run means the runner finished; it does not mean every execution succeeded or every metric applies.

## Retrieval

Let R be the set of labeled relevant document IDs, L the actual ranked sequence, S the set of actually supplied context IDs, and T the set of relevant IDs appearing in the first K **original positions** of L. K must be an integer 1–5; booleans are rejected. Duplicate IDs consume rank positions but cannot increase the relevant-document count. Duplicate and short-ranking anomalies are recorded.

| Metric | Per-case formula |
| --- | --- |
| Recall@K | size(T) / size(R) |
| Precision@K | size(T) / K, including short rankings |
| Hit@K | 1 if T nonempty, else 0 |
| RR@K | 1 / first relevant original rank within K; otherwise 0 |
| MRR@K | Macro average RR@K over scored cases |
| Supplied-context document coverage | size(R intersect S) / size(R) |
| Retrieved-relevant context retention | size(T intersect S) / size(T) |

With relevant labels and a successful empty ranking, recall/precision/hit/RR/context coverage are zero. Retention is N/A if no relevant documents were retrieved. With R empty, all relevant-set metrics are N/A; evaluate abstention separately. Failed executions with nonempty R remain eligible but unscored. Relevance labels describe static document retrieval; tool-dependent tasks must have separate task labels and are not included in this document-only dataset.

## Constrained generation

The application fixture extracts a closed set of factual claim objects from actual supplied documents. It receives only the question, execution settings and actual context, never reference facts/relevance labels. The displayed answer is rendered from those claim objects. The evaluator independently validates the exact contract, allowed values/alternatives, evidence mapping and correspondence with displayed answer. A mismatched/free-form/uninterpretable response receives `semantic_review_pending` and null semantic scores, not keyword-overlap scores. This is not natural-language entailment or general-purpose hallucination detection.

- **Groundedness:** claims with a rubric-approved value supported by at least one actually supplied document / all generated claims. Full-corpus evidence alone is insufficient. A recognized fact key with a disallowed closed-domain value is a rubric contradiction; an unknown key is unsupported. Both judgments and independent source document IDs are retained. This closed-domain contradiction rule is not a general semantic contradiction detector.
- **Required-fact coverage:** distinct required fact keys correctly stated / independently labeled required fact keys. Missing required facts reduce coverage even if every generated claim is grounded.
- **Overall rubric correctness:** binary, requiring appropriate answerability/abstention, every required fact, no invalid/unknown claim values and no rubric contradictions. Correctness compares against source truth, while groundedness requires evidence actually supplied. Thus a correct fact can be ungrounded in the observed context. A grounded true but incomplete answer can fail overall correctness.
- **Abstention:** binary match between actual abstention and labeled unanswerability. Unknown questions should abstain/request clarification. Abstaining on an answerable case fails abstention/correctness/required coverage and receives groundedness zero; no automatic perfect empty-answer score.

## Citations

Each claim has explicit citation associations. For each association independently check: document ID exists in the frozen corpus; document was actually supplied; and its independent evidence mapping supports this exact approved claim value. A valid or relevant document alone is insufficient.

| Metric | Numerator / denominator |
| --- | --- |
| Citation ID validity | Existing corpus IDs / emitted claim-citation associations |
| Citation availability | Actually supplied IDs / emitted associations |
| Citation correctness | Supplied citations supporting exact claim / emitted associations |
| Citation coverage | Citation-required generated claims with at least one valid, supplied, supporting citation / citation-required generated claims |

Multiple citations create multiple associations; one supported citation can cover a claim while an additional wrong citation reduces correctness. A missing citation is detected by coverage: it does not make other existing correct citations incorrect. With a non-abstaining answer requiring citations and no citations, validity/availability/correctness are zero using an explicit 0/1 missing-citation penalty; coverage uses generated-claim count. For an answerable abstention, citation coverage is zero (0/1); other citation metrics are N/A because no associations exist. Appropriate unanswerable abstentions have citation/groundedness/required-coverage N/A, not perfect scores. Free-form semantics pending keeps all semantic/citation scores pending, including apparent citation strings that have not been interpreted reliably.

Known usage/cost comes from the existing canonical attempt summary and retains its completeness/provenance; latency is measured runner HTTP wall time, not model-only latency. No actual billing, throughput benchmark or live-model quality claim follows from fixture measurements. A trace can remain pending/unavailable without invalidating a persisted execution result or creating a fictitious trace.
