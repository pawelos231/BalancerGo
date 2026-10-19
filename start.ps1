$ErrorActionPreference = 'Stop'

docker compose --project-directory $PSScriptRoot up --build -d
exit $LASTEXITCODE
