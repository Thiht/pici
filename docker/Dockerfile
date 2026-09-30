FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG LDFLAGS=""
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w ${LDFLAGS}" \
    -o /out/pici ./cmd/pici

FROM docker:28.5.2-dind

ARG VERSION=""
ARG REVISION=""

LABEL org.opencontainers.image.source=https://github.com/Thiht/pici
LABEL org.opencontainers.image.version=${VERSION}
LABEL org.opencontainers.image.revision=${REVISION}

ENV DOCKER_TLS_CERTDIR=""

RUN mkdir -p /data
WORKDIR /data

COPY --from=build /out/pici /usr/local/bin/pici
COPY docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENTRYPOINT ["docker-entrypoint.sh"]
