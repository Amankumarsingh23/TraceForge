# Explicit, non-destructive existing-log index backfill. No retention changes.
$ErrorActionPreference = 'Stop'
& docker compose exec -T clickhouse sh -c 'exec clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --query "ALTER TABLE traceforge.logs MATERIALIZE INDEX s13_trace_bloom SETTINGS mutations_sync=2"'
if ($LASTEXITCODE -ne 0) { throw 'Log trace index materialization failed; original rows remain' }
