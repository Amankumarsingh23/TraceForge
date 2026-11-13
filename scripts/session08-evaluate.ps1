param([switch]$Experiments)
$ErrorActionPreference='Stop'
$health=Invoke-RestMethod 'http://127.0.0.1:8084/healthz' -TimeoutSec 3
if($health.mode -ne 'fixture'){throw 'Offline evaluation requires fixture mode; no billable requests allowed'}
$savedPassword=$env:POSTGRES_PASSWORD
Push-Location (Join-Path $PSScriptRoot '../examples/agent-demo')
try {
    if(-not $env:POSTGRES_PASSWORD){foreach($line in Get-Content ../../.env){if($line -match '^POSTGRES_PASSWORD=(.+)$'){$env:POSTGRES_PASSWORD=$Matches[1]}}}
    if($Experiments){& ./.venv/Scripts/python.exe -m evaluation.experiments --output ../../artifacts/session08}
    else {& ./.venv/Scripts/python.exe -m evaluation.runner --report ../../artifacts/session08/baseline.json}
    if($LASTEXITCODE -ne 0){throw 'Evaluation failed; inspect safe service/configuration errors'}
} finally {Pop-Location;$env:POSTGRES_PASSWORD=$savedPassword}
