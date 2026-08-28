# live-test.ps1
#
# Unity Editor가 켜져 있고 Connector가 붙어 있을 때, 방금 빌드한 CLI로
# 실제 HTTP 명령을 보낸다. go test의 목 send가 아니라 한 번 보내기 계약을 본다.
#
# 확인하는 것:
#   - 한 CLI 호출 = 응답 하나
#   - 겹치는 요청도 각각 답이 온다 (유실/재전송 없음)
#   - 메인 스레드가 바쁠 때 다음 요청은 기다렸다가 실행된다
#   - 주요 명령(list/exec/console/menu/screenshot/profiler/refresh/test/play)
#
# 하지 않는 것:
#   - RequestScriptCompilation 으로 프로젝트 전체를 다시 컴파일시키기
#   - 짐작한 초만큼 sleep 하고 끝났다고 가정하기
#   대기는 status/heartbeat를 다시 읽어서 한다. TimeoutMs는 마지막 상한일 뿐이다.
#
#   powershell -File scripts/live-test.ps1
#   powershell -File scripts/live-test.ps1 -Project C:/path/to/unity-project

param(
    [string]$Project = "",
    [int]$TimeoutMs = 180000
)

$ErrorActionPreference = "Continue"
$RepoRoot = Split-Path -Parent $PSScriptRoot
# 빌드 결과와 stdin/stdout 임시 파일. 끝나면 finally에서 지운다.
$WorkDir = Join-Path $env:TEMP ("unity-cli-live-test-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$Cli = Join-Path $WorkDir "unity-cli-live.exe"
# slow exec가 메인 스레드를 붙잡는 시간. fast가 이보다 짧으면 병렬로 샌 것이다.
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

# Start-Process -Wait 직후 ExitCode가 $null인 Windows 경우가 있다. 그때는 성공(0)으로 본다.
function Read-CliExit($process) {
    $exit = $process.ExitCode
    if ($null -eq $exit) { return 0 }
    return [int]$exit
}

# 설치된 unity-cli가 아니라, 방금 빌드한 $Cli를 한 번 실행하고 끝날 때까지 기다린다.
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

# exec는 코드를 stdin으로 받는다. 셸 이스케이프를 피하려고 임시 파일 redirect를 쓴다.
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

# 기다리지 않고 CLI를 띄운다. 겹치는 요청을 같은 순간에 넣기 위한 것.
# Code가 있으면 exec stdin, 없으면 editor/profiler처럼 인자만 있는 명령.
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

# 백그라운드 CLI가 끝날 때까지 프로세스 종료를 기다린다. sleep으로 시간을 짐작하지 않는다.
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

# heartbeat를 읽는다. /command는 안 보낸다. 출력은 "Unity: ready" 한 줄.
function Get-UnityState {
    $r = Invoke-Cli -CliArgs @("status")
    $text = "$(if ($r.Stdout) { $r.Stdout } else { '' })`n$(if ($r.Stderr) { $r.Stderr } else { '' })"
    if ($text -match 'Unity:\s+(\S+)') { return $Matches[1].Trim() }
    if ($text -match 'not responding') { return "not-responding" }
    return ""
}

# 원하는 state가 찍힐 때까지 status를 다시 읽는다.
# 200ms는 폴링 간격이다. "N초 후면 됐을 것"이라는 대기가 아니다.
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
    # CLI와 Connector는 같은 버전이어야 한다. 숫자는 여기 박지 않고 package.json을 따른다.
    $pkgPath = Join-Path $RepoRoot "unity-connector\package.json"
    $version = (Get-Content -Raw $pkgPath | ConvertFrom-Json).version
    if (-not $version) { throw "connector version missing in package.json" }

    Write-Host "Building current CLI $version into $WorkDir"
    Push-Location $RepoRoot
    go build -ldflags "-X main.Version=$version" -o $Cli .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    # 이후 케이스는 Unity가 살아 있어야 한다. 없으면 여기서 끝낸다.
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

    # 한 번 보내면 응답 하나. 1+1이 그대로 와야 한다.
    Write-Case "exec once"
    $r = Invoke-CliStdin -CliArgs @("exec") -Code 'return (1+1).ToString();'
    if ($r.ExitCode -eq 0 -and $r.Stdout.Trim() -eq "2") { Pass "2" }
    else { Fail "exec 1+1 => exit $($r.ExitCode) out='$($r.Stdout)' err='$($r.Stderr)'" }

    # async/deferred 코드는 Unity에 넣기 전에 CLI가 막는다.
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

    # 세 프로세스를 거의 동시에 띄운다. Connector가 직렬로 처리해도 답은 A,B,C 모두 와야 한다.
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

    # slow가 메인 스레드를 $SlowMs 동안 붙잡는다. fast가 그보다 빨리 끝나면 겹쳐서 샌 것이다.
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

    # 같은 명령 두 번도 각각 한 응답. 한쪽이 드롭되거나 재사용되면 안 된다.
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

    # 프로젝트 Screenshots/에 남기지 않고 임시 폴더에 작은 PNG만 쓴다.
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

    # --compile은 안 넣는다. 에셋 리프레시만 요청한다.
    Write-Case "editor refresh"
    $r = Invoke-Cli -CliArgs @("editor", "refresh")
    if ($r.ExitCode -eq 0) { Pass "refresh" }
    else { Fail "refresh: $($r.Stderr)" }

    # 전체 테스트 스위트가 아니라 러너가 살아 있는지만 본다.
    Write-Case "test --filter with no matches"
    $r = Invoke-Cli -CliArgs @("test", "--filter", "UnityCliLiveTestDoesNotExist")
    if ($r.ExitCode -eq 0 -and $r.Stdout -match '"total"\s*:\s*0') { Pass "runner returned 0 tests" }
    else { Fail "test filter: exit $($r.ExitCode) $($r.Stdout) $($r.Stderr)" }

    # play --wait는 HTTP를 붙잡지 않고 heartbeat가 playing이 될 때까지 폴링한다.
    # 끝난 뒤 status/exec로 다시 읽고, pause → hierarchy → stop도 같은 식으로 확인한다.
    Write-Case "play --wait, pause, hierarchy, stop"
    $play = Invoke-Cli -CliArgs @("editor", "play", "--wait")
    if ($play.ExitCode -ne 0) {
        Fail "play --wait: $($play.Stderr)"
    } else {
        $playState = Wait-UnityState -Want @("playing", "paused")
        $playing = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
        # play 중 Game 뷰는 Overlay UI까지 들어가야 한다. ScreenCapture가 파일을 쓸 때까지 기다린다.
        $gameShot = (Join-Path $WorkDir "game.png").Replace("\", "/")
        $game = Invoke-Cli -CliArgs @("screenshot", "--view", "game", "--output_path", $gameShot)
        if ($game.ExitCode -ne 0 -or -not (Test-Path $gameShot) -or ((Get-Item $gameShot).Length -le 0)) {
            Fail "game screenshot: exit $($game.ExitCode) $($game.Stdout) $($game.Stderr)"
        }

        Invoke-Cli -CliArgs @("profiler", "enable") | Out-Null
        # play 직후 프레임이 없을 수 있다. hierarchy 데이터가 올 때까지 다시 요청한다.
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
            -and $game.ExitCode -eq 0 -and (Test-Path $gameShot) `
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
    # 중간 실패로 play/profiler가 켜져 있을 수 있다. 다음 실행을 위해 되돌린다.
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
