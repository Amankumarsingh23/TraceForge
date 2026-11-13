import argparse
import json
import uuid
from pathlib import Path

from psycopg.types.json import Jsonb

from .comparison import compare
from .dataset import checksum
from .gates import decide, validate
from .store import Store


def read_run(store, run_id):
    row = store.connection.execute(
        "SELECT configuration,status,attempted FROM eval_runs WHERE run_id=%s", (run_id,)
    ).fetchone()
    if row is None:
        raise ValueError("run not found")
    results = [
        r[0]
        for r in store.connection.execute(
            "SELECT result FROM eval_case_results WHERE run_id=%s ORDER BY case_id LIMIT 51",
            (run_id,),
        ).fetchall()
    ]
    if len(results) > 50 or len(results) != row[2]:
        raise ValueError("run result count invalid")
    return {"run_id": run_id, "configuration": row[0], "status": row[1], "results": results}


def persist(store, comparison, policy):
    decision = decide(comparison, policy)
    digest = validate(policy)
    comparison_id = checksum(
        {
            "baseline": comparison["baseline_run_id"],
            "candidate": comparison["candidate_run_id"],
            "engine": comparison["engine_version"],
            "result": comparison,
        }
    )[:32]
    decision_id = uuid.uuid4().hex
    with store.connection.transaction():
        store.connection.execute(
            "INSERT INTO eval_comparisons(comparison_id,baseline_run_id,candidate_run_id,engine_version,result) VALUES(%s,%s,%s,%s,%s) ON CONFLICT DO NOTHING",
            (
                comparison_id,
                comparison["baseline_run_id"],
                comparison["candidate_run_id"],
                comparison["engine_version"],
                Jsonb(comparison),
            ),
        )
        store.connection.execute(
            "INSERT INTO gate_policies VALUES(%s,%s,%s,%s) ON CONFLICT DO NOTHING",
            (policy["policy_id"], policy["version"], digest, Jsonb(policy)),
        )
        existing = store.connection.execute(
            "SELECT checksum FROM gate_policies WHERE policy_id=%s AND version=%s",
            (policy["policy_id"], policy["version"]),
        ).fetchone()
        if existing[0] != digest:
            raise ValueError("immutable policy version mismatch")
        store.connection.execute(
            "INSERT INTO gate_decisions(decision_id,comparison_id,policy_checksum,outcome,result) VALUES(%s,%s,%s,%s,%s) ON CONFLICT(comparison_id,policy_checksum) DO NOTHING",
            (decision_id, comparison_id, digest, decision["outcome"], Jsonb(decision)),
        )
        decision_id = store.connection.execute(
            "SELECT decision_id FROM gate_decisions WHERE comparison_id=%s AND policy_checksum=%s",
            (comparison_id, digest),
        ).fetchone()[0]
    return {
        "comparison_id": comparison_id,
        "decision_id": decision_id,
        "comparison": comparison,
        "decision": decision,
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("baseline")
    parser.add_argument("candidate")
    parser.add_argument("--policy", type=Path, required=True)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    policy = json.loads(args.policy.read_text())
    store = Store()
    try:
        store.migrate()
        report = persist(
            store, compare(read_run(store, args.baseline), read_run(store, args.candidate)), policy
        )
        if args.report:
            args.report.write_text(json.dumps(report, indent=2))
        print(
            json.dumps(
                {k: report[k] for k in ("comparison_id", "decision_id")}
                | {"outcome": report["decision"]["outcome"]}
            )
        )
    finally:
        store.close()


if __name__ == "__main__":
    main()
