FROM golang:1.26.1-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/echopoint-runner ./cmd/runner

FROM alpine:3.22

RUN adduser -D -u 10001 runner

WORKDIR /app

COPY --from=builder /out/echopoint-runner /usr/local/bin/echopoint-runner

USER runner

ENTRYPOINT ["/usr/local/bin/echopoint-runner"]
