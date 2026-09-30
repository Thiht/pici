FROM golang:1.27-alpine

RUN apk add --no-cache git ca-certificates github-cli jq \
    && go install github.com/icholy/gomajor@v0.15.0
