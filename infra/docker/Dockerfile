# Stage 1: Builder
FROM golang:1.23-alpine AS builder

WORKDIR /app

# Copy and download go dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the executable with caching enabled for go build
ENV GOCACHE=/root/.cache/go-build
RUN --mount=type=cache,target="/root/.cache/go-build" CGO_ENABLED=0 GOOS=linux go build -o sentinel

# Stage 2: Final
FROM alpine:3.20 AS final

LABEL version="1.0" \
      org.opencontainers.image.authors="Denis AKPAGNONITE <akpagnonited@gmail.com>"

# Install dumb-init and database clients
RUN apk update && apk add --no-cache dumb-init mysql-client mariadb-client postgresql-client mongodb-tools

WORKDIR /app

# Copy the executable from the builder stage
COPY --from=builder /app/sentinel .

# Create a sentinel user and group, set permissions and create a backups directory
RUN addgroup -S sentinel  \
    && adduser -S sentinel -G sentinel  \
    && chmod +x /app/sentinel  \
    && mkdir -p /app/backups  \
    && chown -R sentinel:sentinel /app/backups

USER sentinel

ENTRYPOINT ["dumb-init", "--", "/app/sentinel"]
CMD ["help"]
