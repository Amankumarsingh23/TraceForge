# Local measured telemetry -> immutable evaluations -> persistent incident lifecycle.
$ErrorActionPreference='Stop'
New-Item -ItemType Directory -Force artifacts/session12 | Out-Null
$savedPg=$env:POSTGRES_PASSWORD;$savedCh=$env:CLICKHOUSE_PASSWORD;$savedNamespace=$env:TRACEFORGE_SERVICE_NAMESPACE
$schedulerEnabled=$false
$definitionPath='artifacts/session12/definitions.json'
$overridePath='artifacts/session12/scheduler.compose.json'
$allRequests=[Collections.Generic.List[object]]::new()
function Checkout([string]$phase,[bool]$fail=$false,[bool]$delay=$false){
 $query=if($fail){'?fail=true'}elseif($delay){'?delay_ms=300'}else{''}
 $response=Invoke-WebRequest "http://127.0.0.1:8080/checkout$query" -SkipHttpErrorCheck -TimeoutSec 6
 if($response.StatusCode -ne $(if($fail){502}else{200})){throw 'Unexpected real checkout outcome'}
 $body=$response.Content|ConvertFrom-Json;$allRequests.Add(@{phase=$phase;trace_id=$body.trace_id;status=$response.StatusCode;completed=[DateTime]::UtcNow.ToString('o')})
}
function Traffic([string]$phase,[int]$count,[bool]$fail=$false,[int]$delayed=0){for($i=0;$i -lt $count;$i++){Checkout $phase $fail ($i -lt $delayed);Start-Sleep -Milliseconds 750}}
function Evaluate([string]$label,[string]$key,[string]$cutoff='',[int]$revision=1){
 if(-not $cutoff){$cutoff=[DateTime]::UtcNow.AddSeconds(-3).ToString('yyyy-MM-ddTHH:mm:ssZ')}
 try{$cutoff=([DateTime]::Parse($cutoff)).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')}catch{throw 'Invalid local evaluation cutoff'}
 $raw=& ./bin/slo-evaluate.exe --file $definitionPath --key $key --cutoff $cutoff --revision $revision
 if($LASTEXITCODE -ne 0){throw "Evaluation $label failed"};$raw|Set-Content "artifacts/session12/$label.json";return @($raw|ConvertFrom-Json -DateKind String)[0]
}
function Incidents([string]$key){$response=Invoke-RestMethod 'http://127.0.0.1:8082/api/v1/alerts?limit=50' -TimeoutSec 5;return @($response.alerts|Where-Object{$_.slo_key -eq $key})}
function WaitReady { $deadline=[DateTime]::UtcNow.AddSeconds(75);do{try{if((Invoke-WebRequest http://127.0.0.1:8082/readyz -TimeoutSec 3).StatusCode -eq 200){return}}catch{};Start-Sleep -Milliseconds 500}while([DateTime]::UtcNow -lt $deadline);throw 'Backend did not recover' }
try{
 foreach($line in Get-Content .env){if($line -match '^POSTGRES_PASSWORD=(.+)$'){$env:POSTGRES_PASSWORD=$Matches[1]};if($line -match '^CLICKHOUSE_PASSWORD=(.+)$'){$env:CLICKHOUSE_PASSWORD=$Matches[1]}}
 $namespace='session12-'+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss');$env:TRACEFORGE_SERVICE_NAMESPACE=$namespace
 .\scripts\restart-demo.ps1
 Checkout 'warmup';$deadline=[DateTime]::UtcNow.AddSeconds(25)
 do{$q='namespace='+$namespace+'&service=payment&name=traceforge.requests.completed';$m=Invoke-RestMethod ('http://127.0.0.1:8082/api/v1/metrics/query?'+$q) -TimeoutSec 6;if($m.series.Count -eq 1){break};Start-Sleep -Milliseconds 300}while([DateTime]::UtcNow -lt $deadline)
 if($m.series.Count -ne 1){throw 'No measured payment writer'};$writer=$m.series[0].metadata.instance
 $id='s12-'+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')
 $rule=@{id='fast-demo';version=1;long_seconds=20;short_seconds=10;threshold='2';severity='critical';confirm=1;recover=1}
 $availability=@{id=$id+'-availability';version=1;description='Local synthetic completion availability, HTTP>=500 bad; declared payment writer';kind='availability';source_version='otlp-metrics/v1';service='payment';namespace=$namespace;environment='local';mode='synthetic';provider='';route='/charge';expected_writers=@($writer);target='0.9';window_seconds=20;min_count=3;min_coverage='0.6';rules=@($rule)}
 $latency=($availability|ConvertTo-Json -Depth 10|ConvertFrom-Json -AsHashtable);$latency.id=$id+'-latency';$latency.kind='latency';$latency.threshold_seconds='0.1';$latency.description='Local synthetic completed requests <=0.1 seconds at an actual explicit boundary';$latency.rules=@()
 $provider=@{id=$id+'-provider';version=1;description='Observed fixture provider attempts, not live inference or answer quality';kind='provider_attempt';source_version='agentops/v1';service='support-agent';namespace='';environment='local';mode='fixture';provider='traceforge_fixture';route='';expected_writers=@();target='0.8';window_seconds=30;min_count=1;min_coverage='1';rules=@()}
 $logical=($provider|ConvertTo-Json -Depth 10|ConvertFrom-Json -AsHashtable);$logical.id=$id+'-logical';$logical.kind='model_operation';$logical.target='0.9'
 $live=($provider|ConvertTo-Json -Depth 10|ConvertFrom-Json -AsHashtable);$live.id=$id+'-live';$live.mode='live';$live.provider='openai';$live.description='No paid/live calls authorized: missing live evidence must remain UNKNOWN'
 @($availability,$latency,$provider,$logical,$live)|ConvertTo-Json -Depth 12|Set-Content $definitionPath
 $key=$availability.id+'-v1'
 Traffic 'healthy' 25
 $healthy=Evaluate 'healthy' $key
 if($healthy.overall.evidence.status -ne 'MEASURED' -or $healthy.overall.evidence.bad -ne '0' -or -not $healthy.overall.budget.meets_target){throw 'Healthy real workload did not satisfy declared objective'}
 Traffic 'failing' 25 $true
 $firing=Evaluate 'firing' $key
 if($firing.rules[0].decision -ne 'BREACH'){throw 'Real failures did not breach both windows'}
 $incidents=Incidents $key;if($incidents.Count -ne 1 -or $incidents[0].state -ne 'FIRING'){throw 'Expected one actual firing incident'};$incidentId=$incidents[0].id
 $same=Evaluate 'same-cutoff' $key $firing.cutoff
 if($same.id -ne $firing.id){throw 'Retry duplicated evaluation'}
 $cutoff=[DateTime]::Parse($firing.cutoff).ToUniversalTime();$cutoffNs=(([bigint]($cutoff.Ticks-[DateTime]::UnixEpoch.Ticks))*100).ToString()
 $sql='INSERT INTO traceforge.metric_points SELECT * REPLACE(now64(9) AS ingested_at) FROM traceforge.metric_points WHERE service_namespace={ns:String} AND end_ns<={to:UInt64}'
 & docker compose exec -T clickhouse sh -c 'exec clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --param_ns "$1" --param_to "$2" --query "$3"' sh $namespace $cutoffNs $sql
 if($LASTEXITCODE -ne 0){throw 'Isolated metric replay failed'}
 $replay=Evaluate 'replay-revision' $key $firing.cutoff 2
 if($replay.overall.evidence.total -ne $firing.overall.evidence.total -or $replay.overall.evidence.bad -ne $firing.overall.evidence.bad -or ($replay.overall.budget|ConvertTo-Json -Compress) -ne ($firing.overall.budget|ConvertTo-Json -Compress)){throw 'Replay changed SLI or budget'}
 @{services=@{'traceforge-server'=@{environment=@{TRACEFORGE_SLO_ACTIVE=$key;TRACEFORGE_SLO_CADENCE='2s'}}}}|ConvertTo-Json -Depth 8|Set-Content $overridePath
 $schedulerEnabled=$true
 & docker compose -f compose.yaml -f $overridePath up -d --no-deps --wait --wait-timeout 90 traceforge-server
 if($LASTEXITCODE -ne 0){throw 'Scheduler restart failed'}
 Traffic 'repeated-failure' 6 $true
 $deadline=[DateTime]::UtcNow.AddSeconds(30)
 do{$scheduled=Invoke-RestMethod "http://127.0.0.1:8082/api/v1/slos/$key" -TimeoutSec 5;if($scheduled.latest.rules[0].decision -eq 'BREACH' -and ([DateTime]$scheduled.latest.cutoff).ToUniversalTime() -gt ([DateTime]$firing.cutoff).ToUniversalTime()){break};Start-Sleep -Milliseconds 500}while([DateTime]::UtcNow -lt $deadline)
 if($scheduled.latest.rules[0].decision -ne 'BREACH'){throw 'Actual scheduler did not measure ongoing failure'};$scheduled|ConvertTo-Json -Depth 50|Set-Content artifacts/session12/scheduler.json
 & docker compose restart traceforge-server;if($LASTEXITCODE -ne 0){throw 'Backend restart failed'};WaitReady;Traffic 'after-restart-failure' 6 $true
 $afterRestart=Evaluate 'after-restart' $key
 $incidents=Incidents $key;if($incidents.Count -ne 1 -or $incidents[0].id -ne $incidentId){throw 'Restart duplicated/lost incident'}
 try{
  & docker compose stop clickhouse;if($LASTEXITCODE -ne 0){throw 'ClickHouse stop failed'}
  $unknown=Evaluate 'unavailable' $key
  if($unknown.status -ne 'UNKNOWN' -or $unknown.overall.evidence.status -ne 'BACKEND_UNAVAILABLE'){throw 'Outage did not remain UNKNOWN'}
  $active=Incidents $key;if($active.Count -ne 1 -or $active[0].state -ne 'FIRING'){throw 'Outage falsely resolved alert'}
 }finally{& docker compose start clickhouse;WaitReady}
 Traffic 'recovery' 34
 $recovered=Evaluate 'recovered' $key
 $deadline=[DateTime]::UtcNow.AddSeconds(20)
 do{$incidents=Incidents $key;if($incidents[0].state -eq 'RESOLVED'){break};Start-Sleep -Milliseconds 500}while([DateTime]::UtcNow -lt $deadline)
 if($recovered.overall.evidence.status -ne 'MEASURED' -or -not $recovered.overall.budget.meets_target -or $incidents.Count -ne 1 -or $incidents[0].state -ne 'RESOLVED'){throw 'Valid healthy recovery did not resolve same incident'}
 $history=Invoke-RestMethod "http://127.0.0.1:8082/api/v1/alerts/$incidentId" -TimeoutSec 5
 if($history.events.Count -ne 2){throw 'Firing/resolution event history duplicated'};$history|ConvertTo-Json -Depth 40|Set-Content artifacts/session12/incident.json
 & docker compose up -d --no-deps --wait --wait-timeout 90 traceforge-server;if($LASTEXITCODE -ne 0){throw 'Scheduler disable failed'};$schedulerEnabled=$false
 Traffic 'latency' 26 $false 8
 $latencyResult=Evaluate 'latency' ($latency.id+'-v1')
 if($latencyResult.overall.evidence.status -ne 'MEASURED' -or [int]$latencyResult.overall.evidence.bad -le 0){throw 'Actual delayed histogram objective not classified'}
 try{$health=Invoke-RestMethod http://127.0.0.1:8084/healthz -TimeoutSec 2}catch{$python=(Resolve-Path examples/agent-demo/.venv/Scripts/python.exe).Path;$process=Start-Process $python -ArgumentList '-m','agent_demo' -WorkingDirectory (Resolve-Path examples/agent-demo).Path -WindowStyle Hidden -RedirectStandardError artifacts/session12/agent.log -PassThru;$process.Id|Set-Content artifacts/session12/agent-pid.txt;Start-Sleep -Seconds 3;$health=Invoke-RestMethod http://127.0.0.1:8084/healthz -TimeoutSec 3}
 if($health.mode -ne 'fixture'){throw 'Refusing any paid/live provider call'}
 $providerRequests=@();foreach($scenario in @('normal','retry','permanent_error')){$response=Invoke-WebRequest http://127.0.0.1:8084/ask -Method Post -ContentType application/json -Body (@{question='refund policy';scenario=$scenario;max_model_calls=1}|ConvertTo-Json) -SkipHttpErrorCheck -TimeoutSec 15;$body=$response.Content|ConvertFrom-Json;$providerRequests+=@{scenario=$scenario;trace_id=$body.trace_id;run_id=$body.run_id;status=$response.StatusCode}}
 Start-Sleep -Seconds 4
 $providerResult=Evaluate 'provider' ($provider.id+'-v1');$logicalResult=Evaluate 'logical' ($logical.id+'-v1');$liveResult=Evaluate 'live-unknown' ($live.id+'-v1')
 if($providerResult.overall.evidence.total -ne '4' -or $providerResult.overall.evidence.good -ne '2' -or $logicalResult.overall.evidence.total -ne '3' -or $logicalResult.overall.evidence.good -ne '2' -or $providerResult.overall.evidence.details.retry_recovered -ne 1 -or $liveResult.status -ne 'UNKNOWN'){throw 'Provider attempt/logical/fixture/live accounting mismatch'}
 $providerRequests|ConvertTo-Json|Set-Content artifacts/session12/provider-requests.json
 $traceList=($providerRequests|ForEach-Object {$_.trace_id}) -join ','
 $replaySql='INSERT INTO traceforge.spans SELECT * REPLACE(now64(3) AS ingested_at) FROM traceforge.spans WHERE trace_id IN splitByChar('','',{ids:String})'
 & docker compose exec -T clickhouse sh -c 'exec clickhouse-client --user traceforge --password "$CLICKHOUSE_PASSWORD" --param_ids "$1" --query "$2"' sh $traceList $replaySql
 if($LASTEXITCODE -ne 0){throw 'Provider trace replay failed'}
 $providerReplay=Evaluate 'provider-replay' ($provider.id+'-v1') $providerResult.cutoff 2
 if($providerReplay.overall.evidence.total -ne '4' -or $providerReplay.overall.evidence.good -ne '2'){throw 'Provider replay inflated observations'}
 $durationResult=Evaluate 'provider-duration-revision' ($provider.id+'-v1') $providerResult.cutoff 3
 if($null -eq $durationResult.overall.evidence.details.logical_mean_ms){throw 'Logical duration evidence missing'}
 @{namespace=$namespace;writer=$writer;slo_key=$key;latency_key=$latency.id+'-v1';provider_key=$provider.id+'-v1';logical_key=$logical.id+'-v1';live_key=$live.id+'-v1';incident_id=$incidentId;healthy=$healthy.id;firing=$firing.id;unknown=$unknown.id;recovery=$recovered.id;latency=$latencyResult.id;provider=$providerResult.id;logical=$logicalResult.id;live=$liveResult.id;requests=$allRequests;provider_requests=$providerRequests}|ConvertTo-Json -Depth 20|Set-Content artifacts/session12/evidence.json
 Write-Output 'PASS: actual healthy/failing/recovery, scheduler dedup/restart/outage, metric replay, delayed histogram and canonical fixture provider/live-unknown evidence persisted.'
}finally{
 if($schedulerEnabled){& docker compose up -d --no-deps --wait --wait-timeout 90 traceforge-server}
 $env:POSTGRES_PASSWORD=$savedPg;$env:CLICKHOUSE_PASSWORD=$savedCh;$env:TRACEFORGE_SERVICE_NAMESPACE=$savedNamespace
}
