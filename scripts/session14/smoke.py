"""Actual TLS/auth/tenant/pressure/outage integration. Local synthetic targets only.

Credentials are read from ignored private files and never serialized to reports.
Run after the secure Compose profile and schema bootstrap. --outages is explicit.
"""

import argparse
import concurrent.futures
import hashlib
import json
import os
import secrets
import ssl
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

import grpc
import psycopg
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import (
    ExportLogsServiceRequest,
)
from opentelemetry.proto.collector.logs.v1.logs_service_pb2_grpc import LogsServiceStub
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import (
    ExportMetricsServiceRequest,
)
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2_grpc import (
    MetricsServiceStub,
)
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)
from opentelemetry.proto.collector.trace.v1.trace_service_pb2_grpc import (
    TraceServiceStub,
)

OUT = Path("artifacts/session14")
PRIVATE = json.loads((OUT / "private/credentials.json").read_text())
KEYS = PRIVATE["keys"]
CA = (OUT / "private/certs/server.crt").read_bytes()
TLS = ssl.create_default_context(cafile=str(OUT / "private/certs/server.crt"))
CHANNEL = grpc.secure_channel(
    "127.0.0.1:4334", grpc.ssl_channel_credentials(root_certificates=CA)
)
TRACE = TraceServiceStub(CHANNEL)
LOG = LogsServiceStub(CHANNEL)
METRIC = MetricsServiceStub(CHANNEL)
REPORT = {"checks": [], "pressure": {}, "outages": {}}
TAG = secrets.token_hex(5)


def save():
    (OUT / "boundary.json").write_text(json.dumps(REPORT, indent=2))


def http(path, tenant="alpha", scope="read", body=None, key=None, retry=True):
    for attempt in range(8):
        req = urllib.request.Request(
            "https://127.0.0.1:8444" + path,
            None if body is None else json.dumps(body).encode(),
            headers={
                "Authorization": "Bearer "
                + (key if key is not None else KEYS[tenant][scope]),
                "Content-Type": "application/json",
            },
        )
        try:
            with urllib.request.urlopen(req, context=TLS, timeout=8) as response:
                return response.status, json.load(response)
        except urllib.error.HTTPError as exc:
            result = exc.code, json.load(exc)
            if exc.code == 429 and retry and attempt < 7:
                time.sleep(0.2)
                continue
            return result
    raise RuntimeError("HTTP admission retry exhausted")


def check(label, condition, **evidence):
    REPORT["checks"].append({"label": label, "passed": bool(condition), **evidence})
    save()
    if not condition:
        raise AssertionError(label)


def export(stub, request, tenant="alpha", scope="ingest"):
    try:
        result = stub.Export(
            request,
            metadata=(("authorization", "Bearer " + KEYS[tenant][scope]),),
            timeout=8,
        )
        return "OK", result
    except grpc.RpcError as exc:
        return exc.code().name, None


def traces(tid, duration, marker="", claim=None):
    req = ExportTraceServiceRequest()
    group = req.resource_spans.add()
    for k, v in {
        "service.name": "s14-payment",
        "service.namespace": "security-" + TAG,
        "deployment.environment.name": "local",
        "traceforge.workload.kind": "synthetic",
        "service.instance.id": "same-writer",
    }.items():
        group.resource.attributes.add(key=k).value.string_value = v
    if claim:
        group.resource.attributes.add(key="tenant_id").value.string_value = claim
    span = group.scope_spans.add().spans.add(
        trace_id=bytes.fromhex(tid),
        span_id=bytes.fromhex("0000000000000001"),
        name="GET /charge",
        kind=2,
        start_time_unix_nano=time.time_ns() - 1000000000,
    )
    span.end_time_unix_nano = span.start_time_unix_nano + duration
    span.status.code = 2 if duration == 900000000 else 0
    if marker:
        span.attributes.add(key="safe.detail").value.string_value = marker
    return req


def logs(tid, text):
    req = ExportLogsServiceRequest()
    group = req.resource_logs.add()
    group.resource.attributes.add(key="service.name").value.string_value = "s14-payment"
    record = group.scope_logs.add().log_records.add(
        time_unix_nano=time.time_ns(),
        observed_time_unix_nano=time.time_ns(),
        trace_id=bytes.fromhex(tid),
        span_id=bytes.fromhex("0000000000000001"),
        severity_number=9,
        severity_text="INFO",
    )
    record.body.string_value = text
    return req


def metrics(total, bad, cutoff, service="s14-payment"):
    req = ExportMetricsServiceRequest()
    group = req.resource_metrics.add()
    for k, v in {
        "service.name": service,
        "service.namespace": "security-" + TAG,
        "deployment.environment.name": "local",
        "traceforge.workload.kind": "synthetic",
        "service.instance.id": "same-writer",
    }.items():
        group.resource.attributes.add(key=k).value.string_value = v
    scope = group.scope_metrics.add()
    scope.scope.name = "traceforge.demo"
    scope.scope.version = "0.1.0"
    for name, value in (
        ("traceforge.requests.completed", total),
        ("traceforge.requests.errors", bad),
    ):
        metric = scope.metrics.add(name=name, unit="{request}")
        metric.sum.aggregation_temporality = 1
        metric.sum.is_monotonic = True
        point = metric.sum.data_points.add(
            start_time_unix_nano=(cutoff - 9) * 1000000000,
            time_unix_nano=(cutoff - 1) * 1000000000,
            as_int=value,
        )
        for k, v in {"http.route": "/charge", "http.request.method": "GET"}.items():
            point.attributes.add(key=k).value.string_value = v
    return req


def pg():
    return psycopg.connect(
        host="127.0.0.1",
        dbname="traceforge",
        user="traceforge",
        password=os.environ["POSTGRES_PASSWORD"],
        connect_timeout=2,
        autocommit=True,
        options="-c statement_timeout=2500",
    )


def metadata(beta_trace):
    ids = {k: secrets.token_hex(16) for k in ("run", "comparison", "gate", "security")}
    dataset = "s14-" + TAG
    digest = hashlib.sha256(dataset.encode()).hexdigest()
    with pg() as conn:
        conn.execute("SET search_path=tf_tenant_beta")
        conn.execute(
            "INSERT INTO eval_datasets VALUES(%s,'1',%s,%s,%s)",
            (
                dataset,
                digest,
                digest,
                json.dumps({"facts": {}, "fixture": "beta-only"}),
            ),
        )
        conn.execute("INSERT INTO eval_cases VALUES(%s,'1','C01','{}')", (dataset,))
        conn.execute(
            "INSERT INTO eval_runs(run_id,dataset_id,dataset_version,dataset_checksum,configuration,status,attempted,succeeded,scored) VALUES(%s,%s,'1',%s,'{}','completed',1,1,1)",
            (ids["run"], dataset, digest),
        )
        conn.execute(
            "INSERT INTO eval_case_results VALUES(%s,'C01',%s,'1','succeeded',%s,%s,%s)",
            (
                ids["run"],
                dataset,
                beta_trace,
                ids["run"],
                json.dumps({"fixture": "BETA_PRIVATE_" + TAG}),
            ),
        )
        conn.execute(
            "INSERT INTO eval_comparisons(comparison_id,baseline_run_id,candidate_run_id,engine_version,result) VALUES(%s,%s,%s,'security-isolation-fixture',%s)",
            (
                ids["comparison"],
                ids["run"],
                ids["run"],
                json.dumps({"fixture": "BETA_PRIVATE_" + TAG}),
            ),
        )
        conn.execute(
            "INSERT INTO gate_policies VALUES(%s,'1',%s,'{}')", (dataset, digest)
        )
        conn.execute(
            "INSERT INTO gate_decisions(decision_id,comparison_id,policy_checksum,outcome,result) VALUES(%s,%s,%s,'INCONCLUSIVE',%s)",
            (
                ids["gate"],
                ids["comparison"],
                digest,
                json.dumps({"fixture": "BETA_PRIVATE_" + TAG}),
            ),
        )
        conn.execute(
            "INSERT INTO security_evaluations VALUES(%s,%s,now())",
            (
                ids["security"],
                json.dumps(
                    {
                        "run_id": ids["security"],
                        "tenant": "beta",
                        "mode": "security-isolation-fixture",
                        "metrics": {},
                        "cases": [],
                    }
                ),
            ),
        )
    return ids


def compose(*args):
    env = dict(
        os.environ,
        ALPHA_INGEST_KEY=KEYS["alpha"]["ingest"],
        BETA_INGEST_KEY=KEYS["beta"]["ingest"],
    )
    subprocess.run(
        ["docker", "compose", "-f", "compose.yaml", "-f", "compose.secure.yaml", *args],
        env=env,
        check=True,
        capture_output=True,
        timeout=45,
    )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--outages", action="store_true")
    args = parser.parse_args()
    shared = secrets.token_hex(16)
    beta = secrets.token_hex(16)
    marker = "BETA_PRIVATE_" + TAG
    cutoff = int(time.time())
    REPORT.update(tag=TAG, shared_trace=shared, beta_only_trace=beta, cutoff=cutoff)
    check("missing HTTP credential", http("/api/v1/traces", key="")[0] == 401)
    check(
        "invalid HTTP credential",
        http("/api/v1/traces", key="tf_" + "0" * 64)[0] == 401,
    )
    check(
        "read cannot ingest",
        export(TRACE, traces(shared, 100000000), scope="read")[0]
        == "PERMISSION_DENIED",
    )
    check("ingest cannot read", http("/api/v1/traces", scope="ingest")[0] == 403)
    check(
        "foreign tenant payload denied",
        export(TRACE, traces(shared, 100000000, claim="beta"))[0]
        == "PERMISSION_DENIED",
    )
    a = traces(shared, 100000000, "ALPHA_PRIVATE_" + TAG)
    b = traces(shared, 900000000, marker)
    check("alpha synchronous ingest", export(TRACE, a)[0] == "OK")
    check("beta synchronous ingest", export(TRACE, b, "beta")[0] == "OK")
    check(
        "beta unique object ingest",
        export(TRACE, traces(beta, 900000000, marker), "beta")[0] == "OK",
    )
    check(
        "alpha log ingest", export(LOG, logs(shared, "ALPHA_PRIVATE_" + TAG))[0] == "OK"
    )
    check("beta log ingest", export(LOG, logs(beta, marker), "beta")[0] == "OK")
    for tenant, total, bad in (("alpha", 3, 1), ("beta", 7, 2)):
        check(
            tenant + " metric ingest",
            export(METRIC, metrics(total, bad, cutoff), tenant)[0] == "OK",
        )
    for tenant, duration in (("alpha", 100000000), ("beta", 900000000)):
        code, detail = http("/api/v1/traces/" + shared, tenant)
        check(
            tenant + " same IDs isolated",
            code == 200 and int(detail["root_spans"][0]["duration_ns"]) == duration,
            duration=detail.get("root_spans", [{}])[0].get("duration_ns"),
        )
    code, body = http("/api/v1/traces/" + beta)
    check("foreign unique trace hidden", code == 404 and marker not in json.dumps(body))
    channel = grpc.secure_channel(
        "127.0.0.1:4331", grpc.ssl_channel_credentials(root_certificates=CA)
    )
    other = TraceServiceStub(channel)
    check(
        "alpha key rejected at beta Collector",
        export(other, traces(shared, 100000000))[0] == "UNAUTHENTICATED",
    )
    channel.close()
    collector = grpc.secure_channel(
        "127.0.0.1:4330", grpc.ssl_channel_credentials(root_certificates=CA)
    )
    check(
        "authenticated alpha Collector path",
        export(TraceServiceStub(collector), a)[0] == "OK",
    )
    collector.close()
    ids = metadata(beta)
    REPORT["metadata_ids"] = ids
    for path in (
        "/api/v1/evaluations/" + ids["run"],
        "/api/v1/evaluations/" + ids["run"] + "/cases/C01",
        "/api/v1/comparisons/" + ids["comparison"],
        "/api/v1/gates/" + ids["gate"],
        "/api/v1/security/evaluations/" + ids["security"],
    ):
        code, body = http(path)
        check(
            "cross-tenant metadata hidden " + path,
            code == 404 and marker not in json.dumps(body),
        )
        check("owner metadata readable " + path, http(path, "beta")[0] == 200)
    query = urllib.parse.urlencode(
        {
            "from": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(cutoff - 20)),
            "to": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(cutoff + 10)),
        }
    )
    for path in (
        "/api/v1/logs?trace_id=" + beta + "&" + query,
        "/api/v1/traces/" + beta + "/logs?" + query,
        "/api/v1/metrics/query?" + query,
        "/api/v1/metrics/series?" + query,
        "/api/v1/services?" + query,
        "/api/v1/service-graph?" + query,
        "/api/v1/evaluations",
        "/api/v1/experiments",
        "/api/v1/comparisons",
        "/api/v1/slos",
        "/api/v1/alerts",
        "/api/v1/provider-reliability",
    ):
        code, body = http(path)
        check(
            "tenant scoped list " + path, code == 200 and marker not in json.dumps(body)
        )
    definition = {
        "id": "s14-" + TAG,
        "version": 1,
        "description": "Authenticated synthetic matched intervals",
        "kind": "availability",
        "source_version": "otlp-metrics/v1",
        "service": "s14-payment",
        "namespace": "security-" + TAG,
        "environment": "local",
        "mode": "synthetic",
        "provider": "",
        "route": "/charge",
        "expected_writers": ["same-writer"],
        "target": "0.9",
        "window_seconds": 20,
        "min_count": 1,
        "min_coverage": "0.3",
        "rules": [
            {
                "id": "fast",
                "version": 1,
                "long_seconds": 20,
                "short_seconds": 10,
                "threshold": "2",
                "severity": "critical",
                "confirm": 1,
                "recover": 1,
            }
        ],
    }
    check(
        "read cannot manage SLO", http("/api/v1/manage/slos", body=definition)[0] == 403
    )
    for tenant, total, bad in (("alpha", "3", "1"), ("beta", "7", "2")):
        check(
            tenant + " frozen SLO registered",
            http("/api/v1/manage/slos", tenant, "manage", definition)[0] == 200,
        )
        code, result = http(
            "/api/v1/manage/evaluate",
            tenant,
            "manage",
            {
                "key": definition["id"] + "-v1",
                "cutoff": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(cutoff)),
                "revision": 1,
            },
        )
        check(
            tenant + " SLO counts isolated",
            code == 200
            and result["overall"]["evidence"]["total"] == total
            and result["overall"]["evidence"]["bad"] == bad
            and result["rules"][0]["decision"] == "BREACH",
        )
        REPORT[tenant + "_evaluation"] = result
    REPORT["slo_key"] = definition["id"] + "-v1"
    for tenant in ("alpha", "beta"):
        code, alerts = http("/api/v1/alerts", tenant)
        incident = next(
            x for x in alerts["alerts"] if x["slo_key"] == REPORT["slo_key"]
        )
        REPORT[tenant + "_incident"] = incident
    # Revocation is online and owner-bound; no authorization cache grace period.
    key = "tf_" + secrets.token_hex(32)
    digest = hashlib.sha256(key.encode()).digest()
    cid = digest[:12].hex()
    with pg() as conn:
        conn.execute(
            "INSERT INTO public.security_credentials(id,tenant_id,verifier,scopes) VALUES(%s,'alpha',%s,ARRAY['read'])",
            (cid, digest),
        )
    check("new credential active", http("/api/v1/security/session", key=key)[0] == 200)
    with pg() as conn:
        conn.execute(
            "UPDATE public.security_credentials SET revoked=true WHERE id=%s", (cid,)
        )
    check("revocation immediate", http("/api/v1/security/session", key=key)[0] == 401)
    time.sleep(2)

    def flood(n):
        return export(TRACE, traces(secrets.token_hex(16), 1000000))[0]

    started = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=10) as pool:
        futures = [pool.submit(flood, n) for n in range(50)]
        beta_results = []
        for _ in range(5):
            start = time.perf_counter()
            code, _ = export(TRACE, traces(secrets.token_hex(16), 1000000), "beta")
            beta_results.append(
                {"status": code, "ms": (time.perf_counter() - start) * 1000}
            )
            time.sleep(0.15)
        alpha = [f.result() for f in futures]
    REPORT["pressure"] = {
        "alpha_offered": 50,
        "alpha_statuses": {x: alpha.count(x) for x in set(alpha)},
        "beta": beta_results,
        "elapsed_seconds": time.perf_counter() - started,
    }
    check(
        "bounded pressure admission and beta progress",
        alpha.count("RESOURCE_EXHAUSTED") > 0
        and any(x["status"] == "OK" for x in beta_results),
    )
    check(
        "private diagnostics require manage",
        http("/api/v1/manage/diagnostics")[0] == 403,
    )
    REPORT["diagnostics"] = http("/api/v1/manage/diagnostics", scope="manage")[1]
    if args.outages:
        try:
            compose("stop", "postgres")
            code, _ = http("/api/v1/security/session")
            REPORT["outages"]["postgres_http"] = code
            status, _ = export(TRACE, a)
            REPORT["outages"]["postgres_grpc"] = status
            check(
                "PostgreSQL failure closes new authorization",
                code == 503 and status == "UNAVAILABLE",
            )
        finally:
            compose("start", "postgres")
            time.sleep(3)
        code, incident = http("/api/v1/alerts/" + REPORT["alpha_incident"]["id"])
        check(
            "auth recovery preserves firing",
            code == 200 and incident["incident"]["state"] == "FIRING",
        )
        try:
            compose("stop", "clickhouse")
            status, _ = export(TRACE, a)
            REPORT["outages"]["clickhouse_grpc"] = status
            check("storage outage no false persistence ACK", status != "OK")
            code, result = http(
                "/api/v1/manage/evaluate",
                scope="manage",
                body={"key": REPORT["slo_key"]},
            )
            REPORT["outages"]["unknown_evaluation"] = result
            check(
                "ClickHouse outage persists unknown",
                code == 200 and result["status"] == "UNKNOWN",
            )
            code, incident = http("/api/v1/alerts/" + REPORT["alpha_incident"]["id"])
            check(
                "unknown never resolves firing",
                code == 200 and incident["incident"]["state"] == "FIRING",
            )
        finally:
            compose("start", "clickhouse")
            time.sleep(4)
        # Let old bad intervals expire; new explicit direct metric intervals recover.
        remaining = cutoff + 23 - time.time()
        if remaining > 0:
            time.sleep(min(remaining, 23))
        fresh = int(time.time())
        recovery_statuses = []
        for _ in range(8):
            status, _ = export(METRIC, metrics(10, 0, fresh))
            recovery_statuses.append(status)
            if status == "OK":
                break
            time.sleep(0.5)
        REPORT["outages"]["recovery_ingest_statuses"] = recovery_statuses
        check("fresh metric recovery ingest", "OK" in recovery_statuses)
        code, result = http(
            "/api/v1/manage/evaluate",
            scope="manage",
            body={
                "key": REPORT["slo_key"],
                "cutoff": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(fresh)),
            },
        )
        check(
            "valid recovery measured",
            code == 200 and result["rules"][0]["decision"] == "HEALTHY",
        )
        code, incident = http("/api/v1/alerts/" + REPORT["alpha_incident"]["id"])
        check(
            "one incident two lifecycle events",
            code == 200
            and incident["incident"]["state"] == "RESOLVED"
            and len(incident["events"]) == 2,
        )
        compose("restart", "secure-gateway")
        time.sleep(2)
        check(
            "credential remains scoped after gateway restart",
            http("/api/v1/traces/" + beta)[0] == 404,
        )
        check("replay accepted within same tenant", export(TRACE, a)[0] == "OK")
        code, detail = http("/api/v1/traces/" + shared)
        check(
            "replay logical identity stable",
            code == 200 and detail["diagnostics"]["unique_spans"] == 1,
        )
    save()
    print(
        "PASS: "
        + str(len(REPORT["checks"]))
        + " actual TLS/tenant checks; no credentials printed"
    )


if __name__ == "__main__":
    try:
        main()
    finally:
        save()
