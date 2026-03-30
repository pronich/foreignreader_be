FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod ./
COPY main.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o /out/app .


FROM scratch

COPY --from=builder /out/app /app

EXPOSE 8080

ENTRYPOINT ["/app"]

