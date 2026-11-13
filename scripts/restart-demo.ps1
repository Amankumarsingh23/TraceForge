# Restart only this checkout's known synthetic demo binaries after rebuilding.
$ErrorActionPreference = 'Stop'
$workspaceRoot = (Resolve-Path '.').Path
$binaryPath = Join-Path $workspaceRoot 'bin/shop-demo-session06.exe'
if (-not (Test-Path -LiteralPath $binaryPath)) { throw 'Build bin/shop-demo-session06.exe first' }
New-Item -ItemType Directory -Force artifacts/session06 | Out-Null
foreach ($port in 8080,8081) {
    foreach ($connection in Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort $port -State Listen -ErrorAction SilentlyContinue) {
        $ownedProcess = Get-CimInstance Win32_Process -Filter "ProcessId=$($connection.OwningProcess)"
        $allowedPaths = @((Join-Path $workspaceRoot 'bin/shop-demo.exe'), $binaryPath)
        if ($ownedProcess.ExecutablePath -notin $allowedPaths) { throw "Port $port belongs to another process; refusing to stop it" }
        Stop-Process -Id $connection.OwningProcess
    }
}
$paymentProcess = Start-Process -FilePath $binaryPath -ArgumentList '--service','payment','--port','8081' -WindowStyle Hidden -RedirectStandardError artifacts/session06/payment.log -PassThru
$gatewayProcess = Start-Process -FilePath $binaryPath -ArgumentList '--service','gateway','--port','8080','--payment-url','http://localhost:8081' -WindowStyle Hidden -RedirectStandardError artifacts/session06/gateway.log -PassThru
@($paymentProcess.Id,$gatewayProcess.Id) | Set-Content artifacts/session06/demo-pids.txt
Write-Output 'Local gateway/payment demo processes restarted with OTLP logging enabled.'
