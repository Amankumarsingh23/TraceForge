# Twenty technical questions with reference answers

1. **Why ClickHouse and PostgreSQL?** Raw telemetry benefits from compressed columnar scans and sort-key/index pruning. Evaluation versions, credentials and incident transitions need transactions and relational constraints. The split adds two operational dependencies but keeps each workload's guarantees explicit.

2. **Does MergeTree enforce uniqueness?** No. Sorting/primary keys support data access; append/replay rows remain physical observations. Canonical selectors choose one logical span deterministically and expose equivalence/conflict diagnostics. Never infer SQL unique-key semantics from an analytical index.

3. **What does successful OTLP export mean?** The direct Go backend waits for database confirmation before success. A timeout/lost response may follow a successful insert, so a retry can duplicate physical data. The Collector's earlier asynchronous acceptance can still be lost on restart. None establishes exactly-once delivery.

4. **How does backpressure work?** Bounded connections/streams, message/field limits, reserved work/payload capacity, finite independent signal queues, batch flush bounds and deadlines reject overload explicitly. Reservations must release on success/error/cancellation. Payload accounting is not a precise heap/RSS guarantee, and finite buffers do not make delivery durable.

5. **How do you reconstruct malformed traces?** Canonicalize IDs first, retain roots and missing-parent orphans, detect cycles, detach unsafe cycle edges for presentation and traverse iteratively with visited identities. UI diagnostics communicate uncertainty. Never invent an unobserved parent or critical path.

6. **How are logs joined?** Exact trace IDs, optionally exact span IDs, with bounded effective-time windows. Standalone, trace-only and span-associated logs remain distinct. Timing proximity is not a join; no matching record can mean missing export, expired data, a window mismatch or delayed arrival.

7. **Why not compute availability from sampled traces?** Sampling changes the observed population; tail policies often retain errors disproportionately. Trace-derived RED explicitly describes observed canonical SERVER spans. Independent request counters measure a different population, with their own writer/coverage/overflow limits. Absence never means zero errors.

8. **How are cumulative counters made into intervals?** Identify writer/stream, canonicalize replay, recover the appropriate predecessor, detect reset/start-time changes and compute compatible differences. If history is missing or overlapping intervals conflict, mark eligibility/coverage rather than fabricating increments or negative monotonic rates. Delta and cumulative point semantics differ.

9. **Why can a histogram SLI be unknown?** The requested threshold must correspond to a supported explicit boundary, with compatible bucket/count/reset/temporality and sufficient coverage. Summing incompatible buckets or treating a missing bucket as zero corrupts the denominator. Gauge and signed sums are not availability denominators.

10. **How do you calculate SLO burn accurately?** Eligible good/bad counts define the SLI, target error allowance defines budget, and exact rational arithmetic avoids floating-point boundary errors. Long/short window conditions and coverage gates determine MEASURED/BREACH/HEALTHY/UNKNOWN. A budget number without its population/window/coverage is misleading.

11. **What happens to a firing alert during an outage?** Measurement can become UNKNOWN while the persisted incident remains FIRING. A PostgreSQL transaction and uniqueness/idempotency rules prevent duplicate lifecycle transitions under retries/concurrent workers. Recovery needs qualifying measured evidence; missing data cannot resolve an incident.

12. **Why isn't tool allowlisting enough?** A valid tool and correctly formatted order ID can still refer to another tenant. The application must independently verify tool name/schema/bounds, allowed operation and object ownership before execution. Model instructions and a retrieved document cannot grant authorization.

13. **What do adversarial fixture results prove?** A malicious provider fixture proposes a controlled action; actual tool executions/disclosures determine whether its prohibited effect occurred. The tested controls contained20 evaluated attacks and allowed six benign controls in S14. This tests application containment, not the probability a live model follows an injection or arbitrary secret detection. Timeouts stay unknown/failed execution evidence, not blocked successes.

14. **How is AgentOps usage kept replay-safe?** Diagnostic provider-attempt spans own accounting counters; logical operation spans group retries without summing duplicate usage a second time. Canonical attempt identity and explicit unknown usage/pricing coverage avoid double counting. Fixture usage and hypothetical price files produce estimates, not provider invoices.

15. **What is Recall@K here?** Relevant expected document IDs retrieved in topK divided by relevant expected IDs. Precision@K uses the declaredK denominator; context coverage separately measures relevant IDs actually supplied after context truncation. Retrieval success, grounded claims, required-fact correctness and valid/correct citations can disagree and need separate denominators.

16. **Why freeze comparison inputs?** Dataset/corpus/prompt/model/retrieval/pricing/scoring configurations and checksums distinguish the populations. Paired cases and versioned policy preserve a decision's provenance even if later data changes. Missing required evidence yields INCONCLUSIVE; a local PASS is not an automatic deployment.

17. **What is the real tenant key?** Credential verification resolves the owner. Logical span identity is `(trusted tenant,trace_id,span_id)`, implemented through fixed private databases/processes and schema-only PostgreSQL search paths. All reads/joins/caches need that ownership boundary. Payload tenant claims cannot choose it; foreign IDs return scoped404.

18. **Why not add Kafka and many microservices?** No measured requirement justified their additional durability, routing, replay and operations contracts. The local core already bounds work and exposes failures. A future sustained multi-node workload might motivate durable partitioned transport, but that would require new measurements and delivery guarantees rather than architectural fashion.

19. **What exactly did the index improve?** In the same-data/cache-controlled300k log fixture, selected marks fell38→1 and rows300096→8192 with about0.43% extra disk. Ten sequential warm trials and equal result checksums support reduced scanning. API p95 did not consistently improve; merge/cache/order effects preclude a36× overall-speed claim. Service-first/codec/snapshot alternatives had documented trade-offs and were not adopted.

20. **What remains unfinished for production?** Live-model/independent semantic review, independent security review, authenticated browser policy clearance, distributed quotas/fairness, durable queue/delivery operations, per-tenant database principals/production IAM, production TLS/secrets rotation and workload-specific capacity verification. Development-tool glob advisories remain documented. Local tests and a small demonstration establish neither complete production security nor universal reliability.
