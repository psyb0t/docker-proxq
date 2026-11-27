FROM golang:1.26.11-alpine@sha256:ecb67f987aa1a57794c91d19ade7ae0209f2f0f5496aaef9e6ae85ddf92d02ab AS builder

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 go build -a \
    -mod=vendor \
    -ldflags '-extldflags "-static"' \
    -o ./build/proxq ./cmd/...

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk --no-cache add ca-certificates

RUN adduser -D -s /bin/sh proxq

WORKDIR /app

COPY --from=builder /app/build/proxq .

RUN chown proxq:proxq /app/proxq

USER proxq

ENTRYPOINT ["./proxq"]
