# Explicit local bootstrap. Never prints actual keys or overwrites .env.
$ErrorActionPreference='Stop'
$env:ALPHA_INGEST_KEY='bootstrap-parse-only'
$env:BETA_INGEST_KEY='bootstrap-parse-only'
New-Item -ItemType Directory -Force artifacts/session14/private | Out-Null
docker compose -f compose.yaml -f compose.secure.yaml build
if ($LASTEXITCODE -ne 0){throw 'Secure image build failed'}
docker compose -f compose.yaml -f compose.secure.yaml --profile tools run --rm secure-bootstrap
if ($LASTEXITCODE -ne 0){throw 'Credential bootstrap failed'}
$keys=Get-Content artifacts/session14/private/credentials.json | ConvertFrom-Json
$env:ALPHA_INGEST_KEY=$keys.keys.alpha.ingest
$env:BETA_INGEST_KEY=$keys.keys.beta.ingest
foreach($tenant in @('alpha','beta')){
 $migration="SET search_path=tf_tenant_$tenant;`n"+((Get-ChildItem examples/agent-demo/evaluation/migrations/*.sql | Sort-Object Name | Get-Content -Raw) -join "`n")
 $migration | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U traceforge -d traceforge
 if($LASTEXITCODE -ne 0){throw 'Tenant evaluation schema migration failed'}
}
docker compose -f compose.yaml -f compose.secure.yaml up -d
if($LASTEXITCODE -ne 0){throw 'Secure stack start failed'}
Write-Output 'Started TLS tenant demo. Private generated credentials/certificates remain under ignored artifacts/session14/private.'
