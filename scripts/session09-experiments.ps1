$ErrorActionPreference='Stop'
$health=Invoke-RestMethod http://127.0.0.1:8084/healthz -TimeoutSec 3
if($health.mode -ne 'fixture'){throw 'Session 9 experiments require fixture mode; no paid calls allowed'}
$savedPassword=$env:POSTGRES_PASSWORD
Push-Location (Join-Path $PSScriptRoot '../examples/agent-demo')
try {
 if(-not $env:POSTGRES_PASSWORD){foreach($line in Get-Content ../../.env){if($line -match '^POSTGRES_PASSWORD=(.+)$'){$env:POSTGRES_PASSWORD=$Matches[1]}}}
 & ./.venv/Scripts/python.exe -m evaluation.session09 --output ../../artifacts/session09
 if($LASTEXITCODE -ne 0){throw 'Experiment verification failed'}
} finally {Pop-Location;$env:POSTGRES_PASSWORD=$savedPassword}
