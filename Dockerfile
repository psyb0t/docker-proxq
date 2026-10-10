FROM golang:1.26.9-alpine@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder

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
