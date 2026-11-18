# Final feature and verification matrix

Date2026-11-18; starting commit `b70052b`. VERIFIED means an executed check supports the stated scope; FAILED means an executed trial did not satisfy it; BLOCKED means external policy/environment prevented execution; PENDING means not executed/supported. Check statuses use PASSED/FAILED/BLOCKED/NOT RUN, without treating skips as passes. Raw final logs and selected API/demo JSON are in [evidence](evidence/); dated historical evidence remains under the original session paths.

| Feature / invariant | Status and scope | Actual evidence |
| --- | --- | --- |
| Compose startup, migrations, loopback fixture setup | VERIFIED existing-volume safe rebuild/idempotent migrations; empty-volume reinstall PENDING | setup-rerun.txt; compose-start.txt |
| Persistence-confirmed direct OTLP ACK/backpressure | VERIFIED tested trace/log/metric cases, not exactly once | integration-final.txt; metric-outage-child.txt when executed; S4/S14 evidence |
| Canonical replay/conflict reconstruction | VERIFIED real stored rows and independent production-engine checks | integration-final.txt; S13/S14 storage-regression reports |
| Logs exact IDs, standalone/body/redaction | VERIFIED bounded queries/fixtures; arbitrary DLP PENDING | integration-final.txt; browser logs scenarios; S14 privacy tests |
| Counter temporality/reset/predecessor and explicit histograms | VERIFIED supported subset; exponential histogram/Summary PENDING | integration-final.txt; demo-a workload/replay; S13 cross-partition checks |
| Service RED/paired dependency graph | VERIFIED canonical observed population, not complete sampled traffic | DemoA100/5/100; analytics-refresh; api-evidence.json |
| SLO budgets and persistent incidents | VERIFIED measured healthy/breach/UNKNOWN/recovery/restart/replay | demo-a reliability-rerun.txt; incident response; postgres-tests.txt |
| Python agent/tools/evaluation/gates | VERIFIED fixtures, five35-case runs, immutable decisions and real trace links | demo-b summaries; api-evidence.json;88 Python tests |
| Go build/unit/vet | PASSED | go-unit.txt; go-vet.txt; go-build.txt |
| Linux race | PASSED Docker verify target,13 tested internal packages | linux-verify.txt; test-free command packages have no test count |
| Python Ruff/format/pip consistency | PASSED | python-lint.txt; python-format.txt; pip-check.txt |
| Python tests incl actual PostgreSQL | PASSED **88**, two deprecation warnings | python-tests.txt |
| Frontend lint/typecheck/unit/build | PASSED **70 tests /17 files**, production compilation | web-lint.txt; web-typecheck.txt; web-tests.txt; web-build.txt |
| PostgreSQL transactions/incident concurrency | PASSED fresh tagged tests, explicit real DB opt-in | postgres-tests.txt |
| Existing real Go integration suite | PASSED sequential rerun; opt-in-only skips remain explicit | integration-final.txt |
| Browser primary workflows | 48/50 in full run; corrected two-case follow-up2/2; all50 distinct scenarios covered across runs | browser-final.txt; screenshots; follow-up logs if needed |
| Alpha/Beta credential/tenant isolation | VERIFIED dated S14 lower-level59 assertions plus26 owner-local FK checks; fresh S15 secure runtime NOT RUN | ../engineering-journal/session-14-evidence/boundary.json and storage-isolation.json |
| Adversarial containment | VERIFIED dated S14 deterministic20 attacks/six benign controls; one nonattack failure unknown | S14 adversarial reports; fresh Python fixture tests |
| Authenticated browser login | **BLOCKED environment approval policy**, no bypass | S14 journal; security-and-limitations.md |
| Dependency review | VERIFIED inventory and production/full audits; findings remain OPEN | npm-current.json; npm-production-current.json; dependency-review.md |
| Cached clean dependency graph | PASSED256 packages, offline/ignore-scripts; full clean OS/lifecycle install PENDING | clean-install.txt |
| Sampling/expiry honesty | VERIFIED selected dated experiments and browser paths | S13 sampling/expiry reports and final browser cases |
| Live LLM quality/billing/independent semantic review | **PENDING / NOT RUN** | No paid calls authorized or performed |
| Production deployment/security/capacity | **PENDING / NOT RUN** | Explicit local scope; no independent certification |

## Failed trials retained

The first fresh healthy SLO window was UNKNOWN because numerator/denominator eligible intervals differed at stream startup. Six seconds of stream warm-up before measurement fixed the orchestration; assertions and eligibility logic were preserved. An ordinary integration run overlapped the SLO demo's intentional backend restarts and failed on EOF/connection resets; the subsequent sequential run passed. Initial capture/demo URL composition duplicated bucket_seconds and returned400; it was corrected to use the actual recorded query without duplicate parameters. These failures were not changed to skipped tests.

The S14 metric browser subprocess previously failed with missing Go PATH and cold compile timeout. S15 compiles while healthy, executes one test binary with bounded child/parent timeouts, writes stdout/stderr/status on errors, and restores ClickHouse in finally with readiness polling. Only an actual rerun can mark this gap closed; see the session journal for its final result.
