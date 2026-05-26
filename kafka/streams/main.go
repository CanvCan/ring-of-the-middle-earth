package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	brokers := envOr("KAFKA_BROKERS", "localhost:9092")

	log.Printf("[streams] starting — brokers=%s", brokers)

	runner, err := NewRunner(brokers)
	if err != nil {
		log.Fatalf("[streams] init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		log.Println("[streams] shutting down")
		cancel()
	}()

	if err := runner.Run(ctx); err != nil {
		log.Fatalf("[streams] run: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
