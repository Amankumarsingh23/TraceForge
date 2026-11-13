"""Opt-in tests against the provisioned PostgreSQL, never paid provider calls."""

import os
import uuid

import psycopg
import pytest

from evaluation.dataset import load
from evaluation.metrics import aggregate
from evaluation.store import Store

pytestmark = pytest.mark.skipif(
    os.getenv("TRACEFORGE_PG_TEST") != "1", reason="requires explicit local PostgreSQL integration"
)


def test_postgres_transactions_immutable_versions_and_rerun_isolation():
    dataset, _, digest = load()
    store = Store()
    store.migrate()
    store.migrate()
    first, second = uuid.uuid4().hex, uuid.uuid4().hex
    try:
        store.start(dataset, digest, first, {"purpose": "storage_integration", "mode": "fixture"})
        store.start(dataset, digest, second, {"purpose": "storage_integration", "mode": "fixture"})
        result = {
            "status": "execution_failed",
            "semantic_status": "execution_failed",
            "metrics": {},
            "error": "controlled_no_execution",
            "trace_visibility": "pending",
        }
        store.save(first, dataset, dataset["cases"][0], result)
        with pytest.raises(psycopg.errors.UniqueViolation):
            store.save(first, dataset, dataset["cases"][0], result)
        row = store.connection.execute(
            "SELECT attempted,failed FROM eval_runs WHERE run_id=%s", (first,)
        ).fetchone()
        assert row == (1, 1)
        assert (
            store.connection.execute(
                "SELECT count(*) FROM eval_case_results WHERE run_id=%s", (second,)
            ).fetchone()[0]
            == 0
        )
        with pytest.raises(ValueError, match="immutable"):
            store.start(dataset, "f" * 64, uuid.uuid4().hex, {})
        store.finish(first, aggregate([result]))
        with pytest.raises(ValueError, match="not writable"):
            store.save(first, dataset, dataset["cases"][1], result)
        store.finish(second, {}, "interrupted")
    finally:
        store.close()
