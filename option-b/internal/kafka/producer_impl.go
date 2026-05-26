//go:build cgo

package kafka

import (
	"fmt"

	confluentkafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Producer is the real confluent-kafka-go producer.
// Only compiled when CGO is available (Linux/Docker).
type Producer struct {
	p *confluentkafka.Producer
}

// NewIdempotentProducer creates a producer with enable.idempotence=true.
// Guarantees exactly-once delivery for GameOver events (spec K6).
func NewIdempotentProducer(brokers string) (*Producer, error) {
	p, err := confluentkafka.NewProducer(&confluentkafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"enable.idempotence": true,
		"acks":               "all",
		"retries":            5,
		"retry.backoff.ms":   200,
	})
	if err != nil {
		return nil, fmt.Errorf("create idempotent producer: %w", err)
	}
	return &Producer{p: p}, nil
}

func (pr *Producer) Produce(topic, key string, value []byte) error {
	return pr.p.Produce(&confluentkafka.Message{
		TopicPartition: confluentkafka.TopicPartition{Topic: &topic, Partition: confluentkafka.PartitionAny},
		Key:            []byte(key),
		Value:          value,
	}, nil)
}

func (pr *Producer) ProduceSync(topic, key string, value []byte) error {
	ch := make(chan confluentkafka.Event, 1)
	if err := pr.p.Produce(&confluentkafka.Message{
		TopicPartition: confluentkafka.TopicPartition{Topic: &topic, Partition: confluentkafka.PartitionAny},
		Key:            []byte(key),
		Value:          value,
	}, ch); err != nil {
		return err
	}
	e := <-ch
	if m, ok := e.(*confluentkafka.Message); ok && m.TopicPartition.Error != nil {
		return m.TopicPartition.Error
	}
	return nil
}

func (pr *Producer) Flush(timeoutMs int) int { return pr.p.Flush(timeoutMs) }
func (pr *Producer) Close()                  { pr.p.Close() }
