# =========================
# Стадия сборки
# =========================
FROM golang:1.25-alpine AS build

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GO111MODULE=on

# Непривилегированный пользователь для сборки
RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

WORKDIR /app

# Сначала зависимости для кэша
COPY go.mod go.sum ./
RUN go mod verify && go mod download

# Затем исходники
COPY . .

# Сборка бинарника
RUN go build -ldflags="-s -w" -o /app/pr-manager-service ./cmd/app

# =========================
# Финальный образ
# =========================
FROM alpine:3.21

RUN apk update --no-cache && apk add --no-cache ca-certificates tzdata

# Непривилегированный пользователь для рантайма
RUN addgroup -g 1000 -S appgroup && \
    adduser -u 1000 -S appuser -G appgroup

WORKDIR /app

# Бинарь
COPY --from=build --chown=appuser:appgroup /app/pr-manager-service /usr/local/bin/pr-manager-service

# Порт HTTP-сервера
EXPOSE 8080

USER appuser

ENTRYPOINT ["/usr/local/bin/pr-manager-service"]
