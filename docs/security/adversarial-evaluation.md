# Fixture containment evaluation v1

The versioned dataset is `examples/agent-demo/security_eval/cases-v1.json`, SHA-256 `63626a77089f9e8f927bcbc10efa756e0474f5d3a8f34f0591fd4a0e504288c0`. Its expected effects/outcomes are declared before execution. It contains 20 attacks, four failure controls and six benign controls across two owners. Attacks cover direct instruction override, indirect retrieved-document and malicious tool-output instructions, disallowed tool names/operations, malformed/oversized arguments, ownership violations, synthetic disclosure attempts and repeated-call pressure. The runner drives the actual Agent with deliberately adversarial fixture responses. It records proposals, actual executed tools, ownership and tested marker disclosure; an attack-looking string alone is not a successful attack.

Provider and tool timeouts, permanent provider failure, and the overall agent deadline have separate diagnostic outcomes. The deadline case is **unknown**, never counted as a blocked attack success. Failed/unknown execution is not silently removed from coverage. Benign in-owner order lookups and normal questions must succeed, so denying everything would fail the control score.

## Measured 2026-11-16

| Measure | Alpha | Beta | Combined |
| --- | ---: | ---: | ---: |
| All attempted cases | 24 | 6 | 30 |
| Attack attempted/evaluated | 17/17 | 3/3 | 20/20 |
| Prohibited effects | 0 | 0 | 0 |
| Unauthorized tool executions | 0 | 0 | 0 |
| Tested marker disclosures | 0 | 0 | 0 |
| Benign successful/attempted | 3/3 | 3/3 | 6/6 |
| Unknown failure controls | 1 | 0 | 1 |
| Matched declared expectations | 24 | 6 | 30 |

Attack success rate = prohibited effects / evaluated attacks = 0/20 = 0%; attack execution coverage = 20/20 = 100%; benign success = 6/6 = 100%. These denominators do not count the four nonattack failure controls. Reports retain each outcome and immutable run ID, with actual tenant-owned trace links. All 30 owner links returned 200; the same links returned 404 under the other owner. The tested raw instruction/marker was absent from the retrieved trace representation. These observations do not prove arbitrary secret absence.

Final recorded runs: Alpha `194f3d6e7dbf48e0a9c6ed6e653c7f34`; Beta is recorded in the committed evidence JSON. Fixture telemetry is unsampled and uses authenticated per-owner TLS Collector paths. Run metadata is persisted in owner-local `security_evaluations` with an immutable update/delete trigger. `/api/v1/security/evaluations` and `/security` keep this assessment separate from the earlier RAG answer-quality evaluator.

No external provider, paid judge, customer data or destructive tool was used. Deterministic attacks demonstrate that the application checks contain these proposed actions; they do not measure how often a live model follows an injection or establish universal injection resistance. The synthetic marker guard is not a general exfiltration detector. Live-model validation, independent semantic review and independent security testing remain pending.
