FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
COPY main.go ./
COPY internal ./internal
COPY prompts ./prompts

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o /out/app .


FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget \
	&& addgroup -g 65532 -S app \
	&& adduser -u 65532 -S -G app -H -s /sbin/nologin app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/app /app
COPY --from=builder /src/prompts /prompts

RUN chown -R app:app /app /prompts

USER app

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
	CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/app"]
