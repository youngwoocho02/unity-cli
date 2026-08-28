# live-test.ps1
#
# Unity Editor가 켜져 있고 Connector가 붙어 있을 때, 방금 빌드한 CLI로
# 실제 HTTP 명령을 보낸다. go test의 목 send가 아니라 한 번 보내기 계약을 본다.
#
# 한 번 실행하면 아래를 모두 확인한다.
#   - CLI 로컬: version / help (Unity 없이)
#   - 연결: status, Connector 버전, list의 내장 도구
#   - exec: stdin/위치인자/--params/--usings, 컴파일·런타임 오류,
#           async 기본 차단, --allow-async
#   - 한 CLI 호출 = 응답 하나. 겹치는 요청도 각각 답이 온다
#   - 메인 스레드가 바쁠 때 다음 요청은 기다렸다가 실행된다
#   - console 필터/스택/줄수, menu(차단 포함), screenshot(scene/game/IEND)
#   - profiler enable/status/clear/hierarchy/disable
#   - editor play(--wait 없음 포함)/pause/stop/refresh(--force)
#   - manage_editor: tool/tag/layer
#   - reserialize: 임시 에셋 하나 (프로젝트 전체는 안 함)
#   - test: EditMode/PlayMode 빈 필터, 잘못된 mode
#
# 하지 않는 것:
#   - RequestScriptCompilation 으로 프로젝트 전체를 다시 컴파일시키기
#   - reserialize 인자 없이 프로젝트 전체를 다시 쓰기
#   - File/Quit 실행 (차단만 확인)
#   - 짐작한 초만큼 sleep 하고 끝났다고 가정하기
#   대기는 status/heartbeat를 다시 읽어서 한다. TimeoutMs는 마지막 상한일 뿐이다.
#
# 순서: Unity 없이 version/help → 연결/list → exec·겹침 → console/menu/scene 샷
# → profiler/refresh/tag → 임시 reserialize → 빈 test → 마지막에 play 세션.
# play를 뒤로 미루는 이유: play 중 refresh/tag가 막히고, 실패해도 finally가 stop 한다.
#
#   powershell -File scripts/live-test.ps1
#   powershell -File scripts/live-test.ps1 -Project C:/path/to/unity-project

# $Project 비우면 heartbeat 인스턴스 한 대를 고른다. 여러 Editor면 경로를 넘긴다.
# $TimeoutMs는 각 CLI 호출의 상한이다. 케이스마다 sleep(N초)로 끝내지 않는다.
param(
    [string]$Project = "",
    [int]$TimeoutMs = 180000
)

$ErrorActionPreference = "Continue"
$RepoRoot = Split-Path -Parent $PSScriptRoot
# 설치된 unity-cli가 아니라 이 실행 전용 바이너리/입출력을 둔다. 프로젝트 Assets에는 안 쓴다.
# GUID를 붙여 겹친 live-test가 서로의 파일을 덮어쓰지 않게 한다. finally에서 폴더째 지운다.
$WorkDir = Join-Path $env:TEMP ("unity-cli-live-test-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$Cli = Join-Path $WorkDir "unity-cli-live.exe"
# slow exec가 메인 스레드를 붙잡는 시간. fast가 이보다 짧으면 두 명령이 겹쳐 샌 것이다.
$SlowMs = 400
# TagManager/Assets에 남기지 않을 임시 이름. 이전 실행이 남겼으면 지운 뒤 다시 만들고, finally에서도 지운다.
$script:LiveTag = "UCliLiveTag"
$script:LiveLayer = "UCliLiveLy"
$script:LiveAsset = "Assets/_UnityCliLiveTest.txt"

$script:Fail = 0
$script:Pass = 0

# 케이스 제목만 찍는다. Unity에는 아무것도 보내지 않는다.
function Write-Case($name) {
    Write-Host ""
    Write-Host "===== $name =====" -ForegroundColor Cyan
}

function Pass($detail) {
    $script:Pass++
    if ($detail) { Write-Host "PASS  $detail" -ForegroundColor Green }
    else { Write-Host "PASS" -ForegroundColor Green }
}

# 실패해도 다음 케이스를 이어서 본다. 중간 throw는 Unity가 없을 때뿐이다.
function Fail($detail) {
    $script:Fail++
    Write-Host "FAIL  $detail" -ForegroundColor Red
}

# 성공 데이터는 stdout, 실패 메시지는 stderr. 한쪽만 보면 놓친다.
function Get-CombinedText($r) {
    return "$(if ($r.Stdout) { $r.Stdout } else { '' })`n$(if ($r.Stderr) { $r.Stderr } else { '' })"
}

# Start-Process -Wait 직후 ExitCode가 $null인 Windows 경우가 있다.
# 프로세스는 이미 끝났는데 코드만 비는 것이라, 실패로 보지 않고 0으로 본다.
function Read-CliExit($process) {
    $exit = $process.ExitCode
    if ($null -eq $exit) { return 0 }
    return [int]$exit
}

# Game 뷰는 ScreenCapture가 다음 프레임에 파일을 쓴다. length>0만 보면 쓰기 중인 잘린 PNG를 통과시킨다.
# 커넥터와 같이 끝 8바이트 IEND+CRC를 본다. 파일은 읽기만 하고 Unity에는 안 보낸다.
function Test-PngComplete([string]$path) {
    if (-not (Test-Path $path)) { return $false }
    $fs = $null
    try {
        # ReadWrite 공유: ScreenCapture가 아직 닫지 않은 파일을 전용 잠그면 여기서 예외가 난다.
        $fs = [IO.File]::Open($path, [IO.FileMode]::Open, [IO.FileAccess]::Read, [IO.FileShare]::ReadWrite)
        if ($fs.Length -lt 8) { return $false }
        $null = $fs.Seek(-8, [IO.SeekOrigin]::End)
        $tail = New-Object byte[] 8
        if ($fs.Read($tail, 0, 8) -ne 8) { return $false }
        $iend = [byte[]](0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82)
        for ($i = 0; $i -lt 8; $i++) {
            if ($tail[$i] -ne $iend[$i]) { return $false }
        }
        return $true
    } catch {
        return $false
    } finally {
        if ($fs) { $fs.Dispose() }
    }
}

# Start-Process는 인자 배열을 공백으로 이어 한 줄로 만든 뒤 CreateProcess가 다시 쪼갠다.
# 공백/따옴표가 있는 exec 코드와 --params JSON은 여기서 따옴표를 붙여야 한 인자로 남는다.
function Join-ProcessArgs([string[]]$Values) {
    $parts = foreach ($v in $Values) {
        if ($v -notmatch '[\s"]') { $v }
        else {
            $escaped = [regex]::Replace($v, '(\\*)"', '$1$1\"')
            $escaped = [regex]::Replace($escaped, '(\\+)$', '$1$1')
            '"' + $escaped + '"'
        }
    }
    return ($parts -join ' ')
}

# 설치된 unity-cli가 아니라 방금 빌드한 $Cli를 한 번 실행하고 프로세스가 끝날 때까지 기다린다.
# --project/--timeout을 여기서 붙인다. 호출측은 명령 인자만 넘긴다.
# Start-Process로 띄우는 이유: 겹친 요청과 stdin redirect를 같은 방식으로 다루기 위해서다.
# 출력을 변수에 모으지 않고 파일로 받는다. 큰 JSON/로그가 파이프라인을 막지 않게.
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
    $p = Start-Process -FilePath $Cli -ArgumentList (Join-ProcessArgs $all) -NoNewWindow -Wait -PassThru `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $sw.Stop()
    [pscustomobject]@{
        ExitCode = Read-CliExit $p
        Stdout   = Get-FileText $outFile
        Stderr   = Get-FileText $errFile
        Ms       = $sw.ElapsedMilliseconds
    }
}

# Windows PowerShell에서 빈 파일 Get-Content -Raw는 $null이다. Trim()이 터지지 않게 빈 문자열로 만든다.
function Get-FileText([string]$path) {
    $raw = Get-Content -Raw $path -ErrorAction SilentlyContinue
    if ($null -eq $raw) { return "" }
    return $raw
}

# exec 코드를 stdin으로 넣는다. 인자 문자열로 넘기면 PowerShell/따옴표가 코드를 깨뜨린다.
# 임시 .cs를 RedirectStandardInput으로 붙인다. Unity 쪽 --code 플래그를 새로 만들지 않는다.
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
    $p = Start-Process -FilePath $Cli -ArgumentList (Join-ProcessArgs $all) -NoNewWindow -Wait -PassThru `
        -RedirectStandardInput $inFile `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
    $sw.Stop()
    [pscustomobject]@{
        ExitCode = Read-CliExit $p
        Stdout   = Get-FileText $outFile
        Stderr   = Get-FileText $errFile
        Ms       = $sw.ElapsedMilliseconds
    }
}

# 프로세스를 띄우고 바로 리턴한다. 같은 순간에 여러 /command를 넣기 위한 것이다.
# -Wait를 쓰면 첫 호출이 끝난 뒤에야 둘째가 나가, 겹침/직렬화를 검증할 수 없다.
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
        ArgumentList           = (Join-ProcessArgs $all)
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

# 백그라운드 CLI 프로세스가 끝날 때까지 기다린다. "아마 N초면 끝났을 것" sleep은 쓰지 않는다.
# Ms는 시작 시각부터의 경과다. Connector 직렬화 때문에 fast가 slow보다 길어야 하는 비교에 쓴다.
function Wait-CliJob($job) {
    if (-not $job.Process.HasExited) {
        $job.Process.WaitForExit()
    }
    $out = Get-FileText $job.OutFile
    $err = Get-FileText $job.ErrFile
    [pscustomobject]@{
        ExitCode = Read-CliExit $job.Process
        Stdout   = $out.Trim()
        Stderr   = $err.Trim()
        Ms       = [int]((Get-Date) - $job.Started).TotalMilliseconds
    }
}

# status만 보낸다. /command는 안 보낸다. heartbeat 파일의 state를 읽는다.
# 출력 한 줄 "Unity: ready|playing|paused|compiling|reloading". 파싱 실패면 빈 문자열.
function Get-UnityState {
    $r = Invoke-Cli -CliArgs @("status")
    $text = Get-CombinedText $r
    if ($text -match 'Unity:\s+(\S+)') { return $Matches[1].Trim() }
    if ($text -match 'not responding') { return "not-responding" }
    return ""
}

# 원하는 heartbeat state가 찍힐 때까지 status를 다시 읽는다. play/stop/compile을 여기서 보내지 않는다.
# 200ms는 폴링 간격이다. play 전환·도메인 리로드 시간을 초로 짐작하지 않는다.
# Timeout은 마지막 상한. 그 안에 안 오면 마지막 state를 그대로 돌려 호출측이 Fail 하게 한다.
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

# 실패 중간에 끊겨도 다음 실행이 깨끗한 Editor를 보게 되돌린다.
# play/profiler를 끄고, 이 스크립트가 만든 tag/layer/임시 에셋만 지운다.
# 프로젝트 전체 refresh/--compile/reserialize는 보내지 않는다. 없어도 되는 삭제는 실패를 무시한다.
function Invoke-CleanupUnity {
    if (-not (Test-Path $Cli)) { return }
    Invoke-Cli -CliArgs @("editor", "stop") | Out-Null
    Invoke-Cli -CliArgs @("profiler", "disable") | Out-Null
    Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_tag", "--tag_name", $script:LiveTag) | Out-Null
    Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_layer", "--layer_name", $script:LiveLayer) | Out-Null
    $del = "if (AssetDatabase.LoadAssetAtPath<UnityEngine.Object>(`"$($script:LiveAsset)`") != null) { AssetDatabase.DeleteAsset(`"$($script:LiveAsset)`"); AssetDatabase.SaveAssets(); } return `"ok`";"
    Invoke-CliStdin -CliArgs @("exec") -Code $del | Out-Null
}

try {
    # CLI와 Connector는 같은 숫자여야 한다. 여기 0.x.y를 박지 않고 package.json을 읽는다.
    # -ldflags로 그 숫자를 바이너리에 넣는다. 소스 기본값 dev면 버전 검사를 건너뛰어 이 테스트가 무의미해진다.
    $pkgPath = Join-Path $RepoRoot "unity-connector\package.json"
    $version = (Get-Content -Raw $pkgPath | ConvertFrom-Json).version
    if (-not $version) { throw "connector version missing in package.json" }

    Write-Host "Building current CLI $version into $WorkDir"
    Push-Location $RepoRoot
    go build -ldflags "-X main.Version=$version" -o $Cli .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    # version/help는 DiscoverInstance 전에 끝난다. Unity가 꺼져 있어도 이 케이스는 돌아야 한다.
    # screenshot help에 Overlay가 있는지로 Game 뷰 계약이 문서에 남았는지 본다. /command는 안 보낸다.
    Write-Case "version / help (no Unity)"
    $ver = Invoke-Cli -CliArgs @("version")
    $help = Invoke-Cli -CliArgs @("help")
    $editorHelp = Invoke-Cli -CliArgs @("editor", "--help")
    $shotHelp = Invoke-Cli -CliArgs @("screenshot", "--help")
    if ($ver.ExitCode -eq 0 -and $ver.Stdout -match [regex]::Escape("unity-cli $version") `
        -and $help.ExitCode -eq 0 -and $help.Stdout -match "screenshot" `
        -and $editorHelp.ExitCode -eq 0 -and $editorHelp.Stdout -match "play" `
        -and $shotHelp.ExitCode -eq 0 -and $shotHelp.Stdout -match "Overlay") {
        Pass "version=$version, help texts present"
    } else {
        Fail "version='$($ver.Stdout)' help exit=$($help.ExitCode) editorHelp=$($editorHelp.ExitCode)"
    }

    # 이후는 전부 /command다. Editor가 없거나 Connector 버전이 다르면 여기서 멈춘다.
    # status는 heartbeat만 읽는다. 버전이 다른데 명령을 넣으면 실패 원인이 섞인다.
    Write-Case "status (Unity must be running, versions match)"
    $r = Invoke-Cli -CliArgs @("status")
    Write-Host $r.Stdout
    $verOk = $r.Stdout -match ("Connector:\s+" + [regex]::Escape($version))
    if ($r.ExitCode -eq 0 -and $r.Stdout -match "Unity:" -and $verOk) { Pass $r.Stdout.Trim() }
    else { Fail "status failed or version mismatch: $($r.Stdout) $($r.Stderr)" ; throw "Unity is not reachable. Open the Editor with the Connector and rerun." }

    # 이전 live-test나 수동 play가 남아 있으면 refresh/tag 케이스가 play-mode 규칙에 막힌다.
    # playing/paused면 stop 한 뒤 ready를 다시 읽는다. stop 응답만 보고 끝난 줄 알지 않는다.
    $ready = Wait-UnityState -Want @("ready", "playing", "paused")
    if ($ready -eq "playing" -or $ready -eq "paused") {
        Invoke-Cli -CliArgs @("editor", "stop") | Out-Null
        $ready = Wait-UnityState -Want @("ready")
    }
    if ($ready -ne "ready") { throw "Unity did not become ready (state=$ready). Stop play mode and rerun." }

    # list는 커넥터가 리플렉션으로 모은 도구 이름이다. CLI 헬프가 아니라 실제 등록을 본다.
    # manage_editor/refresh_unity/run_tests는 속성 Name이 없어 클래스 스네이크 케이스다.
    Write-Case "list tools"
    $r = Invoke-Cli -CliArgs @("list")
    $need = @("exec", "console", "menu", "screenshot", "profiler", "manage_editor", "refresh_unity", "reserialize", "run_tests")
    $missing = @($need | Where-Object { $r.Stdout -notmatch [regex]::Escape("`"$($_)`"") -and $r.Stdout -notmatch [regex]::Escape("name`: $_") -and $r.Stdout -notmatch "`"name`": `"$_`"" })
    if ($r.ExitCode -eq 0 -and $missing.Count -eq 0) { Pass ("listed " + ($need -join ", ")) }
    else { Fail "list missing $($missing -join ', '): exit $($r.ExitCode)" }

    # 한 호출 = 응답 하나. stdin 코드가 그대로 컴파일·실행되고 1+1=2가 나와야 한다.
    Write-Case "exec once (stdin)"
    $r = Invoke-CliStdin -CliArgs @("exec") -Code 'return (1+1).ToString();'
    if ($r.ExitCode -eq 0 -and $r.Stdout.Trim() -eq "2") { Pass "2" }
    else { Fail "exec 1+1 => exit $($r.ExitCode) out='$($r.Stdout)' err='$($r.Stderr)'" }

    # 코드를 넣는 세 길을 모두 본다. 위치 인자, --params JSON, stdin(위 케이스).
    # --usings가 없으면 CultureInfo는 기본 using 밖이라 컴파일이 실패해야 한다.
    Write-Case "exec positional / --params / --usings"
    $pos = Invoke-Cli -CliArgs @("exec", "return 4;")
    $json = Invoke-Cli -CliArgs @("exec", "--params", '{"code":"return 3;"}')
    $usingOk = Invoke-Cli -CliArgs @("exec", "return typeof(CultureInfo).Name;", "--usings", "System.Globalization")
    $usingFail = Invoke-Cli -CliArgs @("exec", "return typeof(CultureInfo).Name;")
    if ($pos.ExitCode -eq 0 -and $pos.Stdout.Trim() -eq "4" `
        -and $json.ExitCode -eq 0 -and $json.Stdout.Trim() -eq "3" `
        -and $usingOk.ExitCode -eq 0 -and $usingOk.Stdout.Trim() `
        -and $usingFail.ExitCode -ne 0) {
        Pass "positional=4 params=3 usings ok, missing using fails"
    } else {
        Fail "pos=$($pos.ExitCode)/'$($pos.Stdout)' json=$($json.ExitCode)/'$($json.Stdout)' usingOk=$($usingOk.ExitCode)/'$($usingOk.Stdout)' usingFail=$($usingFail.ExitCode)"
    }

    # 잘못된 코드도 응답이 와야 한다. CLI가 타임아웃하거나 빈 실패로 삼키면 안 된다.
    # 컴파일 실패와 throw는 둘 다 exit!=0. 런타임은 메시지에 live-test-boom이 남아야 한다.
    Write-Case "exec compile / runtime errors"
    $compile = Invoke-CliStdin -CliArgs @("exec") -Code 'return this_symbol_does_not_exist;'
    $runtime = Invoke-CliStdin -CliArgs @("exec") -Code 'throw new InvalidOperationException("live-test-boom");'
    $compileText = Get-CombinedText $compile
    $runtimeText = Get-CombinedText $runtime
    if ($compile.ExitCode -ne 0 -and $compileText -match "Compile error|error" `
        -and $runtime.ExitCode -ne 0 -and $runtimeText -match "live-test-boom") {
        Pass "compile and runtime errors surfaced"
    } else {
        Fail "compile=$($compile.ExitCode) '$compileText' runtime=$($runtime.ExitCode) '$runtimeText'"
    }

    # async/코루틴/delayCall은 Unity에 넣기 전에 CLI가 막는다. 커넥터까지 가면 응답 전에 끝나지 않는다.
    # --allow-async는 그 검사를 건너뛴다. 여기서는 typeof(Task).Name만 실행한다. 실제 Delay는 돌리지 않는다.
    Write-Case "async exec is blocked, --allow-async lets it through"
    $blockedAwait = Invoke-CliStdin -CliArgs @("exec") -Code 'await Task.Delay(1); return "no";'
    $blockedDelay = Invoke-CliStdin -CliArgs @("exec") -Code 'EditorApplication.delayCall += () => {}; return "no";'
    $blockedCo = Invoke-CliStdin -CliArgs @("exec") -Code 'StartCoroutine(null); return "no";'
    $allowed = Invoke-CliStdin -CliArgs @("exec", "--allow-async") -Code 'return typeof(Task).Name;'
    $blockedText = "$(Get-CombinedText $blockedAwait)$(Get-CombinedText $blockedDelay)$(Get-CombinedText $blockedCo)"
    if ($blockedAwait.ExitCode -ne 0 -and $blockedDelay.ExitCode -ne 0 -and $blockedCo.ExitCode -ne 0 `
        -and $blockedText -match "allow-async" `
        -and $allowed.ExitCode -eq 0 -and $allowed.Stdout.Trim() -eq "Task") {
        Pass "blocked await/delayCall/StartCoroutine; --allow-async returned Task"
    } else {
        Fail "await=$($blockedAwait.ExitCode) delay=$($blockedDelay.ExitCode) co=$($blockedCo.ExitCode) allow='$($allowed.Stdout)'"
    }

    # 없는 도구 이름은 커넥터가 Unknown command로 거절한다. 성공한 척 빈 응답이 오면 안 된다.
    Write-Case "unknown command fails"
    $r = Invoke-Cli -CliArgs @("nonexistent_command_xyz")
    if ($r.ExitCode -ne 0 -and $r.Stderr -match "Unknown command") { Pass "rejected" }
    else { Fail "expected unknown command error, got exit $($r.ExitCode) $($r.Stderr)" }

    # 같은 연결로 다섯 번 연속. 이전 응답이 다음 입력에 섞이거나 카운터가 어긋나면 여기서 걸린다.
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

    # 세 CLI를 거의 동시에 띄운다. Connector SemaphoreSlim이 직렬로 처리해도 답은 A,B,C 모두 와야 한다.
    # 유실·재전송·한쪽 응답을 세 번 돌려주는 버그를 본다. 순서는 상관 없어서 Sort 후 비교한다.
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

    # slow가 메인 스레드를 $SlowMs 동안 바쁜 루프로 붙잡는다. Sleep이 아니라 메인 스레드를 실제로 막는다.
    # fast를 바로 뒤에 띄운다. Connector가 한 번에 하나만 실행하면 fast 경과가 SlowMs에 가깝다.
    # fast가 SlowMs보다 훨씬 짧으면 두 HandleCommand가 겹친 것이다. -50ms는 타이머 오차 여유.
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

    # 내용이 같은 exec 두 개. 캐시/중복 제거가 있으면 한쪽이 드롭되거나 같은 버퍼를 두 번 준다.
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

    # 콘솔을 비운 뒤 log/warn/error를 하나씩 남기고 타입·줄수·스택 옵션을 읽는다.
    # --type log에 warn이 보이면 필터가 무시된 것이다. --lines 1은 lt-log가 한 번만.
    # 마지막 clear 이후 lt-log가 남으면 clear가 안 된 것이다. 프로젝트 기존 로그에 의존하지 않는다.
    Write-Case "console clear / types / lines / stacktrace"
    $clear = Invoke-Cli -CliArgs @("console", "--clear")
    $log = Invoke-CliStdin -CliArgs @("exec") -Code 'Debug.Log("lt-log"); Debug.LogWarning("lt-warn"); Debug.LogError("lt-err"); return "logged";'
    $all = Invoke-Cli -CliArgs @("console", "--type", "error,warning,log", "--lines", "50")
    $onlyLog = Invoke-Cli -CliArgs @("console", "--type", "log", "--lines", "50")
    $onlyWarn = Invoke-Cli -CliArgs @("console", "--type", "warning", "--lines", "50")
    $onlyErr = Invoke-Cli -CliArgs @("console", "--type", "error", "--lines", "50")
    $oneLine = Invoke-Cli -CliArgs @("console", "--type", "log", "--lines", "1")
    $none = Invoke-Cli -CliArgs @("console", "--type", "log", "--stacktrace", "none", "--lines", "20")
    $full = Invoke-Cli -CliArgs @("console", "--type", "log", "--stacktrace", "full", "--lines", "20")
    $cleared = Invoke-Cli -CliArgs @("console", "--clear")
    $afterClear = Invoke-Cli -CliArgs @("console", "--type", "log", "--lines", "20")
    $typesOk = $all.Stdout -match "lt-log" -and $all.Stdout -match "lt-warn" -and $all.Stdout -match "lt-err" `
        -and $onlyLog.Stdout -match "lt-log" -and $onlyLog.Stdout -notmatch "lt-warn" `
        -and $onlyWarn.Stdout -match "lt-warn" -and $onlyWarn.Stdout -notmatch "lt-log" `
        -and $onlyErr.Stdout -match "lt-err" -and $onlyErr.Stdout -notmatch "lt-log"
    $linesOk = ($oneLine.Stdout -split "lt-log").Count -eq 2
    $stackOk = $none.Stdout -match "lt-log" -and $full.Stdout -match "lt-log"
    $clearOk = $afterClear.Stdout -notmatch "lt-log"
    if ($clear.ExitCode -eq 0 -and $log.Stdout.Trim() -eq "logged" -and $cleared.ExitCode -eq 0 `
        -and $typesOk -and $linesOk -and $stackOk -and $clearOk) {
        Pass "filtered types, lines=1, stacktrace, cleared"
    } else {
        Fail "typesOk=$typesOk linesOk=$linesOk stackOk=$stackOk clearOk=$clearOk all='$($all.Stdout)'"
    }

    # Console 메뉴는 창만 연다. 저장/빌드/종료는 보내지 않는다.
    # File/Quit는 커넥터 블랙리스트라 실패해야 한다. 성공하면 Editor가 꺼진다.
    Write-Case "menu / File/Quit blocked / unknown menu"
    $menu = Invoke-Cli -CliArgs @("menu", "Window/General/Console")
    $quit = Invoke-Cli -CliArgs @("menu", "File/Quit")
    $badMenu = Invoke-Cli -CliArgs @("menu", "Window/DoesNotExist/LiveTest")
    $quitText = Get-CombinedText $quit
    $badText = Get-CombinedText $badMenu
    if ($menu.ExitCode -eq 0 -and (Get-CombinedText $menu) -match "Console" `
        -and $quit.ExitCode -ne 0 -and $quitText -match "blocked" `
        -and $badMenu.ExitCode -ne 0 -and $badText -match "Failed to execute|not") {
        Pass "Console ok, File/Quit blocked, unknown rejected"
    } else {
        Fail "menu=$($menu.ExitCode) quit=$($quit.ExitCode) '$quitText' bad=$($badMenu.ExitCode)"
    }

    # Scene 뷰는 카메라 Render라 width/height가 JSON과 파일에 그대로 반영돼야 한다.
    # 프로젝트 Screenshots/는 건드리지 않고 $WorkDir에 64x64만 쓴다. 잘못된 view는 거절.
    Write-Case "screenshot scene / size / IEND / bad view"
    $shot = (Join-Path $WorkDir "shot.png").Replace("\", "/")
    $r = Invoke-Cli -CliArgs @("screenshot", "--view", "scene", "--width", "64", "--height", "64", "--output_path", $shot)
    $badView = Invoke-Cli -CliArgs @("screenshot", "--view", "not-a-view", "--output_path", $shot)
    if ($r.ExitCode -eq 0 -and $r.Stdout -match '"view"\s*:\s*"scene"' `
        -and $r.Stdout -match '"width"\s*:\s*64' -and $r.Stdout -match '"height"\s*:\s*64' `
        -and (Test-PngComplete $shot) `
        -and $badView.ExitCode -ne 0) {
        Pass "scene 64x64 PNG complete, bad view rejected"
    } else {
        Fail "scene exit $($r.ExitCode) png=$(Test-PngComplete $shot) bad=$($badView.ExitCode) $($r.Stdout)"
    }

    # enable만으로는 Edit Mode 캡처가 안 붙을 수 있어 profileEditor를 켠다. hierarchy 프레임이 생길 때까지 다시 읽는다.
    # disable 뒤 enabled와 profileEditor가 둘 다 false여야 한다. 창 Record만 끄면 Edit Mode가 계속 녹음한다.
    # --compile이나 Profiler 창 메뉴는 열지 않는다.
    Write-Case "profiler enable / status / clear / hierarchy / disable"
    $en = Invoke-Cli -CliArgs @("profiler", "enable")
    $on = Invoke-Cli -CliArgs @("profiler", "status")
    # enable 직후 프레임이 없을 수 있다. 15초 안에서 hierarchy가 올 때까지 다시 요청한다. sleep으로 한 방을 기다리지 않는다.
    Invoke-CliStdin -CliArgs @("exec") -Code 'UnityEditorInternal.ProfilerDriver.profileEditor = true; return "armed";' | Out-Null
    $hier = $null
    $hierDeadline = [datetime]::UtcNow.AddMilliseconds([Math]::Min($TimeoutMs, 15000))
    while ([datetime]::UtcNow -lt $hierDeadline) {
        $hier = Invoke-Cli -CliArgs @("profiler", "hierarchy", "--depth", "2", "--max", "8", "--min", "0", "--sort", "self")
        if ($hier.ExitCode -eq 0 -and $hier.Stdout -match "children|items|name") { break }
        Start-Sleep -Milliseconds 200
    }
    $frames = Invoke-Cli -CliArgs @("profiler", "hierarchy", "--frames", "2", "--max", "8")
    $clearedProf = Invoke-Cli -CliArgs @("profiler", "clear")
    $dis = Invoke-Cli -CliArgs @("profiler", "disable")
    $off = Invoke-Cli -CliArgs @("profiler", "status")
    $offOk = $off.Stdout -match '"enabled"\s*:\s*false' -and $off.Stdout -match '"profileEditor"\s*:\s*false'
    if ($en.ExitCode -eq 0 -and $on.Stdout -match '"enabled"\s*:\s*true' `
        -and $hier.ExitCode -eq 0 -and $hier.Stdout `
        -and $clearedProf.ExitCode -eq 0 -and $dis.ExitCode -eq 0 -and $offOk) {
        Pass "enabled, hierarchy, cleared, disabled (profileEditor off)"
    } else {
        Fail "on='$($on.Stdout)' hier=$($hier.ExitCode) frames=$($frames.ExitCode) off='$($off.Stdout)'"
    }

    # if_dirty 리프레시만 보낸다. --compile은 RequestScriptCompilation이라 프로젝트 전체가 다시 돈다. 넣지 않는다.
    Write-Case "editor refresh (edit mode)"
    $r = Invoke-Cli -CliArgs @("editor", "refresh")
    if ($r.ExitCode -eq 0) { Pass "refresh" }
    else { Fail "refresh: $($r.Stderr)" }

    # editor 서브커맨드에 없는 도구/태그/레이어는 manage_editor 패스스루로 보낸다.
    # 도구는 확인 후 원래 값으로 되돌린다. 태그와 레이어는 추가→중복 실패→삭제까지 보고 TagManager에 남기지 않는다.
    Write-Case "manage_editor tool / tag / layer"
    $prevTool = Invoke-CliStdin -CliArgs @("exec") -Code 'return UnityEditor.Tools.current.ToString();'
    $setTool = Invoke-Cli -CliArgs @("manage_editor", "--action", "set_active_tool", "--tool_name", "Move")
    $nowTool = Invoke-CliStdin -CliArgs @("exec") -Code 'return UnityEditor.Tools.current.ToString();'
    if ($prevTool.Stdout.Trim() -and $prevTool.Stdout.Trim() -ne "None") {
        Invoke-Cli -CliArgs @("manage_editor", "--action", "set_active_tool", "--tool_name", $prevTool.Stdout.Trim()) | Out-Null
    }
    # 이전 실행이 태그를 남겼으면 add가 "already exists"로 실패한다. 먼저 지운 뒤 추가한다.
    Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_tag", "--tag_name", $script:LiveTag) | Out-Null
    $addTag = Invoke-Cli -CliArgs @("manage_editor", "--action", "add_tag", "--tag_name", $script:LiveTag)
    $dupTag = Invoke-Cli -CliArgs @("manage_editor", "--action", "add_tag", "--tag_name", $script:LiveTag)
    $hasTag = Invoke-CliStdin -CliArgs @("exec") -Code "return UnityEditorInternal.InternalEditorUtility.tags.Contains(`"$($script:LiveTag)`").ToString();"
    $rmTag = Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_tag", "--tag_name", $script:LiveTag)
    $goneTag = Invoke-CliStdin -CliArgs @("exec") -Code "return UnityEditorInternal.InternalEditorUtility.tags.Contains(`"$($script:LiveTag)`").ToString();"
    Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_layer", "--layer_name", $script:LiveLayer) | Out-Null
    $addLayer = Invoke-Cli -CliArgs @("manage_editor", "--action", "add_layer", "--layer_name", $script:LiveLayer)
    $hasLayer = Invoke-CliStdin -CliArgs @("exec") -Code "return LayerMask.NameToLayer(`"$($script:LiveLayer)`") >= 0 ? `"True`" : `"False`";"
    $rmLayer = Invoke-Cli -CliArgs @("manage_editor", "--action", "remove_layer", "--layer_name", $script:LiveLayer)
    $badAction = Invoke-Cli -CliArgs @("manage_editor", "--action", "not_an_action")
    if ($setTool.ExitCode -eq 0 -and $nowTool.Stdout.Trim() -eq "Move" `
        -and $addTag.ExitCode -eq 0 -and $dupTag.ExitCode -ne 0 -and $hasTag.Stdout.Trim() -eq "True" `
        -and $rmTag.ExitCode -eq 0 -and $goneTag.Stdout.Trim() -eq "False" `
        -and $addLayer.ExitCode -eq 0 -and $hasLayer.Stdout.Trim() -eq "True" -and $rmLayer.ExitCode -eq 0 `
        -and $badAction.ExitCode -ne 0) {
        Pass "Move tool, tag add/dup/remove, layer add/remove, bad action rejected"
    } else {
        Fail "tool='$($nowTool.Stdout)' tag add=$($addTag.ExitCode) dup=$($dupTag.ExitCode) has=$($hasTag.Stdout) rm=$($rmTag.ExitCode) layer add=$($addLayer.ExitCode) has=$($hasLayer.Stdout) bad=$($badAction.ExitCode)"
    }

    # 인자 없는 reserialize는 프로젝트 전체를 다시 쓴다. 보내지 않는다.
    # 방금 만든 txt 하나만 ForceReserializeAssets 한 뒤 바로 지운다. 기존 프리팹/씬은 안 건드린다.
    Write-Case "reserialize one temp asset"
    $create = Invoke-CliStdin -CliArgs @("exec") -Code @"
var path = "$($script:LiveAsset)";
if (AssetDatabase.LoadAssetAtPath<UnityEngine.Object>(path) != null) AssetDatabase.DeleteAsset(path);
System.IO.File.WriteAllText(path, "unity-cli-live-test");
AssetDatabase.ImportAsset(path);
return path;
"@
    $res = Invoke-Cli -CliArgs @("reserialize", $script:LiveAsset)
    $del = Invoke-CliStdin -CliArgs @("exec") -Code "AssetDatabase.DeleteAsset(`"$($script:LiveAsset)`"); AssetDatabase.SaveAssets(); return `"deleted`";"
    # 성공 시 stdout은 message가 아니라 data JSON({ paths })이다. "Reserialized"를 기다리지 않는다.
    if ($create.ExitCode -eq 0 -and $res.ExitCode -eq 0 -and (Get-CombinedText $res) -match "_UnityCliLiveTest" `
        -and $del.ExitCode -eq 0) {
        Pass "reserialized $script:LiveAsset"
    } else {
        Fail "create=$($create.ExitCode) '$($create.Stdout)' res=$($res.ExitCode) $($res.Stderr) del=$($del.ExitCode)"
    }

    # 프로젝트 테스트를 돌리지 않는다. 없는 필터라 total=0이면 러너·폴링 경로만 산 것이다.
    # PlayMode는 "running" 후 결과 파일을 기다린다. 리로드 중 heartbeat가 사라져도 바로 종료로 보면 안 된다.
    # 잘못된 --mode는 Unity에 보내기 전에 CLI가 거절한다. PlayMode 뒤 play가 남으면 stop하고 ready를 읽는다.
    Write-Case "test EditMode / PlayMode empty filter / bad mode"
    $edit = Invoke-Cli -CliArgs @("test", "--filter", "UnityCliLiveTestDoesNotExist")
    $playTests = Invoke-Cli -CliArgs @("test", "--mode", "PlayMode", "--filter", "UnityCliLiveTestDoesNotExist")
    $badMode = Invoke-Cli -CliArgs @("test", "--mode", "Nope")
    Invoke-Cli -CliArgs @("editor", "stop") | Out-Null
    $null = Wait-UnityState -Want @("ready")
    if ($edit.ExitCode -eq 0 -and $edit.Stdout -match '"total"\s*:\s*0' `
        -and $playTests.ExitCode -eq 0 -and $playTests.Stdout -match '"total"\s*:\s*0' `
        -and $badMode.ExitCode -ne 0 -and (Get-CombinedText $badMode) -match "EditMode or PlayMode") {
        Pass "EditMode/PlayMode 0 tests, bad mode rejected"
    } else {
        Fail "edit=$($edit.ExitCode) '$($edit.Stdout)' play=$($playTests.ExitCode) '$($playTests.Stdout)' bad=$($badMode.ExitCode)"
    }

    # play는 커넥터가 isPlaying=true만 켜고 바로 리턴한다. 실제 진입은 나중이다.
    # --wait 없는 호출은 "요청함"만 보고, playing은 status를 다시 읽어 확인한다. 초를 짐작하지 않는다.
    # Edit Mode에서 pause는 실패, stop은 already stopped여야 한다.
    # 같은 play 세션에서 already-playing, refresh 차단/--force, game ScreenCapture, pause 토글을 본다.
    # Game 뷰는 Overlay가 camera.Render에 안 들어가서 ScreenCapture만 쓴다. IEND가 올 때까지 커넥터가 기다린다.
    # Overlay 픽셀(프로젝트 UI)은 프로젝트마다 달라 여기서 비교하지 않는다. 완료된 game PNG인지만 본다.
    Write-Case "play without --wait, refresh rules, game screenshot, pause toggle, stop"
    $pauseEdit = Invoke-Cli -CliArgs @("editor", "pause")
    $stopEdit = Invoke-Cli -CliArgs @("editor", "stop")
    $play = Invoke-Cli -CliArgs @("editor", "play")
    $playState = Wait-UnityState -Want @("playing", "paused")
    $playing = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
    # 이미 play면 커넥터는 플래그를 다시 안 켠다. "Already in play mode"가 와야 한다.
    $again = Invoke-Cli -CliArgs @("editor", "play")
    # play 중 refresh는 에셋이 바뀌어 play가 깨질 수 있어 기본 차단. --force만 통과.
    $refreshBlocked = Invoke-Cli -CliArgs @("editor", "refresh")
    $refreshForce = Invoke-Cli -CliArgs @("editor", "refresh", "--force")
    $gameShot = (Join-Path $WorkDir "game.png").Replace("\", "/")
    $game = Invoke-Cli -CliArgs @("screenshot", "--view", "game", "--output_path", $gameShot)
    Invoke-Cli -CliArgs @("profiler", "enable") | Out-Null
    # play 직후 프로파일 프레임이 비어 있을 수 있다. hierarchy 데이터가 올 때까지 다시 요청한다.
    $playHier = $null
    $hierDeadline = [datetime]::UtcNow.AddMilliseconds([Math]::Min($TimeoutMs, 30000))
    while ([datetime]::UtcNow -lt $hierDeadline) {
        $playHier = Invoke-Cli -CliArgs @("profiler", "hierarchy", "--max", "8")
        if ($playHier.ExitCode -eq 0 -and $playHier.Stdout) { break }
        Start-Sleep -Milliseconds 200
    }
    # pause는 토글이다. 한 번은 paused, 한 번은 다시 playing. 두 번째를 resume 전용 명령으로 보내지 않는다.
    $pause = Invoke-Cli -CliArgs @("editor", "pause")
    $pausedState = Wait-UnityState -Want @("paused")
    $paused = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPaused.ToString();'
    $resume = Invoke-Cli -CliArgs @("editor", "pause")
    $resumedState = Wait-UnityState -Want @("playing")
    $resumed = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPaused.ToString();'
    $stop = Invoke-Cli -CliArgs @("editor", "stop")
    $readyState = Wait-UnityState -Want @("ready")
    $after = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
    $dis = Invoke-Cli -CliArgs @("profiler", "disable")

    # play --wait는 /command를 붙잡지 않는다. play 요청 후 CLI가 heartbeat의 playing/paused를 폴링한다.
    # 위에서 이미 stop 한 뒤라, 이 경로가 "바로 리턴"이 아니라 진입을 기다리는지 따로 본다.
    $waitPlay = Invoke-Cli -CliArgs @("editor", "play", "--wait")
    $waitState = Wait-UnityState -Want @("playing", "paused")
    $waitStop = Invoke-Cli -CliArgs @("editor", "stop")
    $waitReady = Wait-UnityState -Want @("ready")

    $gameOk = $game.ExitCode -eq 0 -and $game.Stdout -match '"view"\s*:\s*"game"' -and (Test-PngComplete $gameShot)
    $refreshOk = $refreshBlocked.ExitCode -ne 0 -and (Get-CombinedText $refreshBlocked) -match "play mode" `
        -and $refreshForce.ExitCode -eq 0
    if ($pauseEdit.ExitCode -ne 0 -and $stopEdit.ExitCode -eq 0 `
        -and $play.ExitCode -eq 0 -and $playState -match 'playing|paused' -and $playing.Stdout.Trim() -eq "True" `
        -and $again.ExitCode -eq 0 -and (Get-CombinedText $again) -match "Already" `
        -and $refreshOk -and $gameOk `
        -and $playHier.ExitCode -eq 0 -and $playHier.Stdout `
        -and $pause.ExitCode -eq 0 -and $pausedState -eq "paused" -and $paused.Stdout.Trim() -eq "True" `
        -and $resume.ExitCode -eq 0 -and $resumed.Stdout.Trim() -eq "False" `
        -and $stop.ExitCode -eq 0 -and $readyState -eq "ready" -and $after.Stdout.Trim() -eq "False" `
        -and $dis.ExitCode -eq 0 `
        -and $waitPlay.ExitCode -eq 0 -and $waitState -match 'playing|paused' `
        -and $waitStop.ExitCode -eq 0 -and $waitReady -eq "ready") {
        Pass "play/refresh/game PNG/pause/stop and play --wait"
    } else {
        Fail "playState=$playState playing=$($playing.Stdout) refreshBlock=$($refreshBlocked.ExitCode) force=$($refreshForce.ExitCode) gameOk=$gameOk pause=$pausedState/$($paused.Stdout) resume=$resumedState/$($resumed.Stdout) stop=$readyState/$($after.Stdout) wait=$($waitPlay.ExitCode)/$waitState"
    }
} catch {
    # Trim() 등에서 터지면 RESULT가 pass만 보이고 성공으로 끝난다. 실패로 남긴다.
    Fail $_.Exception.Message
}
finally {
    # try가 중간에 죽어도 play/profiler/tag/layer/임시 에셋이 Editor에 남으면 다음 실행이 꼬인다.
    # $WorkDir는 프로젝트 밖 TEMP라 여기 있는 PNG/입출력만 지운다. Accelerator/Library는 건드리지 않는다.
    Invoke-CleanupUnity
    Write-Host ""
    Write-Host "RESULT  pass=$script:Pass  fail=$script:Fail" -ForegroundColor $(if ($script:Fail -eq 0) { "Green" } else { "Red" })
    if (Test-Path $WorkDir) { Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue }
}

if ($script:Fail -gt 0) { exit 1 }
exit 0
