"""Actual local SDK head/tail experiment; no paid provider calls.

The Go demo uses fresh ports and namespaces. Parent selection is observed from
stored trace membership, not inferred from configured sampling percentage.
"""

import json
import os
import subprocess
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path

from benchmark import OUT, save, sql


def request(url, body=None):
    req = urllib.request.Request(
        url,
        None if body is None else json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as response:
            return response.status, json.load(response)
    except urllib.error.HTTPError as exc:
        data = exc.read().decode()
        try:
            return exc.code, json.loads(data)
        except ValueError:
            return exc.code, {"message": data}


def wait_port(url):
    for _ in range(40):
        try:
            request(url)
            return
        except OSError:
            time.sleep(0.25)
    raise RuntimeError("demo did not start")


def workload(profile, endpoint, ratio=""):
    namespace = "session13-" + profile + "-" + uuid.uuid4().hex[:12]
    env = dict(
        os.environ,
        TRACEFORGE_SERVICE_NAMESPACE=namespace,
        OTEL_TRACES_SAMPLER="parentbased_traceidratio" if ratio else "always_on",
        OTEL_TRACES_SAMPLER_ARG=ratio,
        OTEL_EXPORTER_OTLP_ENDPOINT=endpoint,
    )
    binary = str(Path("bin/shop-demo-session13.exe").resolve())
    processes, files, offered = [], [], []
    try:
        for service, port in (("payment", 8087), ("gateway", 8086)):
            log = (OUT / f"sampling-{profile}-{service}.log").open("w")
            files.append(log)
            args = [
                binary,
                "--service",
                service,
                "--port",
                str(port),
                "--metric-interval",
                "200ms",
            ]
            if service == "gateway":
                args += ["--payment-url", "http://127.0.0.1:8087"]
            processes.append(
                subprocess.Popen(
                    args,
                    env=env,
                    stdout=log,
                    stderr=log,
                    creationflags=subprocess.CREATE_NO_WINDOW,
                )
            )
        # Connection probe is a non-instrumented404, not extra checkout traffic.
        wait_port("http://127.0.0.1:8086/ready")
        wait_port("http://127.0.0.1:8087/ready")
        start = time.time()
        for i in range(100):
            kind = "normal" if i < 80 else "delayed" if i < 90 else "failed"
            suffix = (
                ""
                if kind == "normal"
                else "?delay_ms=150"
                if kind == "delayed"
                else "?fail=true"
            )
            status, body = request("http://127.0.0.1:8086/checkout" + suffix)
            assert status == (502 if kind == "failed" else 200)
            offered.append(
                {"kind": kind, "status": status, "trace_id": body["trace_id"]}
            )
        time.sleep(10)  # SDK batch timeout + tail wait + storage/export visibility.
        stored = sql(
            f"SELECT trace_id,count() spans,groupUniqArray(service_name) services,countIf(status_code=2) error_spans FROM traceforge.spans WHERE trace_id IN ({','.join(repr(x['trace_id']) for x in offered)}) GROUP BY trace_id ORDER BY trace_id"
        )
        # No filtering on resource or outcome before trace membership/canonicalization.
        points = sql(
            f"SELECT point_json FROM traceforge.metric_points WHERE service_namespace='{namespace}' ORDER BY ingested_at"
        )
        last = {}
        for row in points:
            p = json.loads(row["point_json"])
            last[p["stream_id"]] = p
        metrics = []
        for p in last.values():
            if p["name"] in (
                "traceforge.requests.completed",
                "traceforge.requests.errors",
            ):
                metrics.append(p)
        for service in ("gateway", "payment"):
            for name, expected in (
                ("traceforge.requests.completed", 100),
                ("traceforge.requests.errors", 10),
            ):
                assert (
                    sum(
                        int(p["int_value"])
                        for p in metrics
                        if p["service"] == service and p["name"] == name
                    )
                    == expected
                ), "direct metric population changed"
        result = {
            "profile": profile,
            "namespace": namespace,
            "ratio": ratio or "all SDK roots",
            "offered": offered,
            "stored": stored,
            "direct_metrics": metrics,
            "elapsed_seconds": time.time() - start,
        }
        selected = {x["trace_id"] for x in stored}
        result["retained_by_kind"] = {
            kind: sum(x["kind"] == kind and x["trace_id"] in selected for x in offered)
            for kind in ("normal", "delayed", "failed")
        }
        assert all(x["spans"] == 5 and len(x["services"]) == 2 for x in stored), (
            "incomplete propagated demo trace"
        )
        if profile == "always":
            assert len(selected) == 100
        if profile == "tail":
            assert (
                result["retained_by_kind"]["failed"] == 10
                and result["retained_by_kind"]["delayed"] == 10
            )
        return result
    finally:
        for p in processes:
            p.terminate()
            p.wait(timeout=10)
        for f in files:
            f.close()


def span(trace, sid="0000000000000001", parent="", error=False):
    t = time.time_ns()
    return {
        "traceId": trace,
        "spanId": sid,
        "parentSpanId": parent,
        "name": "s13-controlled",
        "kind": 2,
        "startTimeUnixNano": str(t),
        "endTimeUnixNano": str(t + 1000000),
        "status": {"code": 2 if error else 0},
    }


def send(spans):
    code, body = request(
        "http://127.0.0.1:4322/v1/traces",
        {
            "resourceSpans": [
                {
                    "resource": {
                        "attributes": [
                            {
                                "key": "service.name",
                                "value": {"stringValue": "s13-tail-probe"},
                            }
                        ]
                    },
                    "scopeSpans": [{"scope": {"name": "s13-probe"}, "spans": spans}],
                }
            ]
        },
    )
    assert code == 200, body


def stored(ids):
    return sql(
        f"SELECT trace_id,count() spans,countIf(status_code=2) errors FROM traceforge.spans WHERE trace_id IN ({','.join(repr(x) for x in ids)}) GROUP BY trace_id ORDER BY trace_id"
    )


def probes():
    # A late error may arrive after the normal root's drop decision was cached.
    late = [uuid.uuid4().hex for _ in range(20)]
    send([span(x) for x in late])
    time.sleep(5)
    before = stored(late)
    send([span(x, "0000000000000002", "0000000000000001", True) for x in late])
    time.sleep(5)
    after = stored(late)
    # Deliberately exceed128 buffered traces before the2s decision wait.
    pressure = [uuid.uuid4().hex for _ in range(400)]
    send([span(x, error=True) for x in pressure])
    time.sleep(6)
    pressure_stored = stored(pressure)
    with urllib.request.urlopen("http://127.0.0.1:8890/metrics", timeout=5) as response:
        (OUT / "tail-telemetry.txt").write_bytes(response.read(2 << 20))
    pending = uuid.uuid4().hex
    send([span(pending)])
    subprocess.run(
        [
            "docker",
            "compose",
            "-f",
            "compose.yaml",
            "-f",
            "compose.sampling.yaml",
            "restart",
            "sampling-collector",
        ],
        check=True,
        capture_output=True,
        timeout=30,
    )
    time.sleep(3)
    send([span(pending, "0000000000000002", "0000000000000001", True)])
    time.sleep(5)
    return {
        "late_ids": late,
        "late_before": before,
        "late_after": after,
        "pressure_offered": 400,
        "pressure_stored": pressure_stored,
        "restarted_trace": pending,
        "restarted_stored": stored([pending]),
    }


if __name__ == "__main__":
    report = {}
    try:
        for name, endpoint, ratio in (
            ("always", "http://127.0.0.1:4317", ""),
            ("head", "http://127.0.0.1:4317", "0.2"),
            ("tail", "http://127.0.0.1:4321", ""),
        ):
            report[name] = workload(name, endpoint, ratio)
            (OUT / "sampling.json").write_text(json.dumps(report, indent=2))
            print(name, report[name]["retained_by_kind"], flush=True)
        report["probes"] = probes()
    finally:
        (OUT / "sampling.json").write_text(json.dumps(report, indent=2))
        save()
