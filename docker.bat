@echo off
docker run -p 8080:8080 --env-file .env auth:latest
