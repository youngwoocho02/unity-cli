# Unity가 켜진 상태에서 현재 소스 CLI로 실제 요청을 보낸다.
# 플래그 파싱 목 테스트가 아니라, 한 번 보내기 / 겹치는 요청 / 컴파일 대기를 확인한다.
#
#   pwsh -File scripts/live-test.ps1
#   pwsh -File scripts/live-test.ps1 -Project C:/WorkSpace/project-maid

param(
    [string]$Project = "",
    [int]$TimeoutMs = 180000
)

$ErrorActionPreference = "Continue"
$RepoRoot = Split-Path -Parent $PSScriptRoot
$WorkDir = Join-Path $env:TEMP ("unity-cli-live-test-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$Cli = Join-Path $WorkDir "unity-cli-live.exe"

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
        ExitCode = $p.ExitCode
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
        ExitCode = $p.ExitCode
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
    $inFile = Join-Path $WorkDir ("in-" + [guid]::NewGuid().ToString("N") + ".cs")
    $outFile = Join-Path $WorkDir ("out-" + [guid]::NewGuid().ToString("N") + ".txt")
    $errFile = Join-Path $WorkDir ("err-" + [guid]::NewGuid().ToString("N") + ".txt")
    Set-Content -Path $inFile -Value $Code -NoNewline -Encoding utf8
    $all = @()
    if ($Project) { $all += @("--project", $Project) }
    $all += @("--timeout", "$Timeout")
    $all += $CliArgs
    $p = Start-Process -FilePath $Cli -ArgumentList $all -NoNewWindow -PassThru `
        -RedirectStandardInput $inFile `
        -RedirectStandardOutput $outFile -RedirectStandardError $errFile
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
    $exit = $job.Process.ExitCode
    if ($null -eq $exit) { $exit = 0 }
    $out = Get-Content -Raw $job.OutFile -ErrorAction SilentlyContinue
    if ($null -eq $out) { $out = "" }
    $err = Get-Content -Raw $job.ErrFile -ErrorAction SilentlyContinue
    if ($null -eq $err) { $err = "" }
    [pscustomobject]@{
        ExitCode = [int]$exit
        Stdout   = $out.Trim()
        Stderr   = $err.Trim()
        Ms       = [int]((Get-Date) - $job.Started).TotalMilliseconds
    }
}

try {
    Write-Host "Building current CLI into $WorkDir"
    Push-Location $RepoRoot
    go build -ldflags "-X main.Version=0.3.22" -o $Cli .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    Pop-Location

    Write-Case "status (Unity must be running)"
    $r = Invoke-Cli -CliArgs @("status")
    Write-Host $r.Stdout
    if ($r.ExitCode -eq 0 -and $r.Stdout -match "Unity:") { Pass $r.Stdout.Trim() }
    else { Fail "status failed: $($r.Stderr)" ; throw "Unity is not reachable. Open the Editor with the Connector and rerun." }

    Write-Case "exec once"
    $r = Invoke-CliStdin -CliArgs @("exec") -Code 'return (1+1).ToString();'
    if ($r.ExitCode -eq 0 -and $r.Stdout.Trim() -eq "2") { Pass "2" }
    else { Fail "exec 1+1 => exit $($r.ExitCode) out='$($r.Stdout)' err='$($r.Stderr)'" }

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
var until = System.DateTime.UtcNow.AddMilliseconds(400);
while (System.DateTime.UtcNow < until) {}
return "slow";
"@
    $slow = Start-CliJob -CliArgs @("exec") -Code $slowCode
    $fast = Start-CliJob -CliArgs @("exec") -Code 'return "fast";'
    $slowR = Wait-CliJob $slow
    $fastR = Wait-CliJob $fast
    if ($slowR.ExitCode -eq 0 -and $slowR.Stdout.Trim() -eq "slow" -and $fastR.ExitCode -eq 0 -and $fastR.Stdout.Trim() -eq "fast") {
        Pass "slow=$($slowR.Ms)ms fast=$($fastR.Ms)ms (both got one response)"
    } else {
        Fail "slow='$($slowR.Stdout)'/$($slowR.ExitCode) fast='$($fastR.Stdout)'/$($fastR.ExitCode)"
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

    Write-Case "play --wait then stop"
    $play = Invoke-Cli -CliArgs @("editor", "play", "--wait")
    if ($play.ExitCode -ne 0) {
        Fail "play --wait: $($play.Stderr)"
    } else {
        $stop = Invoke-Cli -CliArgs @("editor", "stop")
        $after = Invoke-CliStdin -CliArgs @("exec") -Code 'return EditorApplication.isPlaying.ToString();'
        if ($stop.ExitCode -eq 0 -and $after.Stdout.Trim() -eq "False") { Pass "entered play and stopped" }
        else { Fail "stop/play leftover playing=$($after.Stdout) stop=$($stop.ExitCode)" }
    }
}
finally {
    Write-Host ""
    Write-Host "RESULT  pass=$script:Pass  fail=$script:Fail" -ForegroundColor $(if ($script:Fail -eq 0) { "Green" } else { "Red" })
    if (Test-Path $WorkDir) { Remove-Item -Recurse -Force $WorkDir -ErrorAction SilentlyContinue }
}

if ($script:Fail -gt 0) { exit 1 }
exit 0
