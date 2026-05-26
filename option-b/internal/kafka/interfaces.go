// Package kafka wraps confluent-kafka-go behind interfaces so that
// the game logic and tests have zero CGO dependency.
// The real implementation is in producer_impl.go / consumer_impl.go
// which are only compiled when CGO is available (i.e., inside Docker).
package kafka

import (
	"context"

	"github.com/rotr/option-b/internal/router"
)

// MessageProducer is the interface used by the game engine to publish events.
type MessageProducer interface {
	Produce(topic, key string, value []byte) error
	ProduceSync(topic, key string, value []byte) error
	Flush(timeoutMs int) int
	Close()
}

// MessageConsumer is the interface used to read from Kafka topics.
type MessageConsumer interface {
	Run(ctx context.Context)
	Close()
}

// KTable is an in-memory key-value store backed by a Kafka topic.
// Each Go instance rebuilds its view from assigned partitions on startup/rebalance.
type KTable[V any] struct {
	store map[string]V
}

func NewKTable[V any]() *KTable[V] {
	return &KTable[V]{store: make(map[string]V)}
}

func (kt *KTable[V]) Set(key string, value V) {
	kt.store[key] = value
}

func (kt *KTable[V]) Get(key string) (V, bool) {
	v, ok := kt.store[key]
	return v, ok
}

func (kt *KTable[V]) Delete(key string) {
	delete(kt.store, key)
}

func (kt *KTable[V]) All() map[string]V {
	out := make(map[string]V, len(kt.store))
	for k, v := range kt.store {
		out[k] = v
	}
	return out
}

// NoopProducer is used in tests and local mode (no CGO/Kafka).
// In local mode, NewServer detects this type and sets localOrderInject to write
// orders directly into kafkaValidatedOrders — no async channel chain needed.
type NoopProducer struct {
	Published []struct{ Topic, Key string; Value []byte }
}

func (n *NoopProducer) Produce(topic, key string, value []byte) error {
	n.Published = append(n.Published, struct{ Topic, Key string; Value []byte }{topic, key, value})
	return nil
}
func (n *NoopProducer) ProduceSync(topic, key string, value []byte) error {
	return n.Produce(topic, key, value)
}
func (n *NoopProducer) Flush(int) int { return 0 }
func (n *NoopProducer) Close()        {}

// NoopConsumer is used in tests.
type NoopConsumer struct{}

func (n *NoopConsumer) Run(_ context.Context) {}
func (n *NoopConsumer) Close()                {}

// ChannelConsumer feeds pre-built events into the eventCh — used in tests.
type ChannelConsumer struct {
	Events  []router.Event
	eventCh chan<- router.Event
}

func NewChannelConsumer(events []router.Event, eventCh chan<- router.Event) *ChannelConsumer {
	return &ChannelConsumer{Events: events, eventCh: eventCh}
}

func (c *ChannelConsumer) Run(ctx context.Context) {
	for _, e := range c.Events {
		select {
		case c.eventCh <- e:
		case <-ctx.Done():
			return
		}
	}
}
func (c *ChannelConsumer) Close() {}
