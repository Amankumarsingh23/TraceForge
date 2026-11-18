"""Capture bounded real API evidence after both demos, without credentials."""

import hashlib
import json
import urllib.request
from pathlib import Path

out = Path("artifacts/session15")
report = {"profile": "legacy-local-fixture", "responses": {}}


def get(name, path):
    with urllib.request.urlopen("http://127.0.0.1:8082" + path, timeout=8) as response:
        raw = response.read(8 << 20)
        assert response.status == 200
        value = json.loads(raw)
    report["responses"][name] = {
        "status": 200,
        "path": path,
        "sha256": hashlib.sha256(raw).hexdigest(),
        "body": value,
    }
    return value


workload = json.loads((out / "demo-a/workload.json").read_text())
reliability = json.loads((out / "demo-a/reliability.json").read_text())
gates = json.loads((out / "demo-b/evidence.json").read_text())
regression = json.loads((out / "demo-b/context_regression.json").read_text())
get("graph", "/api/v1/service-graph?" + workload["trace_query"])
failed = next(row for row in workload["requests"] if row["kind"] == "failed")
get("failed_trace", "/api/v1/traces/" + failed["trace_id"])
get("failed_logs", "/api/v1/traces/" + failed["trace_id"] + "/logs")
get(
    "metrics",
    "/api/v1/metrics/query?"
    + workload["query"]
    + "&name=traceforge.requests.completed",
)
get("slo", "/api/v1/slos/" + reliability["slo_key"])
get("incident", "/api/v1/alerts/" + reliability["incident_id"])
get("baseline", "/api/v1/evaluations/" + gates["run_ids"]["baseline"])
get("comparison", "/api/v1/comparisons/" + gates["context_regression"]["comparison_id"])
case = next(
    row for row in regression["comparison"]["cases"] if row["transition"] == "regressed"
)
for name in ("baseline", "candidate"):
    get(name + "_trace", "/api/v1/traces/" + case[name + "_trace_id"])
(out / "api-evidence.json").write_text(json.dumps(report, indent=2))
print(f"Captured {len(report['responses'])} actual API responses and checksums")
