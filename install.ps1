# Installs portage on Windows:
#   irm https://go.wrench.rocks/portage/install.ps1 | iex
#
# Optional settings (set before running):
#   $env:PORTAGE_VERSION = "v0.2.0"     install a specific release instead of the latest
#   $env:PORTAGE_INSTALL_DIR = "<dir>"  install somewhere other than %LOCALAPPDATA%\Programs\portage
#   $env:PORTAGE_NO_APPS = "1"          don't add portage to your AI apps
#   $env:PORTAGE_NO_MODIFY_PATH = "1"   don't add the install folder to your PATH

function Install-Portage {
    $ErrorActionPreference = "Stop"
    $ProgressPreference = "SilentlyContinue"
    [Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

    $repo = "EverythingSuckz/portage"
    $dir = $env:PORTAGE_INSTALL_DIR
    if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA "Programs\portage" }

    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { "amd64" }
        "ARM64" { "arm64" }
        default { throw "portage: unsupported processor: $env:PROCESSOR_ARCHITECTURE" }
    }
    if ($env:PROCESSOR_ARCHITEW6432 -eq "ARM64") { $arch = "arm64" }

    $version = $env:PORTAGE_VERSION
    if (-not $version) {
        $version = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
    }
    if ($version -notmatch '^v') { throw "portage: couldn't work out the latest release (got '$version')" }

    $file = "portage_$($version.TrimStart('v'))_windows_$arch.zip"
    $base = "https://github.com/$repo/releases/download/$version"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("portage-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null

    try {
        Write-Host "Downloading portage $version for windows/$arch..."
        Invoke-WebRequest -UseBasicParsing "$base/$file" -OutFile (Join-Path $tmp $file)
        Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt")

        $line = Get-Content (Join-Path $tmp "checksums.txt") | Where-Object { $_ -match " $([regex]::Escape($file))$" }
        if (-not $line) { throw "portage: $file is not listed in checksums.txt" }
        $want = ($line -split '\s+')[0]
        $got = (Get-FileHash (Join-Path $tmp $file) -Algorithm SHA256).Hash.ToLower()
        if ($got -ne $want.ToLower()) { throw "portage: checksum mismatch for $file; not installing" }

        Expand-Archive (Join-Path $tmp $file) -DestinationPath (Join-Path $tmp "x") -Force
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        $exe = Join-Path $dir "portage.exe"
        if (Test-Path $exe) {
            # A running program can't be overwritten, but it can be renamed.
            $old = "$exe.old"
            Remove-Item $old -Force -ErrorAction SilentlyContinue
            Move-Item $exe $old -Force
        }
        Copy-Item (Join-Path $tmp "x\portage.exe") $exe
        Remove-Item "$exe.old" -Force -ErrorAction SilentlyContinue
        Write-Host "Installed $exe"
    }
    finally {
        Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }

    if ($env:PORTAGE_NO_MODIFY_PATH -ne "1") {
        $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if (($userPath -split ';') -notcontains $dir) {
            $newPath = if ($userPath) { "$userPath;$dir" } else { $dir }
            [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
            $env:Path = "$env:Path;$dir"
            Write-Host "Added $dir to your PATH. Open a new terminal to use the portage command."
        }
    }

    if ($env:PORTAGE_NO_APPS -ne "1") {
        Write-Host ""
        Write-Host "Adding portage to your AI apps..."
        & $exe install
    }
}

Install-Portage
