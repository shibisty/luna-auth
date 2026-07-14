# Stage 1: build
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Устанавливаем git и ca-certificates для go mod tidy
RUN apk add --no-cache git ca-certificates

# Копируем файлы зависимостей и исходники
COPY go.mod go.sum* ./

# Копируем исходники до разрешения модулей
COPY . .

# Скачиваем зависимости и создаём go.sum
RUN go mod tidy && go mod download

# Собираем бинарник
RUN go build -o auth .

# Stage 2: финальный образ
FROM alpine:latest

WORKDIR /root/

# Для HTTPS
RUN apk add --no-cache ca-certificates

# Копируем бинарник из builder
COPY --from=builder /app/auth .

EXPOSE 8080
CMD ["./auth"]
