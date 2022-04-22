FROM golang:latest as builder

WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . ./
RUN go build -mod=readonly -v -o curltrace

FROM debian:buster-slim
RUN set -x && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y \
    ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/curltrace /usr/bin/curltrace

ENTRYPOINT ["/usr/bin/curltrace"]
