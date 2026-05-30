//go:build cgo

package main

import (
	"log"

	kafkaclient "github.com/lotr/option-b/internal/kafka"
	"github.com/lotr/option-b/internal/router"
	"github.com/lotr/option-b/internal/state"
)

func buildProducer(brokers string) kafkaclient.MessageProducer {
	p, err := kafkaclient.NewIdempotentProducer(brokers)
	if err != nil {
		log.Fatalf("[main] producer: %v", err)
	}
	return p
}

func buildConsumer(brokers, nodeID string, eventCh chan<- router.Event) kafkaclient.MessageConsumer {
	topics := []string{
		"game.orders.validated",
		"game.events.unit",
		"game.events.region",
		"game.events.path",
		"game.broadcast",
		"game.ring.position",
		"game.ring.detection",
		"game.session",
	}
	c, err := kafkaclient.NewConsumer(brokers, "game-engine-"+nodeID, topics, eventCh)
	if err != nil {
		log.Fatalf("[main] consumer: %v", err)
	}
	return c
}

// recoverState replays game.session to restore state after a crash/restart.
// If no snapshot exists the cache is left as initialised from config (fresh game).
func recoverState(brokers, nodeID string, cache *state.WorldStateCache) {
	kafkaclient.RecoverStateFromKafka(brokers, nodeID, cache)
}
