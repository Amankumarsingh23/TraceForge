"""Verify persisted fixture trace evidence through the authenticated TLS boundary."""

import json
import socket
from pathlib import Path

from smoke import http

out = Path("artifacts/session14")
results = []
for owner, other in (("alpha", "beta"), ("beta", "alpha")):
    report = json.loads((out / f"adversarial-{owner}.json").read_text())
    for case in report["cases"]:
        path = "/api/v1/traces/" + case["trace_id"]
        status, trace = http(path, owner)
        assert status == 200, (owner, case["case_id"], status)
        text = json.dumps(trace)
        assert "S14_CANARY_" not in text
        assert "ignore previous instructions" not in text.lower()
        foreign, _ = http(path, other)
        assert foreign == 404, (owner, case["case_id"], foreign)
        results.append(
            {
                "tenant": owner,
                "case_id": case["case_id"],
                "trace_id": case["trace_id"],
                "owner_status": status,
                "foreign_status": foreign,
                "tested_payload_absent": True,
            }
        )
    status, data = http("/api/v1/security/evaluations/" + report["run_id"], other)
    assert status == 404
closed = []
for port in (8082, 4319, 4317):
    with socket.socket() as probe:
        probe.settimeout(1)
        assert probe.connect_ex(("127.0.0.1", port)) != 0, port
        closed.append(port)
(out / "trace-links.json").write_text(
    json.dumps({"cases": results, "private_host_ports_closed": closed}, indent=2)
)
print(
    f"Verified {len(results)} owned trace links, foreign rejection and payload absence; private ports closed"
)
