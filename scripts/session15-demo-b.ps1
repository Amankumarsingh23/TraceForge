$ErrorActionPreference='Stop'
Set-Location (Join-Path $PSScriptRoot '..')
if((Invoke-RestMethod http://127.0.0.1:8084/healthz -TimeoutSec 3).mode -ne 'fixture'){throw 'Fixture mode required'}
foreach($line in Get-Content .env){if($line -match '^POSTGRES_PASSWORD=(.+)$'){$env:POSTGRES_PASSWORD=$Matches[1]}}
Push-Location examples/agent-demo
try{
 & .venv/Scripts/python.exe -m evaluation.session09 --output ../../artifacts/session15/demo-b
 if($LASTEXITCODE -ne 0){throw 'Fixture evaluation/comparison failed'}
}finally{Pop-Location}
