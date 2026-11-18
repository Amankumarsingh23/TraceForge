param([switch]$InstallDependencies)
$ErrorActionPreference='Stop'
Set-Location (Join-Path $PSScriptRoot '..')
if(-not (Test-Path .env)){throw 'Create .env from .env.example with two distinct random local passwords. Existing files are never overwritten.'}
if(-not (Get-Command go -ErrorAction SilentlyContinue)){
 $portable=Join-Path $env:LOCALAPPDATA 'TraceForge-tools/go/bin'
 if(Test-Path "$portable/go.exe"){$env:PATH="$portable;$env:PATH"}else{throw 'Go 1.27.4 is required on PATH'}
}
foreach($line in Get-Content .env){if($line -match '^(CLICKHOUSE_PASSWORD|POSTGRES_PASSWORD)=(.+)$'){[Environment]::SetEnvironmentVariable($Matches[1],$Matches[2],'Process')}}
New-Item -ItemType Directory -Force artifacts/session15,bin | Out-Null
docker compose up -d --build --wait --wait-timeout 180
if($LASTEXITCODE -ne 0){throw 'Compose startup failed; start Docker Desktop and inspect container logs'}
if($InstallDependencies){
 if(-not(Test-Path examples/agent-demo/.venv/Scripts/python.exe)){py -3.13 -m venv examples/agent-demo/.venv}
 & examples/agent-demo/.venv/Scripts/python.exe -m pip install -r examples/agent-demo/requirements.lock
 if($LASTEXITCODE -ne 0){throw 'Locked Python install failed'}
 npm --prefix web ci
 if($LASTEXITCODE -ne 0){throw 'Locked frontend install failed'}
}
if(-not(Test-Path examples/agent-demo/.venv/Scripts/python.exe)){throw 'Run with -InstallDependencies after installing Python3.13 and Node24/npm12'}
# Windows cannot replace a running executable. Stop only the known checkout's
# demo before rebuilding, never a process belonging to another application.
foreach($port in 8080,8081){
 foreach($connection in Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue){
  $owned=Get-Process -Id $connection.OwningProcess
  if($owned.Path -ne "$PWD\bin\shop-demo-session06.exe"){throw "Port $port belongs to a different executable"}
  Stop-Process -Id $owned.Id
 }
}
go build -o bin/shop-demo-session06.exe ./cmd/shop-demo
if($LASTEXITCODE -ne 0){throw 'Demo build failed'}
go build -o bin/slo-evaluate.exe ./cmd/slo-evaluate
if($LASTEXITCODE -ne 0){throw 'Evaluator build failed'}
# Both trace and metadata schema migrations are non-destructive/idempotent.
Get-ChildItem examples/agent-demo/evaluation/migrations/*.sql | Sort-Object Name | Get-Content -Raw | docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U traceforge -d traceforge
if($LASTEXITCODE -ne 0){throw 'Metadata migration failed'}
./scripts/restart-demo.ps1
$listener=Get-NetTCPConnection -State Listen -LocalPort 8084 -ErrorAction SilentlyContinue
if(-not $listener){
 $p=Start-Process -FilePath "$PWD/examples/agent-demo/.venv/Scripts/python.exe" -WorkingDirectory "$PWD/examples/agent-demo" -ArgumentList '-m','uvicorn','agent_demo.app:app','--host','127.0.0.1','--port','8084' -WindowStyle Hidden -PassThru -RedirectStandardOutput "$PWD/artifacts/session15/agent.txt" -RedirectStandardError "$PWD/artifacts/session15/agent-error.txt"
 $p.Id|Set-Content artifacts/session15/agent.pid
}
$deadline=[DateTime]::UtcNow.AddSeconds(20)
do{try{$h=Invoke-RestMethod http://127.0.0.1:8084/healthz -TimeoutSec 2;if($h.mode -eq 'fixture'){break}}catch{};Start-Sleep -Milliseconds 250}while([DateTime]::UtcNow -lt $deadline)
if($h.mode -ne 'fixture'){throw 'Expected fixture-only agent on8084; no live calls allowed'}
Write-Output 'Legacy loopback demo ready: API8082, gateway8080, payment8081, fixture agent8084. Start the production Next server separately on3000. No data volumes were removed.'
