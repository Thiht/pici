FROM golang:1.27-alpine

RUN apk add --no-cache git ca-certificates docker-cli nodejs npm python3 py3-pip shellcheck \
    && go install github.com/go-task/task/v3/cmd/task@v3.53.1
