package router

import (
	"context"
	"encoding/json"

	"github.com/lotr/option-b/internal/state"
)

// Event wraps a Kafka message with its topic.
type Event struct {
	Topic   string
	Payload []byte
}

// EventRouter is the single enforcement point for information asymmetry.
// Ring Bearer's true position NEVER reaches the Dark Side SSE channel.
type EventRouter struct {
	eventCh        <-chan Event
	lightSideSSECh chan<- Event
	darkSideSSECh  chan<- Event
	cacheUpdateCh  chan<- func(*state.WorldStateCache)
	engineCh       chan<- Event
}

func NewEventRouter(
	eventCh <-chan Event,
	lightSideSSECh chan<- Event,
	darkSideSSECh chan<- Event,
	cacheUpdateCh chan<- func(*state.WorldStateCache),
	engineCh chan<- Event,
) *EventRouter {
	return &EventRouter{
		eventCh:        eventCh,
		lightSideSSECh: lightSideSSECh,
		darkSideSSECh:  darkSideSSECh,
		cacheUpdateCh:  cacheUpdateCh,
		engineCh:       engineCh,
	}
}

// Run starts the EventRouter goroutine.
func (r *EventRouter) Run(ctx context.Context) {
	for {
		select {
		case event, ok := <-r.eventCh:
			if !ok {
				return
			}
			r.route(event)
		case <-ctx.Done():
			return
		}
	}
}

func (r *EventRouter) route(event Event) {
	switch event.Topic {

	case "game.ring.position":
		// RingBearerMoved — Light Side ONLY. NEVER darkSideSSECh.
		r.lightSideSSECh <- event

	case "game.ring.detection":
		// RingBearerDetected / RingBearerSpotted — Dark Side ONLY. NEVER lightSideSSECh.
		r.darkSideSSECh <- event

	case "game.broadcast":
		// WorldStateSnapshot / GameOver — both sides, RB stripped for Dark Side.
		r.lightSideSSECh <- event
		r.darkSideSSECh <- stripRingBearer(event)

	case "game.events.unit", "game.events.region", "game.events.path":
		// No RB position in these topics — safe for both sides.
		r.lightSideSSECh <- event
		r.darkSideSSECh <- event

	case "game.orders.validated":
		r.engineCh <- event
	}
}

// stripRingBearer removes the Ring Bearer's true region from a WorldStateSnapshot
// before it is sent to the Dark Side SSE channel.
//
// Identification is always by unit class ("RingBearer"), never by unit ID string.
// This guarantees the invariant holds regardless of what ID is assigned in units.conf.
func stripRingBearer(event Event) Event {
	var payload map[string]interface{}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		safe, _ := json.Marshal(map[string]interface{}{"error": "parse_failure"})
		return Event{Topic: event.Topic, Payload: safe}
	}

	// 1. Strip currentRegion from units array; collect all RingBearer unit IDs by class.
	//    Using class, not ID — no hardcoded string literal.
	ringBearerIDs := make(map[string]bool)
	if units, ok := payload["units"].([]interface{}); ok {
		for _, u := range units {
			if unit, ok := u.(map[string]interface{}); ok {
				if unit["class"] == "RingBearer" {
					unit["currentRegion"] = ""
					if id, ok := unit["id"].(string); ok && id != "" {
						ringBearerIDs[id] = true
					}
				}
			}
		}
		payload["units"] = units
	}

	// 2. Strip RingBearer IDs from region.unitsPresent lists.
	//    Uses the ringBearerIDs set collected above — no hardcoded unit ID.
	if regions, ok := payload["regions"].(map[string]interface{}); ok {
		for _, rv := range regions {
			if region, ok := rv.(map[string]interface{}); ok {
				if up, ok := region["unitsPresent"].([]interface{}); ok {
					filtered := make([]interface{}, 0, len(up))
					for _, entry := range up {
						if id, ok := entry.(string); !ok || !ringBearerIDs[id] {
							filtered = append(filtered, entry)
						}
					}
					region["unitsPresent"] = filtered
				}
			}
		}
	}

	// 3. Belt-and-suspenders: remove any top-level ring bearer region fields
	delete(payload, "ringBearerRegion")
	delete(payload, "trueRegion")

	stripped, _ := json.Marshal(payload)
	return Event{Topic: event.Topic, Payload: stripped}
}
