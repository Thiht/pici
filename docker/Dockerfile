FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/pici ./cmd/pici

FROM docker:dind

ENV DOCKER_TLS_CERTDIR=""

RUN mkdir -p /data
WORKDIR /data

COPY --from=build /out/pici /usr/local/bin/pici
COPY docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["docker-entrypoint.sh"]
