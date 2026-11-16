# Dot-source for local test/admin commands. Never print these values.
$private = Get-Content artifacts/session14/private/credentials.json | ConvertFrom-Json
$env:ALPHA_INGEST_KEY=$private.keys.alpha.ingest
$env:BETA_INGEST_KEY=$private.keys.beta.ingest
foreach($line in Get-Content .env){if($line -match '^(CLICKHOUSE_PASSWORD|POSTGRES_PASSWORD)=(.+)$'){[Environment]::SetEnvironmentVariable($Matches[1],$Matches[2],'Process')}}
$env:TRACEFORGE_CA_FILE=(Resolve-Path artifacts/session14/private/certs/server.crt).Path
$env:TRACEFORGE_AUTH_URL='https://127.0.0.1:8444/api/v1/security/session'
$env:TRACEFORGE_AUTH_IDENTITY_URL='https://127.0.0.1:8444/api/v1/security/identity'
$env:TRACEFORGE_SECURE_OTLP_ALPHA='https://127.0.0.1:4330'
$env:TRACEFORGE_SECURE_OTLP_BETA='https://127.0.0.1:4331'
$agentKeys=@{alpha=$private.keys.alpha.ingest;beta=$private.keys.beta.ingest}|ConvertTo-Json
$agentKeys|Set-Content artifacts/session14/private/agent-ingest.json
$env:TRACEFORGE_AGENT_CREDENTIALS_FILE=(Resolve-Path artifacts/session14/private/agent-ingest.json).Path
