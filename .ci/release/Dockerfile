FROM golang:1.27-alpine

RUN apk add --no-cache git ca-certificates github-cli docker-cli \
    && go install github.com/go-task/task/v3/cmd/task@v3.53.1
