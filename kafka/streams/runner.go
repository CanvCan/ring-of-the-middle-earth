package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Runner wires Topology1 and Topology2 to Kafka consumers.
type Runner struct {
	brokers  string
	producer *kafka.Producer
	t1       *Topology1Processor
	t2       *Topology2Processor
}

func NewRunner(brokers string) (*Runner, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"enable.idempotence": true,
		"acks":               "all",
	})
	if err != nil {
		return nil, err
	}

	return &Runner{
		brokers:  brokers,
		producer: p,
		t1:       NewTopology1Processor(p),
		t2:       NewTopology2Processor(p),
	}, nil
}

// Run starts both topology consumers and blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) error {
	go r.runTopology1(ctx)
	go r.runTopology2(ctx)

	<-ctx.Done()
	r.producer.Flush(5000)
	r.producer.Close()
	return nil
}

func (r *Runner) runTopology1(ctx context.Context) {
	topics := []string{
		"game.orders.raw",
		"game.session",       // → TurnKTable + PlayerKTable
		"game.events.unit",   // → UnitKTable
		"game.events.path",   // → PathKTable
		"game.events.region", // → RegionKTable (Rule 6: enemy-controlled check)
	}
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": r.brokers,
		"group.id":          "streams-t1",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		log.Printf("[streams-t1] consumer error: %v", err)
		return
	}
	defer c.Close()
	_ = c.SubscribeTopics(topics, nil)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := c.ReadMessage(100)
			if err != nil {
				if ke, ok := err.(kafka.Error); ok && ke.Code() == kafka.ErrTimedOut {
					continue
				}
				log.Printf("[streams-t1] read: %v", err)
				continue
			}
			topic := *msg.TopicPartition.Topic
			switch topic {
			case "game.orders.raw":
				r.t1.ProcessMessage(msg)
			case "game.session":
				r.routeSessionRecord(msg)
			case "game.events.unit":
				r.t1.UpdateUnitTable(string(msg.Key), msg.Value)
			case "game.events.path":
				r.t1.UpdatePathTable(string(msg.Key), msg.Value)
			case "game.events.region":
				r.t1.UpdateRegionTable(string(msg.Key), msg.Value)
			}
		}
	}
}

// routeSessionRecord dispatches game.session records to the right KTable updater.
// The key determines the record type:
//   - "turn-state"   → TurnKTable
//   - "world-state"  → ignored by Topology1 (used for state recovery)
//   - anything else  → treated as playerID → PlayerKTable
func (r *Runner) routeSessionRecord(msg *kafka.Message) {
	key := string(msg.Key)
	switch key {
	case "turn-state":
		r.t1.UpdateTurnTable(key, msg.Value)
	case "world-state":
		// Used for state recovery — not needed by Topology1.
	default:
		// Assume key is a playerID carrying a PlayerSession record.
		r.t1.UpdatePlayerTable(key, msg.Value)
	}
}

func (r *Runner) runTopology2(ctx context.Context) {
	topics := []string{
		"game.orders.validated",
		"game.events.path",
		"game.events.region",
		"game.events.unit",
	}
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": r.brokers,
		"group.id":          "streams-t2",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		log.Printf("[streams-t2] consumer error: %v", err)
		return
	}
	defer c.Close()
	_ = c.SubscribeTopics(topics, nil)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := c.ReadMessage(100)
			if err != nil {
				if ke, ok := err.(kafka.Error); ok && ke.Code() == kafka.ErrTimedOut {
					continue
				}
				log.Printf("[streams-t2] read: %v", err)
				continue
			}
			topic := *msg.TopicPartition.Topic
			switch topic {
			case "game.orders.validated":
				r.t2.ProcessMessage(msg)
			case "game.events.path":
				r.t2.UpdatePathTable(string(msg.Key), msg.Value)
			case "game.events.region":
				r.t2.UpdateRegionTable(string(msg.Key), msg.Value)
			case "game.events.unit":
				r.t2.UpdateUnitTable(string(msg.Key), msg.Value)
			}
		}
	}
}

// allMapPaths returns all 37 path definitions as [pathID, from, to].
// Used by both Topology1 and Topology2 to build their in-memory graphs.
func allMapPaths() [][3]string {
	return [][3]string{
		{"shire-to-bree", "the-shire", "bree"},
		{"bree-to-weathertop", "bree", "weathertop"},
		{"bree-to-rivendell", "bree", "rivendell"},
		{"bree-to-tharbad", "bree", "tharbad"},
		{"shire-to-tharbad", "the-shire", "tharbad"},
		{"weathertop-to-rivendell", "weathertop", "rivendell"},
		{"rivendell-to-moria", "rivendell", "moria"},
		{"rivendell-to-lothlorien", "rivendell", "lothlorien"},
		{"moria-to-lothlorien", "moria", "lothlorien"},
		{"lothlorien-to-emyn-muil", "lothlorien", "emyn-muil"},
		{"lothlorien-to-rohan-plains", "lothlorien", "rohan-plains"},
		{"rohan-plains-to-fangorn", "rohan-plains", "fangorn"},
		{"rohan-plains-to-edoras", "rohan-plains", "edoras"},
		{"rohan-plains-to-minas-tirith", "rohan-plains", "minas-tirith"},
		{"fangorn-to-isengard", "fangorn", "isengard"},
		{"isengard-to-rohan-plains", "isengard", "rohan-plains"},
		{"tharbad-to-fords-of-isen", "tharbad", "fords-of-isen"},
		{"fords-of-isen-to-isengard", "fords-of-isen", "isengard"},
		{"fords-of-isen-to-helms-deep", "fords-of-isen", "helms-deep"},
		{"fords-of-isen-to-edoras", "fords-of-isen", "edoras"},
		{"edoras-to-helms-deep", "edoras", "helms-deep"},
		{"helms-deep-to-isengard", "helms-deep", "isengard"},
		{"edoras-to-minas-tirith", "edoras", "minas-tirith"},
		{"emyn-muil-to-dead-marshes", "emyn-muil", "dead-marshes"},
		{"emyn-muil-to-ithilien", "emyn-muil", "ithilien"},
		{"dead-marshes-to-ithilien", "dead-marshes", "ithilien"},
		{"dead-marshes-to-mordor", "dead-marshes", "mordor"},
		{"ithilien-to-minas-tirith", "ithilien", "minas-tirith"},
		{"ithilien-to-osgiliath", "ithilien", "osgiliath"},
		{"ithilien-to-cirith-ungol", "ithilien", "cirith-ungol"},
		{"minas-tirith-to-osgiliath", "minas-tirith", "osgiliath"},
		{"osgiliath-to-minas-morgul", "osgiliath", "minas-morgul"},
		{"minas-morgul-to-cirith-ungol", "minas-morgul", "cirith-ungol"},
		{"minas-morgul-to-mordor", "minas-morgul", "mordor"},
		{"cirith-ungol-to-mordor", "cirith-ungol", "mordor"},
		{"cirith-ungol-to-mount-doom", "cirith-ungol", "mount-doom"},
		{"mordor-to-mount-doom", "mordor", "mount-doom"},
	}
}

// publishUnitKTableUpdate emits a unit's state to game.events.unit so that
// Topology1's UnitKTable stays current when units move or take damage.
// Called by the game engine after each turn; here for reference in testing.
func publishUnitKTableUpdate(p *kafka.Producer, unitID string, state UnitState) {
	data, _ := json.Marshal(state)
	topic := "game.events.unit"
	_ = p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(unitID),
		Value:          data,
	}, nil)
}
