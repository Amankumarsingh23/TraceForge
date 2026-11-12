param([Parameter(Mandatory)][string]$ReportPath)
$ErrorActionPreference='Stop'
$report=Get-Content -LiteralPath $ReportPath -Raw | ConvertFrom-Json
$prefix=$report.load.config.run_id
if ($prefix -notmatch '^[0-9a-f]{16}$' -or $prefix -eq '0000000000000000') { throw 'Invalid benchmark run prefix' }
# Cleanup is scoped by prefix AND synthetic-service/tag markers. It cannot
# delete the historical gateway/payment demo just because a prefix is supplied.
$predicate="startsWith(trace_id,'$prefix') AND startsWith(service_name,'load-service-') AND position(span_attributes_json,'load.synthetic') > 0"
$sql="ALTER TABLE traceforge.spans DELETE WHERE $predicate SETTINGS mutations_sync=1; SELECT count() AS remaining_synthetic_rows FROM traceforge.spans WHERE $predicate;"
$sql | docker compose exec -T clickhouse sh -c 'clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --multiquery'
if ($LASTEXITCODE -ne 0) { throw 'Scoped benchmark cleanup failed' }
