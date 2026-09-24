<#
.SYNOPSIS
    Verifies causal file-write attribution end to end, on an elevated host.

.DESCRIPTION
    Runs the detector with attribution.mode = "audit" against a known writer and
    scores how often it blames the right process. Each round:

      1. builds grima (unless -Binary names one),
      2. starts the detector on a fresh directory with GRIMA_ATTRIB_TRACE set,
      3. runs testdata/scenarios/attribution.py, which prints its own pid,
      4. scores the trace with testdata/scenarios/score_attribution.py.

    The mechanism needs an elevated process: it enables the File System audit
    subcategory and puts an audit ACE on the monitored directory. Without
    elevation the detector falls back to correlation, which scores 0% on this
    workload by design. -AllowUnelevated runs the same steps anyway, to check
    the harness itself.

    Nothing here is destructive outside -WorkDir, and the audit ACE and audit
    policy are left in place afterwards.

.EXAMPLE
    # From an elevated PowerShell, at the repository root:
    .\testdata\scenarios\verify_attribution.ps1

.EXAMPLE
    # Check the harness without elevation (expects 0%):
    .\testdata\scenarios\verify_attribution.ps1 -AllowUnelevated -Rounds 1
#>
[CmdletBinding()]
param(
    [int]$Rounds = 5,
    [int]$Files = 65,
    [int]$Size = 4096,
    [int]$DurationSeconds = 25,
    [string]$WorkDir = "$env:TEMP\grima-attrib-verify",
    [string]$Binary = "",

    # How long a file event waits for its audited record before falling back to
    # correlation. This is the knob the score turns on: every causal decision is
    # correct and every fallback is wrong, so a longer wait captures more events
    # and costs detection latency.
    [string]$MaxDelay = "300ms",

    # Sweep several delays in one elevated run and report the curve, instead of
    # measuring a single point. One UAC prompt rather than one per value.
    # Comma-separated, because a [string[]] parameter does not bind through
    # -File: "100ms,300ms" arrives as a single element and fails to parse.
    [switch]$Sweep,
    [string]$SweepDelays = "100ms,300ms,500ms,1s",

    [switch]$AllowUnelevated
)

$ErrorActionPreference = "Stop"

function Test-Elevated {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Write-Head([string]$Text) {
    Write-Host ""
    Write-Host "== $Text" -ForegroundColor Cyan
}

# The detector holds its logs open while it runs, so they are read with sharing
# rather than with Select-String, which opens them for read alone. Its records go
# to stderr, so both streams are read.
function Read-Log([string[]]$Paths) {
    $text = ""
    foreach ($path in $Paths) {
        if (-not (Test-Path $path)) { continue }
        try { $text += (Get-Content -Path $path -Raw -ErrorAction Stop) } catch { }
    }
    return $text
}

function Wait-ForLine([string[]]$Paths, [string]$Pattern, [int]$TimeoutSeconds) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if ((Read-Log $Paths) -match $Pattern) { return $true }
        Start-Sleep -Milliseconds 250
    }
    return $false
}

function Show-Log([string[]]$Paths, [int]$Lines) {
    (Read-Log $Paths) -split "`r?`n" | Where-Object { $_ } | Select-Object -Last $Lines |
        ForEach-Object { Write-Host "  $_" }
}

function Invoke-Python([string]$Script, [string[]]$Arguments) {
    $output = & python $Script @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "python $Script failed with exit code $LASTEXITCODE`n$output"
    }
    return $output
}

# ---------------------------------------------------------------- preconditions

if (-not (Test-Elevated)) {
    if (-not $AllowUnelevated) {
        Write-Host "error: this must run in an elevated PowerShell: the audit mechanism needs SeSecurityPrivilege." -ForegroundColor Red
        Write-Host "       Right-click Windows Terminal > Run as administrator, then re-run this script."
        exit 1
    }
    Write-Host "warning: not elevated. The detector will fall back to correlation and the score will be 0%." -ForegroundColor Yellow
}

$root = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$writer = Join-Path $PSScriptRoot "attribution.py"
$scorer = Join-Path $PSScriptRoot "score_attribution.py"
foreach ($required in @($writer, $scorer)) {
    if (-not (Test-Path $required)) { Write-Host "error: missing $required" -ForegroundColor Red; exit 1 }
}

New-Item -ItemType Directory -Force -Path $WorkDir | Out-Null
$dataDir = Join-Path $WorkDir "data"

if (-not $Binary) {
    $Binary = Join-Path $WorkDir "grima.exe"
    Write-Head "building the detector"
    Push-Location $root
    try {
        & go build -o $Binary ./cmd/grima
        if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
        Pop-Location
    }
}
Write-Host "detector: $Binary"
Write-Host "work dir: $WorkDir"

# ---------------------------------------------------------------------- rounds

$results = @()
$delays = if ($Sweep) {
    @($SweepDelays -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
} else {
    @($MaxDelay)
}
if ($Sweep) {
    Write-Host ("sweeping {0} delays x {1} rounds = {2} detector runs" -f `
        $delays.Count, $Rounds, ($delays.Count * $Rounds))
}

foreach ($delay in $delays) {
if ($Sweep) { Write-Head "max_delay $delay" }

for ($round = 1; $round -le $Rounds; $round++) {
    Write-Head "round $round of $Rounds at max_delay $delay"

    Remove-Item -Recurse -Force $dataDir -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $dataDir | Out-Null

    $tag = if ($Sweep) { "$delay-$round" } else { "$round" }
    $trace = Join-Path $WorkDir "trace-$tag.jsonl"
    $config = Join-Path $WorkDir "grima-$tag.toml"
    $log = Join-Path $WorkDir "detector-$tag.log"
    $errLog = Join-Path $WorkDir "detector-$tag.err.log"
    Remove-Item -Force $trace, $config, $log, $errLog -ErrorAction SilentlyContinue

    $escapedData = $dataDir.Replace('\', '\\')
    $escapedBaseline = (Join-Path $WorkDir "baseline.json").Replace('\', '\\')
    @"
[general]
monitor_paths = ["$escapedData"]
log_level = "info"

[attribution]
mode = "audit"
max_delay = "$delay"
audit_setup = true

[decoy]
enabled = false

[web]
enabled = false

[calibration]
baseline_path = "$escapedBaseline"
"@ | Set-Content -Path $config -Encoding ASCII

    $env:GRIMA_ATTRIB_TRACE = $trace
    $detector = Start-Process -FilePath $Binary `
        -ArgumentList @("--config", $config, "--duration", "${DurationSeconds}s") `
        -RedirectStandardOutput $log -RedirectStandardError $errLog `
        -PassThru -WindowStyle Hidden

    $detectorLogs = @($log, $errLog)
    if (-not (Wait-ForLine $detectorLogs "watching directories" 20)) {
        Write-Host "the detector never reported watching the target directory:" -ForegroundColor Red
        Show-Log $detectorLogs 10
        if (-not $detector.HasExited) { $detector.Kill() }
        exit 1
    }

    # Let the ACE and the subscription settle before the writer starts.
    Start-Sleep -Seconds 3

    $writerOutput = Invoke-Python $writer @("--role", "writer", "--path", $dataDir,
        "--files", "$Files", "--size", "$Size", "--interval", "0", "--suffix=")
    $writerLine = $writerOutput | Select-Object -First 1
    if ($writerLine -notmatch 'pid=(\d+)') {
        throw "could not read the writer's pid from: $writerLine"
    }
    $writerPid = [int]$Matches[1]
    Write-Host "writer pid $writerPid"

    $detector.WaitForExit(($DurationSeconds + 30) * 1000) | Out-Null
    if (-not $detector.HasExited) { $detector.Kill() }

    $detectorLog = Read-Log $detectorLogs
    $started = $detectorLog -match "causal attribution active"
    if (-not $started) {
        $reason = ($detectorLog -split "`r?`n" | Where-Object { $_ -match "causal attribution unavailable" } | Select-Object -First 1)
        Write-Host "the causal mechanism did not start: $reason" -ForegroundColor Yellow
    }

    $summary = (Invoke-Python $scorer @("--trace", $trace, "--writer-pid", "$writerPid", "--json") | Out-String) | ConvertFrom-Json
    $decisions = @(Get-Content $trace | ForEach-Object { $_ | ConvertFrom-Json })
    $causal = @($decisions | Where-Object { $_.source -eq "causal" }).Count

    $results += [pscustomobject]@{
        Delay      = $delay
        Round      = $round
        WriterPid  = $writerPid
        Started    = [bool]$started
        Decisions  = $summary.total
        Correct    = $summary.correct
        Accuracy   = $summary.accuracy
        Causal     = $causal
        Correlate  = $summary.total - $causal
        Trace      = $trace
    }

    Write-Host ("decisions {0}, correct {1}, accuracy {2:P1}, causal {3}, correlate {4}" -f `
        $summary.total, $summary.correct, $summary.accuracy, $causal, ($summary.total - $causal))
}
}

# ------------------------------------------------------------------- aggregate

Write-Head "aggregate"
$totalDecisions = ($results | Measure-Object -Property Decisions -Sum).Sum
$totalCorrect = ($results | Measure-Object -Property Correct -Sum).Sum
$totalCausal = ($results | Measure-Object -Property Causal -Sum).Sum
$accuracy = if ($totalDecisions -gt 0) { $totalCorrect / $totalDecisions } else { 0 }

$results | Format-Table -AutoSize | Out-String | Write-Host
Write-Host ("decisions {0}, correct {1}, causal {2} ({3:P1} of decisions)" -f `
    $totalDecisions, $totalCorrect, $totalCausal, $(if ($totalDecisions -gt 0) { $totalCausal / $totalDecisions } else { 0 }))
Write-Host ("accuracy {0:P1}" -f $accuracy)

if ($Sweep) {
    Write-Head "curve"
    $curve = foreach ($delay in $delays) {
        $d = @($results | Where-Object { $_.Delay -eq $delay })
        $dec = ($d | Measure-Object -Property Decisions -Sum).Sum
        $cor = ($d | Measure-Object -Property Correct -Sum).Sum
        $cau = ($d | Measure-Object -Property Causal -Sum).Sum
        [pscustomobject]@{
            MaxDelay  = $delay
            Rounds    = $d.Count
            Decisions = $dec
            Correct   = $cor
            Causal    = $cau
            Correlate = $dec - $cau
            Accuracy  = if ($dec -gt 0) { "{0:P1}" -f ($cor / $dec) } else { "-" }
        }
    }
    $curve | Format-Table -AutoSize | Out-String | Write-Host
    Write-Host "Pick the smallest delay whose accuracy has stopped rising. A longer wait buys"
    Write-Host "nothing once the late records are captured, and it costs detection latency:"
    Write-Host "a file event cannot be scored until it has been attributed."
}

if (-not ($results | Where-Object { $_.Started })) {
    Write-Host ""
    Write-Host "the audit mechanism never started, so this run does not measure it." -ForegroundColor Yellow
    Write-Host "Read the reason above, fix it, and run again. Expect: mode=audit with no"
    Write-Host "'causal attribution unavailable' line in the detector log."
    exit 2
}

Write-Host ""
if ($accuracy -ge 0.9) {
    Write-Host ("GATE MET: {0:P1} of {1} decisions named the right process." -f $accuracy, $totalDecisions) -ForegroundColor Green
} else {
    Write-Host ("GATE NOT MET: {0:P1} of {1} decisions named the right process." -f $accuracy, $totalDecisions) -ForegroundColor Red
}
Write-Host "Traces and logs are in $WorkDir."
if ($Sweep) {
    Write-Host "Record the curve and the chosen default in docs/sprints.md section 22."
} else {
    Write-Host "Record the number in docs/sprints.md section 11."
}
exit 0
