#Requires -Version 5.1
<#
.SYNOPSIS
    Installs video-compressor on Windows using WSL2 and Docker/Podman.

.DESCRIPTION
    Checks WSL2, NVIDIA GPU visibility (/dev/dxg), and the container runtime,
    then clones/updates the repository inside the WSL distribution and starts
    docker compose (or podman compose).

.PARAMETER Runtime
    Container runtime to use: docker (default) or podman.

.PARAMETER RepoUrl
    Git repository URL.

.PARAMETER SkipBuild
    Start containers without rebuilding the image.

.EXAMPLE
    .\install-video-compressor.ps1

.EXAMPLE
    .\install-video-compressor.ps1 -Runtime podman
#>
[CmdletBinding()]
param(
    [ValidateSet("docker", "podman")]
    [string]$Runtime = "docker",

    [string]$RepoUrl = "https://github.com/FuzzyHD1993/video-compressor.git",

    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

function Write-Step([string]$Message) {
    Write-Host ""
    Write-Host "==> $Message" -ForegroundColor Cyan
}

function Write-Warn([string]$Message) {
    Write-Host "WARNING: $Message" -ForegroundColor Yellow
}

function Fail([string]$Message) {
    Write-Host "ERROR: $Message" -ForegroundColor Red
    exit 1
}

Write-Host "video-compressor installer (WSL2 + GPU)" -ForegroundColor Green

Write-Step "Checking WSL2"
wsl --status | Out-Host
if ($LASTEXITCODE -ne 0) {
    Fail "WSL2 is not available. Run 'wsl --install -d Ubuntu-24.04' as administrator."
}

Write-Step "Checking NVIDIA GPU inside WSL2"
wsl -e bash -lc 'if [ -e /dev/dxg ]; then echo "/dev/dxg: OK"; else echo "/dev/dxg: MISSING"; exit 1; fi; if command -v nvidia-smi >/dev/null 2>&1; then nvidia-smi --query-gpu=name,driver_version --format=csv,noheader; else echo "nvidia-smi: MISSING (update the Windows NVIDIA driver)"; fi'
if ($LASTEXITCODE -ne 0) {
    Fail "GPU is not passed through to WSL2. Update the Windows NVIDIA driver and run 'wsl --update'."
}

Write-Step "Checking $Runtime inside WSL2"
wsl -e bash -lc "command -v $Runtime >/dev/null 2>&1"
if ($LASTEXITCODE -ne 0) {
    if ($Runtime -eq "docker") {
        Fail "docker not found in WSL. Install Docker Desktop and enable WSL integration."
    }
    Fail "podman not found in WSL. Install Podman Desktop / podman in the distribution."
}

$buildFlag = if ($SkipBuild) { "" } else { "--build" }

if ($Runtime -eq "podman") {
    Write-Step "Checking CDI specification (Podman)"
    wsl -e bash -lc 'if [ -f /etc/cdi/nvidia.yaml ]; then echo "CDI: /etc/cdi/nvidia.yaml"; else echo "CDI: MISSING"; exit 1; fi'
    if ($LASTEXITCODE -ne 0) {
        Write-Warn "CDI spec not found in the default WSL distribution."
        Write-Warn "Generate it inside the Podman machine:"
        Write-Warn "  podman machine ssh"
        Write-Warn "  sudo dnf install -y nvidia-container-toolkit"
        Write-Warn "  sudo nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml"
        Write-Warn "Then run this script with -Runtime podman again."
        Fail "CDI is required for GPU access in Podman."
    }
}

Write-Step "Cloning or updating the repository"
wsl -e bash -lc "if [ -d `$HOME/video-compressor/.git ]; then cd `$HOME/video-compressor && git pull --ff-only; else git clone '$RepoUrl' `$HOME/video-compressor; fi"
if ($LASTEXITCODE -ne 0) {
    Fail "Failed to clone/update the repository inside WSL."
}

Write-Step "Starting containers"
wsl -e bash -lc "cd `$HOME/video-compressor && $Runtime compose up -d $buildFlag"
if ($LASTEXITCODE -ne 0) {
    Fail "Failed to start containers."
}

Write-Step "Verifying NVENC availability"
wsl -e bash -lc "docker exec nas-video-compressor ffmpeg -hide_banner -encoders 2>/dev/null | grep -E 'nvenc' || true"

Write-Host ""
Write-Host "Done. Open http://localhost:8095" -ForegroundColor Green
Write-Host "Files are stored in ~/video-compressor/output inside WSL." -ForegroundColor Green
