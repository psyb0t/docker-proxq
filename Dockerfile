FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 go build -a \
    -mod=vendor \
    -ldflags '-extldflags "-static"' \
    -o ./build/proxq ./cmd/...

FROM alpine:latest

RUN apk --no-cache add ca-certificates

RUN adduser -D -s /bin/sh proxq

WORKDIR /app

COPY --from=builder /app/build/proxq .

RUN chown proxq:proxq /app/proxq

USER proxq

ENTRYPOINT ["./proxq"]
