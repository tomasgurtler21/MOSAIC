Get-ChildItem -Recurse -Filter go.mod | ForEach-Object {
    Push-Location $_.DirectoryName
    Write-Host "`n=== $($_.DirectoryName) ===" -ForegroundColor Cyan
    gotestsum ./...
    Pop-Location
}
Write-Host "`nAll tests complete. Press any key to exit..."
$null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")