//go:build !cgo

package main

import (
	"log"

	kafkaclient "github.com/rotr/option-b/internal/kafka"
	"github.com/rotr/option-b/internal/router"
	"github.com/rotr/option-b/internal/state"
)

// buildProducer returns a NoopProducer.
// NewServer detects this type and wires a direct synchronous localOrderInject
// so orders are written straight into kafkaValidatedOrders without any async hops.
func buildProducer(_ string) kafkaclient.MessageProducer {
	log.Println("[main] CGO not available — using NoopProducer with direct order inject")
	return &kafkaclient.NoopProducer{}
}

func buildConsumer(_ string, _ string, _ chan<- router.Event) kafkaclient.MessageConsumer {
	log.Println("[main] CGO not available — using NoopConsumer (Kafka disabled)")
	return &kafkaclient.NoopConsumer{}
}

// recoverState is a no-op when CGO is not available (no real Kafka).
func recoverState(_ string, _ string, _ *state.WorldStateCache) {
	log.Println("[main] CGO not available — skipping state recovery")
}
