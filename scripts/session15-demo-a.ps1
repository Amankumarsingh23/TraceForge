$ErrorActionPreference='Stop'
Set-Location (Join-Path $PSScriptRoot '..')
New-Item -ItemType Directory -Force artifacts/session15/demo-a | Out-Null
# Run alone: the lifecycle demonstration intentionally restarts/stops dependencies.
./scripts/session11-smoke.ps1 *> artifacts/session15/demo-a/metrics-console.txt
if(-not $?){throw 'Direct-metric/trace workload failed'}
Copy-Item artifacts/session11/evidence.json artifacts/session15/demo-a/workload.json
Copy-Item artifacts/session11/metrics-after-replay.json artifacts/session15/demo-a/metrics-after-replay.json
./scripts/session12-smoke.ps1 *> artifacts/session15/demo-a/reliability-console.txt
if(-not $?){throw 'Reliability demonstration failed'}
Copy-Item artifacts/session12/evidence.json artifacts/session15/demo-a/reliability.json
Write-Output 'Demo A complete: actual100-request mixed workload plus separate measured SLO lifecycle, replay and failure/recovery evidence. Their windows/populations are deliberately separate.'
