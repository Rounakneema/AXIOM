javac MascotOverlay.java
if ($LASTEXITCODE -eq 0) {
    Write-Host "✅ Mascot compiled successfully!" -ForegroundColor Green
} else {
    Write-Host "❌ Compilation failed." -ForegroundColor Red
}
