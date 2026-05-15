$ErrorActionPreference = 'Stop'

function Assert-LastExitCode {
  param([string]$CommandName)
  if ($LASTEXITCODE -ne 0) {
    throw "$CommandName failed with exit code $LASTEXITCODE"
  }
}

$exitCode = 0
$pushed = $false
try {
  go test ./...
  Assert-LastExitCode 'go test'

  if (Test-Path web) {
    try {
      Push-Location web
      $pushed = $true

      pnpm lint
      Assert-LastExitCode 'pnpm lint'

      pnpm build
      Assert-LastExitCode 'pnpm build'
    } finally {
      if ($pushed) {
        Pop-Location
      }
    }
  }
} catch {
  if ($LASTEXITCODE -ne 0) {
    $exitCode = $LASTEXITCODE
  } else {
    $exitCode = 1
  }
  Write-Error $_ -ErrorAction Continue
} finally {
  exit $exitCode
}
