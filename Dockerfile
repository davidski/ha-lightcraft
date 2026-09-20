FROM golang:1.27-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/ha-lightcraft .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates openssh-client su-exec \
    && mkdir -p /data/data /data/backups \
    && chown -R 1000:1000 /data

COPY --from=build /out/ha-lightcraft /usr/local/bin/ha-lightcraft
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh

WORKDIR /data
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["web", "--listen", "0.0.0.0", "--port", "8080", "--data", "/data/data", "--backup-dir", "/data/backups", "--open=false"]
