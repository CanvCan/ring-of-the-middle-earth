//go:build cgo

package kafka

import (
	"context"
	"fmt"
	"log"

	confluentkafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/lotr/option-b/internal/router"
)

// Consumer wraps a confluent-kafka-go consumer.
// Only compiled when CGO is available (Linux/Docker).
type Consumer struct {
	c       *confluentkafka.Consumer
	eventCh chan<- router.Event
}

func NewConsumer(brokers, groupID string, topics []string, eventCh chan<- router.Event) (*Consumer, error) {
	c, err := confluentkafka.NewConsumer(&confluentkafka.ConfigMap{
		"bootstrap.servers":       brokers,
		"group.id":                groupID,
		"auto.offset.reset":       "earliest",
		"enable.auto.commit":      true,
		"auto.commit.interval.ms": 1000,
		"session.timeout.ms":      10000,
		"heartbeat.interval.ms":   3000,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer: %w", err)
	}
	if err := c.SubscribeTopics(topics, nil); err != nil {
		return nil, fmt.Errorf("subscribe: %w", err)
	}
	return &Consumer{c: c, eventCh: eventCh}, nil
}

func (cn *Consumer) Run(ctx context.Context) {
	defer cn.c.Close()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := cn.c.ReadMessage(100)
			if err != nil {
				if ke, ok := err.(confluentkafka.Error); ok && ke.Code() == confluentkafka.ErrTimedOut {
					continue
				}
				log.Printf("[consumer] error: %v", err)
				continue
			}
			topic := *msg.TopicPartition.Topic
			cn.eventCh <- router.Event{Topic: topic, Payload: msg.Value}
		}
	}
}

func (cn *Consumer) Close() { cn.c.Close() }
