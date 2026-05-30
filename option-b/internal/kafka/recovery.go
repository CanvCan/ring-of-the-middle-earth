//go:build cgo

package kafka

import (
	"encoding/json"
	"log"
	"time"

	confluentkafka "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/lotr/option-b/internal/state"
)

// PersistedState is the serializable subset of WorldStateCache written to game.session.
// Graph and UnitConfigs are excluded — they are rebuilt from config on startup.
type PersistedState struct {
	Turn            int                       `json:"turn"`
	MaxTurns        int                       `json:"maxTurns"`
	HiddenUntilTurn int                       `json:"hiddenUntilTurn"`
	Units           map[string]state.UnitSnapshot `json:"units"`
	Regions         map[string]state.RegionState  `json:"regions"`
	Paths           map[string]state.PathState    `json:"paths"`
	RingBearer      state.RingBearerState         `json:"ringBearer"`
	GameOver        bool                          `json:"gameOver"`
	Winner          string                        `json:"winner"`
	LightView       state.LightSideView           `json:"lightView"`
}

// RecoverStateFromKafka reads game.session from the beginning, finds the latest
// "world-state" record, and applies it to cache.  If no snapshot is found the
// cache is left unchanged (fresh game).
//
// The function uses a temporary consumer group so it does not interfere with
// the main game-engine consumer group.
func RecoverStateFromKafka(brokers, nodeID string, cache *state.WorldStateCache) {
	groupID := "recovery-" + nodeID + "-" + time.Now().Format("20060102150405")
	c, err := confluentkafka.NewConsumer(&confluentkafka.ConfigMap{
		"bootstrap.servers":        brokers,
		"group.id":                 groupID,
		"auto.offset.reset":        "earliest",
		"enable.auto.commit":       false,
		"session.timeout.ms":       10000,
		"max.poll.interval.ms":     30000,
	})
	if err != nil {
		log.Printf("[recovery] create consumer: %v", err)
		return
	}
	defer c.Close()

	topic := "game.session"
	if err := c.SubscribeTopics([]string{topic}, nil); err != nil {
		log.Printf("[recovery] subscribe: %v", err)
		return
	}

	log.Printf("[recovery] replaying game.session to restore state…")

	var latest *PersistedState
	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		msg, err := c.ReadMessage(2000)
		if err != nil {
			if ke, ok := err.(confluentkafka.Error); ok && ke.Code() == confluentkafka.ErrTimedOut {
				break // no more messages
			}
			log.Printf("[recovery] read: %v", err)
			break
		}

		if string(msg.Key) != "world-state" {
			continue
		}

		var ps PersistedState
		if err := json.Unmarshal(msg.Value, &ps); err != nil {
			log.Printf("[recovery] parse: %v", err)
			continue
		}
		latest = &ps
	}

	if latest == nil {
		log.Printf("[recovery] no snapshot found — starting fresh")
		return
	}

	// Apply recovered state, preserving read-only fields set by InitCache.
	cache.Turn = latest.Turn
	cache.MaxTurns = latest.MaxTurns
	cache.HiddenUntilTurn = latest.HiddenUntilTurn
	cache.Units = latest.Units
	cache.Regions = latest.Regions
	cache.Paths = latest.Paths
	cache.RingBearer = latest.RingBearer
	cache.GameOver = latest.GameOver
	cache.Winner = latest.Winner
	cache.LightView = latest.LightView
	cache.DarkView = state.DarkSideView{RingBearerRegion: ""} // invariant

	log.Printf("[recovery] restored turn=%d gameOver=%v", cache.Turn, cache.GameOver)
}
