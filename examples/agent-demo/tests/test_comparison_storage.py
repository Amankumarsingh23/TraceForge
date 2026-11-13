import copy
import json
import os
from pathlib import Path

import psycopg
import pytest

from evaluation.compare_cli import persist, read_run
from evaluation.comparison import compare
from evaluation.dataset import ROOT
from evaluation.store import Store

pytestmark = pytest.mark.skipif(
    os.getenv("TRACEFORGE_PG_TEST") != "1", reason="explicit local PostgreSQL integration"
)


def test_comparison_policy_history_immutable_idempotent():
    evidence = json.loads(Path("../../artifacts/session09/evidence.json").read_text())
    store = Store()
    store.migrate()
    try:
        comparison = compare(
            read_run(store, evidence["run_ids"]["baseline"]),
            read_run(store, evidence["run_ids"]["repeat"]),
        )
        policy = json.loads((ROOT / "policies/offline-v1.json").read_text())
        first = persist(store, comparison, policy)
        second = persist(store, comparison, policy)
        assert (
            first["comparison_id"] == second["comparison_id"]
            and first["decision_id"] == second["decision_id"]
        )
        changed = copy.deepcopy(policy)
        changed["rules"][1]["threshold"] = "0.99"
        with pytest.raises(ValueError, match="immutable policy"):
            persist(store, comparison, changed)
        changed["version"] = "storage-v2"
        third = persist(store, comparison, changed)
        assert (
            third["decision_id"] != first["decision_id"] and third["decision"]["outcome"] == "FAIL"
        )
        for table, column, identity in [
            ("gate_decisions", "decision_id", first["decision_id"]),
            ("eval_comparisons", "comparison_id", first["comparison_id"]),
            (
                "experiment_configs",
                "checksum",
                comparison["baseline_configuration"]["manifest"]["checksum"],
            ),
        ]:
            with pytest.raises(psycopg.errors.RaiseException):
                # Identifiers are fixed test literals, values remain parameterized.
                store.connection.execute(
                    f"UPDATE {table} SET {column}={column} WHERE {column}=%s", (identity,)
                )
    finally:
        store.close()
