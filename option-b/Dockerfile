FROM golang:1.22-bookworm AS builder
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY option-b/ .
ENV GONOSUMDB=*
ENV GOFLAGS=-mod=mod
RUN go mod tidy && go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o server .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/server .
COPY config/ ./config/
EXPOSE 8080 6060
CMD ["./server"]
