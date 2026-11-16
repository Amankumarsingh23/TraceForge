"""Concurrent real trace/log/metric ingestion and actual Go API reads."""

import concurrent.futures
import json
import time
import urllib.parse
import urllib.request
from datetime import UTC, datetime, timedelta

from benchmark import OUT, save
from sampling import workload


def main():
    now = datetime.now(UTC)
    query = urllib.parse.urlencode(
        {
            "from": now.isoformat().replace("+00:00", "Z"),
            "to": (now + timedelta(minutes=1)).isoformat().replace("+00:00", "Z"),
        }
    )
    paths = [
        "/api/v1/traces?" + query + "&service=gateway",
        "/api/v1/logs?" + query + "&service=gateway",
        "/api/v1/metrics/query?" + query + "&service=payment",
    ]
    reads = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        future = pool.submit(workload, "mixed", "http://127.0.0.1:4317", "")
        for i in range(60):
            path = paths[i % 3]
            start = time.perf_counter()
            try:
                with urllib.request.urlopen(
                    "http://127.0.0.1:8082" + path, timeout=5
                ) as response:
                    raw = response.read(8 << 20)
                    reads.append(
                        {
                            "path": path,
                            "status": response.status,
                            "bytes": len(raw),
                            "ms": (time.perf_counter() - start) * 1000,
                        }
                    )
            except OSError as exc:
                reads.append(
                    {
                        "path": path,
                        "error": str(exc),
                        "ms": (time.perf_counter() - start) * 1000,
                    }
                )
            time.sleep(0.1)
        result = future.result()
    report = {"workload": result, "reads": reads}
    (OUT / "mixed.json").write_text(json.dumps(report, indent=2))
    save()
    assert all(x.get("status") == 200 for x in reads), (
        "mixed query failures retained in report"
    )
    assert len(result["stored"]) == 100


if __name__ == "__main__":
    main()
