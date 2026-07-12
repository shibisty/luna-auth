@echo off
setlocal EnableDelayedExpansion

REM
set ENV_FILE=.env

if not exist %ENV_FILE% (
  echo .env not found: %ENV_FILE%
  exit /b 1
)

for /f "usebackq tokens=1,* delims==" %%A in ("%ENV_FILE%") do (
  set %%A=%%B
)

echo ENV loaded
auth.exe
