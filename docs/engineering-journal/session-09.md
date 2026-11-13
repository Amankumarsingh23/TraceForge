# Session09 — Versioned paired experiments and fail-closed release evidence

Implemented 2026-11-13, Asia/Calcutta; starting commit `d6ccdc1`. Stops after Session9. No paid model/judge calls, automatic deployment/promotion, Session10 analytics or remote push. Independent human semantic validation and live-provider verification remain pending.

## Implementation and actual configurations

The Session8 runner now separates retrieval `k` from `metric_k` and freezes an allowlisted manifest with stable ID, content-derived version/hash, dataset/corpus/prompt/source identities, provider/model/mode, context and generation/tool settings, scoring definitions, pricing/currency and runtime/measurement scope. Migration0002 references manifests from existing runs and stores comparison/policy/decision metadata with constraints, idempotent writes and immutable-history triggers. No new dependency or ingestion path.

`comparison.py` pairs stable case IDs, validates snapshots/cohorts/semantics, records all intended changes, retains missing/failed/N/A cases, exposes matched vs full-run means, transitions, coverage, exact money subtotals and successful/failed latency samples. Quantiles use R7; paired bootstrap uses seed9/1000 percentile resamples, minimum5 pairs, degenerate/small-sample warnings and no production significance claim. `gates.py` validates declarative rules/units and enforces invalid-comparison→INCONCLUSIVE; otherwise mandatory violation→FAIL, then missing evidence→INCONCLUSIVE, then PASS. Coverage/sample insufficiency is INCONCLUSIVE. Critical regressions/new failures/new unsupported or contradictory claims block release; protected cases are all pre-existing multi-document/truncation cases designated before candidate execution.

`compare_cli.py` reuses completed PostgreSQL runs; same comparison/policy retries return the same decision while policy versions retain separate decisions. Go adds bounded read-only experiment/comparison/case/gate endpoints; Next adds fixed same-origin proxies and comparison/decision pages with backend-authoritative deltas, filters and both actual trace links. No frontend scoring engine or deployment action.

Five actual35-case runs use the unchanged dataset/rubric, fixture provider/model, prompt and corpus. Baseline/repeat retrieve3/supply2; primary candidate retrieves3/supplies1; secondary retrieves1/supplies up to2. All retrieval metrics use **evaluation K3**, including short-ranking precision denominator3. Missing-usage is an explicit separate fixture diagnostic, not ordinary billing evidence. Session8's compatible stored baseline was also reused through an explicit legacy-v1 mapping; its absent manifest/pricing-version fields were not invented, and legacy cost comparison remains incompatible.

## Measured results

| Metric | Baseline K3/C2 | Context K3/C1 | Retrieval K1/C2, scored K3 |
| --- | --- | --- | --- |
| Recall@3,30 eligible pairs | 0.983333 | 0.983333 | 0.844444 |
| MRR@3,30 pairs | 0.966667 | 0.966667 | 0.933333 |
| Supplied-context coverage,30 pairs | 0.955556 | 0.844444 | 0.844444 |
| Correctness,35 pairs | 32/35 | 28/35 | 28/35 |
| Correctness delta | — | -11.428571pp | -11.428571pp |
| Newly regressed / improved | — | 4 / 0 | 4 / 0 |
| Complete hypothetical run cost | $0.028 | $0.028 | $0.028 |
| Synthetic input/output tokens | 42000 / 3500 | 42000 / 3500 | 42000 / 3500 |

Ordinary runs have full matched correctness coverage35/35 and retrieval coverage30/30, five retrieval N/A, zero execution failures. Identical configurations have zero deterministic quality deltas (32 both-correct, three both-incorrect). Context/retrieval candidates have28 both-correct, three both-incorrect and four regressed. The fixture reports fixed synthetic usage, so no real cost reduction is claimed from shorter context.

Exact IDs/manifests, timing distributions, bootstrap intervals, policies and rule decisions are archived in [session-09-results.json](../evaluation/session-09-results.json). Local HTTP p95 values and mean-difference intervals are descriptive, hardware/load-sensitive, small-sample evidence. They are not production model latency or a statistically significant release claim. Intervals never automatically gate release.

Final baseline run `0002c8ab390947c199f1b2839a6c8e36`; context comparison `93f78066ea994914dc7d13d449440f10`. Regressed IDs are support-004/026/027/028, of which026/027/028 are independently protected. Baseline/context/retrieval successful HTTP p95 observations were13.658620/7.902790/9.728750ms (35 samples each), not model-speed claims. The candidate correctness mean-difference diagnostic interval was[-0.228571,-0.028571]; identical-run interval[0,0] was labeled degenerate. The final Python source checksum matched the measured frozen source before commit, and screenshots were inspected.

### Why the three baseline cases fail

- support-025 needs D01/D02, but actual top3 are D07/D02/D11; D01 is absent even before context selection. The refund-window fact is missing: retrieval loss, plus a true but unnecessary refund-processing claim. Labels were not edited to improve the score.
- support-029 needs D08/D09. Ranking D09/D02/D08 retrieves both, but context2 supplies D09/D02 and drops D08; cancellation fact is missing. Context assembly causes the loss.
- support-030 needs D01/D02/D06. Ranking D06/D02/D01 retrieves all, context2 drops D01, so the refund-window fact is missing. Groundedness remains1 while completeness/correctness fails.

### Policy results

- Identical actual runs: offline-v1 PASS. Actual context1 and retrievalK1: FAIL due correctness degradation/floor and critical regressions.
- Same identical comparison under stricter v2 floor0.99: FAIL, with both policy-version decisions retained. No measured score was edited.
- Actual missing-usage run: cost policy INCONCLUSIVE; full cost is null, known subtotal/coverage explicit. Relative change is undefined for zero baseline.
- Production-oriented live/semantic-validation profile: INCONCLUSIVE for fixture evidence. Offline PASS is not production approval.
- Separate controlled gate fixture improves average correctness while regressing a predesignated critical case and returns FAIL. Another controlled dataset incompatibility returns INCONCLUSIVE. These fixtures have conspicuous test provenance, no fabricated trace links and are not persisted as measured application runs.

## Verification and persistence

207 Go cases/vet and Linux race verification passed. Python85 tests including actual PostgreSQL persistence passed; Ruff and pip check passed. Frontend57 tests, lint/typecheck and production build passed. Browser29 scenarios passed, including actual regressed baseline/candidate trace navigation, PASS/FAIL/INCONCLUSIVE, mobile comparison and prior trace/log/evaluation/AgentOps/outage behavior. Actual PostgreSQL tests verified manifest/case pagination, immutable policy-version rejection, new-version history, idempotent decisions and rejected history updates. Earlier storage/replay/privacy, mixed-signal and isolated SIGTERM/SIGKILL regressions passed.

Docker Desktop initially needed starting; no initial unavailable-state attempt is claimed as successful evidence. One verification command overlapped the browser's intentional PostgreSQL outage and another invoked pytest from the wrong directory; reran successfully after recovery/from the Python project directory. The existing Starlette TestClient HTTPX deprecation warning remains non-failing. Fixed-cutoff separation was necessary to avoid comparing Recall@1 with Recall@3. No secrets, `.env`, local notes or artifacts were staged.

## Commands and limitations

Start the healthy Compose stack and fixture Python app, then root `scripts/session09-experiments.ps1`. Direct CLI: from `examples/agent-demo`, `python -m evaluation.compare_cli BASELINE_ID CANDIDATE_ID --policy evaluation/policies/offline-v1.json --report ../../artifacts/session09/manual.json`, with server-side PostgreSQL password. `python -m evaluation.runner --k 1 --metric-k 3 --context-limit 2` explicitly decouples retrieval/measurement. Root README lists checks; intentional DB outages must not overlap DB-dependent tests.

Go APIs remain read-only, bounded and parameterized; source content is synthetic metadata, not copied ClickHouse telemetry. Runtime/label/seed/code/pricing provenance matters. Missing entire telemetry can invalidate observed accounting even when visible attempts are complete. Synthetic/paraphrased cases and author-only labels limit statistical/generalization claims. Production TLS/auth/tenant controls, live validation and independent semantic review remain future work. Session10 may build service analytics; nothing from it is implemented here. Session3 theory remains separately pending.

## Five assessment answers

1. No: changing prompt, retriever and model together confounds attribution. Hold dataset/corpus/labels, prompt, retriever, generation/tool/runtime/measurement settings and evaluator fixed while changing one factor, or design an explicitly analyzed factorial experiment. Freeze checksums/versions, actual case membership, mode/provider/model, cutoff/context, source/build identity, pricing and scoring definitions. A higher aggregate alone cannot identify the responsible component.
2. Correctness28/35=80%;32/35=91.428571%; improvement11.428571 percentage points; relative improvement4/28=14.285714%. Latency increase200/800=25%; cost increase0.0008/0.004=20%. Positive quality delta is desirable, but positive latency/cost delta is worse under lower-is-better objectives. Zero baseline makes relative change undefined.
3. Baseline3/6=50%; candidate4/6=66.666667%. Two improved(C,D), one regressed(B), three unchanged(A,E,F). A protected B regression violates the zero-critical-regression rule even though the average improves. Bootstrap must sample paired case differences, preserving common question difficulty and alignment rather than resampling independent survivor cohorts.
4. Correctness94%≥90% PASS; Recall degradation0.01≤0.02 PASS; latency100/800=12.5%≤20% PASS; cost0.0008/0.004=20%>15% FAIL; no critical regressions PASS. Overall FAIL. If cost is missing and no other mandatory violation exists, overall INCONCLUSIVE, never automatic PASS.
5. Candidate-only successful-case means can improve merely because the hardest five cases timed out. Report all35 attempts, five failures, own-cohort means and matched30-case quality/latency separately, with eligibility/N/A/scored coverage and failure transitions. Compare baseline restricted to those same30 IDs for paired deltas, preserve failed IDs outside quality scoring, and leave unknown usage/cost incomplete rather than zero. Fixture PASS establishes only its declared offline policy, not real inference, calibrated semantics, representative load, billing, reliability, security or deployment readiness.
