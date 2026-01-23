FROM golang:1.24-alpine AS builder
LABEL maintainer="Telegram: t.me/abudaev"

RUN apk add --no-cache \
    git \
    clang \
    llvm \
    make \
    elfutils-dev \
    linux-headers \
    libbpf-dev

WORKDIR /app
COPY . .
RUN make build
RUN ls -la /app/bin
ENTRYPOINT ["/app/bin/main"]

FROM alpine:3.20

RUN apk add --no-cache libelf bash
COPY --from=builder /app/bin /usr/local/bin/
RUN ls -la /usr/local/bin
ENTRYPOINT ["/usr/local/bin/main"]
