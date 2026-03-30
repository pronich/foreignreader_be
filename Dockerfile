FROM golang:1.25-alpine AS builder

# CA bundle for TLS (needed when copying into scratch; scratch has no system roots)
RUN apk add --no-cache ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
COPY main.go ./
COPY internal ./internal
COPY prompts ./prompts

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o /out/app .


FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/app /app
COPY --from=builder /src/prompts /prompts

EXPOSE 8080

ENTRYPOINT ["/app"]

