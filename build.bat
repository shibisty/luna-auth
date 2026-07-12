@echo off
setlocal

echo [1/2] Resolving Go modules...
go mod tidy -v
if errorlevel 1 exit /b %errorlevel%

echo [2/2] Building binary...
go build -o auth.exe
if errorlevel 1 exit /b %errorlevel%
