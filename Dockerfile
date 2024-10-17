FROM golang:1.23-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

ENV GOCACHE=/root/.cache/go-build

RUN --mount=type=cache,target="/root/.cache/go-build" CGO_ENABLED=0 GOOS=linux go build -o sentinel

#FROM alpine:3.20
#
#RUN apk update && apk add --no-cache dumb-init mysql-client postgresql-client
#
#WORKDIR /app
#
#COPY --from=builder /app/sentinel .
#
#RUN addgroup -S sentinel  \
#    && adduser -S sentinel -G sentinel  \
#    && chmod +x /app/sentinel  \
#    && mkdir -p /app/backups  \
#    && chown -R sentinel:sentinel /app/backups
#
#USER sentinel
#
#ENTRYPOINT ["dumb-init", "--", "/app/sentinel"]
#
#CMD ["help"]

FROM ubuntu:22.04

RUN apt-get update && apt-get install -y iputils-ping mysql-client postgresql-client && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/sentinel .

RUN addgroup --system sentinel  \
    && adduser --system --ingroup sentinel sentinel  \
    && chmod +x /app/sentinel \
    && mkdir -p /app/backups \
    && chown -R sentinel:sentinel /app/backups

USER sentinel

CMD ["/bin/bash"]
