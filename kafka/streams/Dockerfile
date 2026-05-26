FROM golang:1.24-bookworm AS builder
# gcc + libc6-dev required for CGO (confluent-kafka-go bundles and compiles librdkafka statically)
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY kafka/streams/ .
ENV GONOSUMDB=*
ENV GOFLAGS=-mod=mod
RUN go mod tidy && go mod download
# No -tags dynamic: uses bundled librdkafka (static link), same as option-b build
RUN CGO_ENABLED=1 GOOS=linux go build -o streams-processor .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/streams-processor .
CMD ["./streams-processor"]
