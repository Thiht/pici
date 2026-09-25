FROM golang:1.27-alpine

RUN apk add --no-cache git ca-certificates \
    && go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
