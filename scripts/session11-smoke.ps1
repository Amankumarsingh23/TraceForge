# Rebuilt backend/Collector and demo required. All traffic is isolated synthetic HTTP.
$ErrorActionPreference='Stop'
New-Item -ItemType Directory -Force artifacts/session11 | Out-Null
$savedNamespace=$env:TRACEFORGE_SERVICE_NAMESPACE
try {
 $namespace='session11-'+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
 $env:TRACEFORGE_SERVICE_NAMESPACE=$namespace
 $from=[DateTime]::UtcNow
 .\scripts\restart-demo.ps1
 $requests=@()
 for($i=0;$i -lt 100;$i++){
  $kind=if($i -lt 80){'normal'}elseif($i -lt 95){'delayed'}else{'failed'}
  $querySuffix=if($kind -eq 'delayed'){'?delay_ms=100'}elseif($kind -eq 'failed'){'?fail=true'}else{''}
  $response=Invoke-WebRequest "http://127.0.0.1:8080/checkout$querySuffix" -SkipHttpErrorCheck -TimeoutSec 6
  $body=$response.Content|ConvertFrom-Json
  $expected=if($kind -eq 'failed'){502}else{200}
  if($response.StatusCode -ne $expected){throw 'Unexpected actual HTTP outcome'}
  $requests+=@{kind=$kind;trace_id=$body.trace_id;status=$response.StatusCode}
 }
 $deadline=[DateTime]::UtcNow.AddSeconds(45)
 do{
  $to=[DateTime]::UtcNow
  $query='from='+[Uri]::EscapeDataString($from.ToString('o'))+'&to='+[Uri]::EscapeDataString($to.ToString('o'))+'&namespace='+$namespace
  $metrics=Invoke-RestMethod ('http://127.0.0.1:8082/api/v1/metrics/query?'+$query) -TimeoutSec 6
  $ok=$true
  foreach($service in @('gateway','payment')){
   $count=@($metrics.series|Where-Object{$_.metadata.service -eq $service -and $_.metadata.name -eq 'traceforge.requests.completed'})
   $errors=@($metrics.series|Where-Object{$_.metadata.service -eq $service -and $_.metadata.name -eq 'traceforge.requests.errors'})
   $hist=@($metrics.series|Where-Object{$_.metadata.service -eq $service -and $_.metadata.name -eq 'traceforge.request.duration'})
   if($count.Count -ne 1 -or $count[0].increment -ne '100' -or $errors.Count -ne 1 -or $errors[0].increment -ne '5' -or $hist.Count -ne 1 -or $hist[0].histogram_count -ne '100'){$ok=$false}
  }
  if($ok){break};Start-Sleep -Milliseconds 300
 }while([DateTime]::UtcNow -lt $deadline)
 if(-not $ok){$metrics|ConvertTo-Json -Depth 50|Set-Content artifacts/session11/incomplete.json;throw 'Metric exports did not reach the actual 100/5/100 workload observations'}
 $traceQuery=$query+'&workload=synthetic&bucket_seconds=60'
 $traceDeadline=[DateTime]::UtcNow.AddSeconds(35)
 do{$traces=Invoke-RestMethod ('http://127.0.0.1:8082/api/v1/service-graph?'+$traceQuery) -TimeoutSec 6;if($traces.services.Count -eq 2 -and $traces.edges.Count -eq 1 -and $traces.edges[0].client.count -eq 100){break};Start-Sleep -Milliseconds 300}while([DateTime]::UtcNow -lt $traceDeadline)
 if($traces.services.Count -ne 2 -or $traces.edges.Count -ne 1 -or $traces.edges[0].client.count -ne 100){throw 'Trace comparison did not observe 100 paired checkouts'}
 foreach($s in $traces.services){if($s.stats.count -ne 100 -or $s.stats.errors -ne 5){throw 'Trace-derived SERVER counts did not match this covered workload'}}
 $metrics|ConvertTo-Json -Depth 50|Set-Content artifacts/session11/metrics-before-replay.json
 $traces|ConvertTo-Json -Depth 50|Set-Content artifacts/session11/trace-comparison.json
 # Replay all isolated points at/before a frozen end; later periodic exports are outside this scope.
  # Use the absolute query cutoff, not one stream's last export, for all-source replay.
 $cutoff=(([bigint]($to.Ticks-[DateTime]::UnixEpoch.Ticks))*100).ToString()
 $sql='INSERT INTO traceforge.metric_points SELECT * REPLACE (now64(9) AS ingested_at) FROM traceforge.metric_points WHERE service_namespace={ns:String} AND end_ns <= {cutoff:UInt64}'
 & docker compose exec -T clickhouse sh -c 'exec clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --param_ns "$1" --param_cutoff "$2" --query "$3"' sh $namespace $cutoff $sql
 if($LASTEXITCODE -ne 0){throw 'Metric replay insert failed'}
 $after=Invoke-RestMethod ('http://127.0.0.1:8082/api/v1/metrics/query?'+$query) -TimeoutSec 6
 foreach($s in $metrics.series){$other=@($after.series|Where-Object{$_.metadata.stream_id -eq $s.metadata.stream_id})[0];if($other.increment -ne $s.increment -or $other.histogram_count -ne $s.histogram_count -or ($other.histogram_buckets|ConvertTo-Json -Compress) -ne ($s.histogram_buckets|ConvertTo-Json -Compress) -or $other.diagnostics.replays -lt 1){throw 'Replay changed a logical metric result or duplication was not exposed'}}
 $after|ConvertTo-Json -Depth 50|Set-Content artifacts/session11/metrics-after-replay.json
 # Restart only this checkout's payment process; gateway keeps running.
 $oldInstance=($metrics.series|Where-Object{$_.metadata.service -eq 'payment'}|Select-Object -First 1).metadata.instance
 $binary=(Resolve-Path bin/shop-demo-session06.exe).Path
 foreach($connection in Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8081 -State Listen){$proc=Get-CimInstance Win32_Process -Filter "ProcessId=$($connection.OwningProcess)";if($proc.ExecutablePath -ne $binary){throw 'Payment port belongs to another checkout'};Stop-Process -Id $connection.OwningProcess}
 $p=Start-Process -FilePath $binary -ArgumentList '--service','payment','--port','8081' -WindowStyle Hidden -RedirectStandardError artifacts/session11/payment-restart.log -PassThru
 $p.Id|Set-Content artifacts/session11/payment-restart-pid.txt
 $restartDeadline=[DateTime]::UtcNow.AddSeconds(10)
 do{try{$ready=(Invoke-WebRequest http://127.0.0.1:8081/healthz -TimeoutSec 2 -SkipHttpErrorCheck).StatusCode -ne 0}catch{$ready=$false};if($ready){break};Start-Sleep -Milliseconds 200}while([DateTime]::UtcNow -lt $restartDeadline)
 if(-not $ready){throw 'Restarted payment did not listen'}
 $restartRequests=@();for($i=0;$i -lt 5;$i++){$body=Invoke-RestMethod http://127.0.0.1:8080/checkout -TimeoutSec 6;$restartRequests+=$body.trace_id}
 $restartDeadline=[DateTime]::UtcNow.AddSeconds(35)
 do{$restartTo=[DateTime]::UtcNow;$restartQuery='from='+[Uri]::EscapeDataString($from.ToString('o'))+'&to='+[Uri]::EscapeDataString($restartTo.ToString('o'))+'&namespace='+$namespace+'&name=traceforge.requests.completed&service=payment';$restart=Invoke-RestMethod ('http://127.0.0.1:8082/api/v1/metrics/query?'+$restartQuery) -TimeoutSec 6;$new=@($restart.series|Where-Object{$_.metadata.instance -ne $oldInstance});if($new.Count -eq 1 -and $new[0].increment -eq '5'){break};Start-Sleep -Milliseconds 300}while([DateTime]::UtcNow -lt $restartDeadline)
 if($new.Count -ne 1 -or $new[0].increment -ne '5' -or @($restart.series|Where-Object{$_.observed_rate_per_second -lt 0}).Count -gt 0){throw 'Restart identity/increment/rate verification failed'}
 $restart|ConvertTo-Json -Depth 50|Set-Content artifacts/session11/restart.json
 @{namespace=$namespace;from=$from.ToString('o');to=$to.ToString('o');query=$query;trace_query=$traceQuery;requests=$requests;old_instance=$oldInstance;new_instance=$new[0].metadata.instance;restart_requests=$restartRequests;restart_query=$restartQuery}|ConvertTo-Json -Depth 20|Set-Content artifacts/session11/evidence.json
 Write-Output 'PASS: actual 80/15/5 workload; each service direct100 requests/5 errors/histogram100, trace SERVER100/5, paired100; replay stable; payment restart new writer5 and no negative monotonic rate.'
}finally{$env:TRACEFORGE_SERVICE_NAMESPACE=$savedNamespace}
