FROM golang:1.26.10-alpine@sha256:87c65a688975c0b1d4585d204874c45ca78354670e3a5f11368c5a492303d696 AS builder

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
