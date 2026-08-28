# Unity가 켜진 상태에서 현재 소스 CLI로 실제 요청을 보낸다.
# 한 번 보내기, 겹치는 요청, 직렬화, 주요 명령, play/pause/stop을 확인한다.
# 전체 스크립트 재컴파일은 걸지 않는다. 대기는 heartbeat/status를 폴링한다.
#
#   powershell -File scripts/live-test.ps1
#   powershell -File scripts/live-test.ps1 -Project C:/path/to/unity-project

param(
    [string]$Project = "",
    [int]$TimeoutMs = 180000
)

$ErrorActionPreference = "Continue"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$WorkDir = Join-Path $env:TEMP ("unity-cli-live-test-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$Cli = Join-Path $WorkDir "unity-cli-live.exe"
$SlowMs = 400

$script:Fail = 0
$script:Pass = 0

function Write-Case($name) {
    Write-Host ""
    Write-Host "===== $name =====" -ForegroundColor Cyan
}

function Pass($detail) {
    $script:Pass++
    if ($detail) { Write-Host "PASS  $detail" -ForegroundColor Green }
    else { Write-Host "PASS" -ForegroundColor Green }
}

function Fail($detail) {
    $script:Fail++
    Write-Host "FAIL  $detail" -ForegroundColor Red
}

function Read-CliExit($process) {
    $exit = $process.ExitCode
    if ($null -eq $exit) { return 0 }
    return [int]$exit
}

function Invoke-Cli {
    param(
        [string[]]$CliArgs,
        [int]$Timeout = $TimeoutMs
    )
    $all = @()
    if ($Project) { $all += @("--project", $Project) }
    $all += @("--timeout", "$Timeout")
    $all += $CliArgs

    $outFile = Join-Path $WorkDir ("out-" + [guid]::NewGuid().ToString("N") + ".txt")
    $errFile = Join-Path $WorkDir ("err-" + [guid]::NewGuid().ToString("N") + ".txt")
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $p = Start-Process -FilePath $Cli -ArgumentList $all -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $sw.Stop()
    [pscustomobject]@{
        ExitCode = Read-CliExit $p
        Stdout   = (Get-Content -Raw $outFile -ErrorAction SilentlyContinue)
        Stderr   = (Get-Content -Raw $errFile -ErrorAction SilentlyContinue)
        Ms       = $sw.ElapsedMilliseconds
    }
}

function Invoke-CliStdin {
    param(
        [string[]]$CliArgs,
        [string]$Code,
        [int]$Timeout = $TimeoutMs
    )
    $inFile = Join-Path $WorkDir ("in-" + [guid]::NewGuid().ToString("N") + ".cs")
    Set-Content -Path $inFile -Value $Code -NoNewline -Encoding utf8
    $all = @()
    if ($Project) { $all += @("--project", $Project) }
    $all += @("--timeout", "$Timeout")
    $all += $CliArgs

    $outFile = Join-Path $WorkDir ("out-" + [guid]::NewGuid().ToString("N") + ".txt")
    $errFile = Join-Path $WorkDir ("err-" + [guid]::NewGuid().ToString("N") + ".txt")
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $p = Start-Process -FilePath $Cli -ArgumentList $all -NoNewWindow -Wait -PassThru `
        -RedirectStandardInput $inFile `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $sw.Stop()
    [pscustomobject]@{
        ExitCode = Read-CliExit $p
        Stdout   = (Get-Content -Raw $outFile -ErrorAction SilentlyContinue)
        Stderr   = (Get-Content -Raw $errFile -ErrorAction SilentlyContinue)
        Ms       = $sw.ElapsedMilliseconds
    }
}

function Start-CliJob {
    param(
        [string[]]$CliArgs,
        [string]$Code,
        [int]$Timeout = $TimeoutMs
    )
    $outFile = Join-Path $WorkDir ("out-" + [guid]::NewGuid().ToString("N") + ".txt")
    $errFile = Join-Path $WorkDir ("err-" + [guid]::NewGuid().ToString("N") + ".txt")
    $all = @()
    if ($Project) { $all += @("--project", $Project) }
    $all += @("--timeout", "$Timeout")
    $all += $CliArgs
    $start = @{
        FilePath               = $Cli
        ArgumentList           = $all
        NoNewWindow            = $true
        PassThru               = $true
        RedirectStandardOutput = $outFile
        RedirectStandardError  = $errFile
    }
    if ($Code) {
        $inFile = Join-Path $WorkDir ("in-" + [guid]::NewGuid().ToString("N") + ".cs")
        Set-Content -Path $inFile -Value $Code -NoNewline -Encoding utf8
        $start.RedirectStandardInput = $inFile
    }
    $p = Start-Process @start
    [pscustomobject]@{
        Process = $p
        OutFile = $outFile
        ErrFile = $errFile
        Started = Get-Date
    }
}

function Wait-CliJob($job) {
    if (-not $job.Process.HasExited) {
        $job.Process.WaitForExit()
    }
    $out = Get-Content -Raw $job.OutFile -ErrorAction SilentlyContinue
    if ($null -eq $out) { $out = "" }
    $err = Get-Content -Raw $job.ErrFile -ErrorAction SilentlyContinue
    if ($null -eq $err) { $err = "" }
    [pscustomobject]@{
        ExitCode = Read-CliExit $job.Process
        Stdout   = $out.Trim()
        Stderr   = $err.Trim()
        Ms       = [int]((Get-Date) - $job.Started).TotalMilliseconds
    }
}

function Get-UnityState {
    $r = Invoke-Cli -CliArgs @("status")
    $text = "$(if ($r.Stdout) { $r.Stdout } else { '' })`n$(if ($r.Stderr) { $r.Stderr } else { '' })"
    if ($text -match 'Unity:\s+(\S+)') { return $Matches[1].Trim() }
    if ($text -match 'not responding') { return "not-responding" }
    return ""
}

function Wait-UnityState {
    param(
        [string[]]$Want,
        [int]$Timeout = $TimeoutMs
    )
    $deadline = [datetime]::UtcNow.AddMilliseconds($Timeout)
    $last = ""
    while ([datetime]::UtcNow -lt $deadline) {
        $last = Get-UnityState
        foreach ($state in $Want) {
            if ($last -eq $state) { return $last }
        }
        Start-Sleep -Milliseconds 200
    }
    return $last
}

try {
    $pkgPath = Join-Path $RepoRoot "unity-connector\package.json"
    $version = (Get-Content -Raw $pkgPath | ConvertFrom-Json).version
    if (-not $version) { throw "connector version missing in package.json" }

    Write-Host "Building current CLI $version into $WorkDir"
    Push-Location $RepoRoot
    go build -ldflags "-X main.Version=$version" -o $Cli .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    Write-Case "status (Unity must be running)"
    $r = Invoke-Cli -CliArgs @("status")
    Write-Host $r.Stdout
    if ($r.ExitCode -eq 0 -and $r.Stdout -match "Unity:") { Pass $r.Stdout.Trim() }
    else { Fail "status failed: $($r.Stderr)" ; throw "Unity is not reachable. Open the Editor with the Connector and rerun." }

    Write-Case "list tools"
    $r = Invoke-Cli -CliArgs @("list")
    if ($r.ExitCode -eq 0 -and $r.Stdout -match "exec" -and $r.Stdout -match "profiler" -and $r.Stdout -match "screenshot") {
        Pass "listed exec/profiler/screenshot"
    } else {
        Fail "list missing tools: exit $($r.ExitCode) $($r.Stdout)"
    }

    Write-Case "exec once"
    $r = Invoke-CliStdin -CliArgs @("exec") -Code 'return (1+1).ToString();'
    if ($r.ExitCode -eq 0 -and $r.Stdout.Trim() -eq "2") { Pass "2" }
    else { Fail "exec 1+1 => exit $($r.ExitCode) out='$($r.Stdout)' err='$($r.Stderr)'" }

    Write-Case "async exec is blocked"
    $r = Invoke-CliStdin -CliArgs @("exec") -Code 'await Task.Delay(1); return "no";'
    if ($r.ExitCode -ne 0 -and "$($r.Stderr)$($r.Stdout)" -match "allow-async") { Pass "blocked without --allow-async" }
    else { Fail "expected async block, got exit $($r.ExitCode) $($r.Stderr)" }

    Write-Case "unknown command fails"
    $r = Invoke-Cli -CliArgs @("nonexistent_command_xyz")
    if ($r.ExitCode -ne 0 -and $r.Stderr -match "Unknown command") { Pass "rejected" }
    else { Fail "expected unknown command error, got exit $($r.ExitCode) $($r.Stderr)" }

    Write-Case "five sequential execs"
    $ok = $true
    for ($i = 1; $i -le 5; $i++) {
        $r = Invoke-CliStdin -CliArgs @("exec") -Code "return `"seq-$i`";"
        if ($r.ExitCode -ne 0 -or $r.Stdout.Trim() -ne "seq-$i") {
            Fail "seq $i => exit $($r.ExitCode) '$($r.Stdout.Trim())'"
            $ok = $false
            break
        }
    }
    if ($ok) { Pass "seq-1 .. seq-5" }

    Write-Case "three overlapping execs (same moment)"
    $jobs = @(
        (Start-CliJob -CliArgs @("exec") -Code 'return "A";'),
        (Start-CliJob -CliArgs @("exec") -Code 'return "B";'),
        (Start-CliJob -CliArgs @("exec") -Code 'return "C";')
    )
    $results = $jobs | ForEach-Object { Wait-CliJob $_ }
    $outs = $results | ForEach-Object { $_.Stdout.Trim() } | Sort-Object
    $codes = $results | ForEach-Object { $_.ExitCode }
    if (($codes | Where-Object { $_ -ne 0 }).Count -eq 0 -and ($outs -join ",") -eq "A,B,C") {
        Pass ("all returned A/B/C, elapsed " + (($results | Measure-Object Ms -Maximum).Maximum) + "ms")
    } else {
        Fail ("exits=$codes outs=$outs")
        $results | ForEach-Object { Write-Host ("  " + $_.ExitCode + " " + $_.Stdout + " " + $_.Stderr) }
    }

    Write-Case "slow + fast overlapping (main-thread serialize)"
    $slowCode = @"
var until = System.DateTime.UtcNow.AddMilliseconds($SlowMs);
while (System.DateTime.UtcNow < until) {}
return "slow";
"@
    $slow = Start-CliJob -CliArgs @("exec") -Code $slowCode
    $fast = Start-CliJob -CliArgs @("exec") -Code 'return "fast";'
    $slowR = Wait-CliJob $slow
    $fastR = Wait-CliJob $fast
    $serialized = $fastR.Ms -ge ($SlowMs - 50)
    if ($slowR.ExitCode -eq 0 -and $slowR.Stdout.Trim() -eq "slow" -and $fastR.ExitCode -eq 0 -and $fastR.Stdout.Trim() -eq "fast" -and $serialized) {
        Pass "slow=$($slowR.Ms)ms fast=$($fastR.Ms)ms (fast waited for the slow one)"
    } else {
        Fail "slow='$($slowR.Stdout)'/$($slowR.ExitCode)/$($slowR.Ms)ms fast='$($fastR.Stdout)'/$($fastR.ExitCode)/$($fastR.Ms)ms"
    }

    Write-Case "duplicate identical execs in parallel"
    $d1 = Start-CliJob -CliArgs @("exec") -Code 'return "dup";'
    $d2 = Start-CliJob -CliArgs @("exec") -Code 'return "dup";'
    $r1 = Wait-CliJob $d1
    $r2 = Wait-CliJob $d2
    if ($r1.ExitCode -eq 0 -and $r2.ExitCode -eq 0 -and $r1.Stdout.Trim() -eq "dup" -and $r2.Stdout.Trim() -eq "dup") {
        Pass "both returned dup"
    } else {
        Fail "r1='$($r1.Stdout)' r2='$($r2.Stdout)'"
    }

    Write-Case "console clear / log / read"
    $clear = Invoke-Cli -CliArgs @("console", "--clear")
    $log = Invoke-CliStdin -CliArgs @("exec") -Code 'Debug.Log("live-test-marker"); return "logged";'
    $read = Invoke-Cli -CliArgs @("console", "--type", "log", "--lines", "20")
    if ($clear.ExitCode -eq 0 -and $log.Stdout.Trim() -eq "logged" -and $read.ExitCode -eq 0 -and $read.Stdout -match "live-test-marker") {
        Pass "logged and read back"
    } else {
        Fail "clear=$($clear.ExitCode) log='$($log.Stdout)' read='$($read.Stdout)' $($read.Stderr)"
    }

    Write-Case "menu"
    $r = Invoke-Cli -CliArgs @("menu", "Window/General/Console")
    if ($r.ExitCode -eq 0 -and "$($r.Stdout)$($r.Stderr)" -match "Console") { Pass "Window/General/Console" }
    else { Fail "menu: exit $($r.ExitCode) $($r.Stdout) $($r.Stderr)" }

    Write-Case "screenshot to temp"
    $shot = (Join-Path $WorkDir "shot.png").Replace("\", "/")
    $r = Invoke-Cli -CliArgs @("screenshot", "--width", "64", "--height", "64", "--output_path", $shot)
    if ($r.ExitCode -eq 0 -and (Test-Path $shot) -and ((Get-Item $shot).Length -gt 0)) {
        Pass "wrote $shot"
    } else {
        Fail "screenshot exit $($r.ExitCode) path=$shot $($r.Stdout) $($r.Stderr)"
    }

    Write-Case "profiler enable / status / disable"
    $en = Invoke-Cli -CliArgs @("profiler", "enable")
    $on = Invoke-Cli -CliArgs @("profiler", "status")
    $dis = Invoke-Cli -CliArgs @("profiler", "disable")
    $off = Invoke-Cli -CliArgs @("profiler", "status")
    if ($en.ExitCode -eq 0 -and $dis.ExitCode -eq 0 -and $on.Stdout -match '"enabled"\s*:\s*true' -and $off.Stdout -match '"enabled"\s*:\s*false') {
        Pass "enabled then disabled"
    } else {
        Fail "enable='$($on.Stdout)' disable='$($off.Stdout)'"
    }

    Write-Case "editor refresh"
    $r = Invoke-Cli -CliArgs @("editor", "refresh")
    if ($r.ExitCode -eq 0) { Pass "refresh" }
    else { Fail "refresh: $($r.Stderr)" }

    Write-Case "test --filter with no matches"
    $r = Invoke-Cli -CliArgs @("test", "--filter", "UnityCliLiveTestDoesNotExist")
    if ($r.ExitCode -eq 0 -and $r.Stdout -match '"total"\s*:\s*0') { Pass "runner returned 0 tests" }
    else { Fail "test filter: exit $($r.ExitCode) $($r.Stdout) $($r.Stderr)" }

    Write-Case "play --wait, pause, hierarchy, stop"
    $play = Invoke-Cli -CliArgs @("editor", "play", "--wait")
    if ($play.ExitCode -ne 0) {
        Fail "play --wait: $($play.Stderr)"
    } else {
        $playState = Wait-UnityState -Want @("playing", "paused")
        $playing = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
        Invoke-Cli -CliArgs @("profiler", "enable") | Out-Null
        $hier = $null
        $hierDeadline = [datetime]::UtcNow.AddMilliseconds([Math]::Min($TimeoutMs, 30000))
        while ([datetime]::UtcNow -lt $hierDeadline) {
            $hier = Invoke-Cli -CliArgs @("profiler", "hierarchy", "--max", "8")
            if ($hier.ExitCode -eq 0 -and $hier.Stdout) { break }
            Start-Sleep -Milliseconds 200
        }
        $pause = Invoke-Cli -CliArgs @("editor", "pause")
        $pausedState = Wait-UnityState -Want @("paused")
        $paused = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPaused.ToString();'
        $stop = Invoke-Cli -CliArgs @("editor", "stop")
        $readyState = Wait-UnityState -Want @("ready")
        $after = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
        $dis = Invoke-Cli -CliArgs @("profiler", "disable")

        if ($playState -match 'playing|paused' -and $playing.Stdout.Trim() -eq "True" `
            -and $hier.ExitCode -eq 0 -and $hier.Stdout `
            -and $pause.ExitCode -eq 0 -and $pausedState -eq "paused" -and $paused.Stdout.Trim() -eq "True" `
            -and $stop.ExitCode -eq 0 -and $readyState -eq "ready" -and $after.Stdout.Trim() -eq "False" `
            -and $dis.ExitCode -eq 0) {
            Pass "played, paused, hierarchy, stopped"
        } else {
            Fail "playState=$playState playing=$($playing.Stdout) hier=$($hier.ExitCode) pause=$pausedState/$($paused.Stdout) stop=$readyState/$($after.Stdout)"
        }
    }
}
finally {
    if (Test-Path $Cli) {
        Invoke-Cli -CliArgs @("editor", "stop") | Out-Null
        Invoke-Cli -CliArgs @("profiler", "disable") | Out-Null
    }
    Write-Host ""
    Write-Host "RESULT  pass=$script:Pass  fail=$script:Fail" -ForegroundColor $(if ($script:Fail -eq 0) { "Green" } else { "Red" })
    if (Test-Path $WorkDir) { Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue }
}

if ($script:Fail -gt 0) { exit 1 }
exit 0
