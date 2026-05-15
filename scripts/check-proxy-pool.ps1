go test ./...
if (Test-Path web) {
  Push-Location web
  pnpm lint
  pnpm build
  Pop-Location
}
