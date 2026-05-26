package kafka

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
)

// RegisterSchemas registers all Avro schemas with Confluent Schema Registry at startup.
// Idempotent — safe to call on every restart; Schema Registry returns the existing ID
// if the schema is already registered.
func RegisterSchemas(registryURL string) {
	schemas := []struct {
		subject string
		schema  string
	}{
		{"game.orders.raw-value", schemaOrderSubmitted},
		{"game.orders.validated-value", schemaOrderValidatedV2}, // V2 with routeRiskScore
		{"game.dlq-value", schemaDLQEntry},
		{"game.events.unit-value", schemaUnitEvent},
		{"game.events.region-value", schemaRegionEvent},
		{"game.events.path-value", schemaPathEvent},
		{"game.session-value", schemaSessionEvent},
		{"game.broadcast-value", schemaBroadcastEvent},
		{"game.ring.position-value", schemaRingPosition},
		{"game.ring.detection-value", schemaRingDetection},
	}

	for _, s := range schemas {
		id, err := registerSchema(registryURL, s.subject, s.schema)
		if err != nil {
			log.Printf("[schema-registry] WARN subject=%s: %v", s.subject, err)
		} else {
			log.Printf("[schema-registry] registered subject=%s id=%d", s.subject, id)
		}
	}
}

func registerSchema(registryURL, subject, schema string) (int, error) {
	url := fmt.Sprintf("%s/subjects/%s/versions", registryURL, subject)
	body, _ := json.Marshal(map[string]string{"schema": schema})
	resp, err := http.Post(url, "application/vnd.schemaregistry.v1+json", bytes.NewReader(body)) //nolint:noctx
	if err != nil {
		return 0, fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, raw)
	}
	var result struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}
	return result.ID, nil
}

// ── Avro Schema Definitions ───────────────────────────────────────────────

// schemaOrderSubmitted — game.orders.raw-value
const schemaOrderSubmitted = `{
  "type": "record",
  "name": "OrderSubmitted",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "playerId",  "type": "string"},
    {"name": "unitId",    "type": "string"},
    {"name": "orderType", "type": "string"},
    {"name": "payload",   "type": "string"},
    {"name": "turn",      "type": "int"},
    {"name": "timestamp", "type": "long"}
  ]
}`

// schemaOrderValidatedV2 — game.orders.validated-value (V2: adds nullable routeRiskScore)
// V1 consumers that do not know about routeRiskScore will ignore it (FORWARD compatibility).
const schemaOrderValidatedV2 = `{
  "type": "record",
  "name": "OrderValidated",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "playerId",        "type": "string"},
    {"name": "unitId",          "type": "string"},
    {"name": "orderType",       "type": "string"},
    {"name": "payload",         "type": "string"},
    {"name": "turn",            "type": "int"},
    {"name": "timestamp",       "type": "long"},
    {"name": "routeRiskScore",  "type": ["null", "int"], "default": null},
    {"name": "threatenedPaths", "type": {"type": "array", "items": "string"}, "default": []},
    {"name": "blockedPaths",    "type": {"type": "array", "items": "string"}, "default": []}
  ]
}`

// schemaDLQEntry — game.dlq-value
const schemaDLQEntry = `{
  "type": "record",
  "name": "DLQEntry",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "originalTopic", "type": "string"},
    {"name": "partition",     "type": "int"},
    {"name": "offset",        "type": "long"},
    {"name": "errorCode",     "type": "string"},
    {"name": "errorMessage",  "type": "string"},
    {"name": "rawPayload",    "type": "bytes"},
    {"name": "timestamp",     "type": "long"}
  ]
}`

// schemaUnitEvent — game.events.unit-value
const schemaUnitEvent = `{
  "type": "record",
  "name": "UnitEvent",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "id",       "type": "string"},
    {"name": "side",     "type": "string"},
    {"name": "class",    "type": "string"},
    {"name": "region",   "type": "string"},
    {"name": "status",   "type": "string"},
    {"name": "cooldown", "type": "int"}
  ]
}`

// schemaRegionEvent — game.events.region-value
const schemaRegionEvent = `{
  "type": "record",
  "name": "RegionEvent",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "regionId",      "type": "string"},
    {"name": "newController", "type": "string"},
    {"name": "attackerWon",   "type": "boolean"},
    {"name": "turn",          "type": "int"}
  ]
}`

// schemaPathEvent — game.events.path-value
const schemaPathEvent = `{
  "type": "record",
  "name": "PathEvent",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "id",                "type": "string"},
    {"name": "status",            "type": "string"},
    {"name": "surveillanceLevel", "type": "int"},
    {"name": "blockedBy",         "type": "string"},
    {"name": "from",              "type": "string"},
    {"name": "to",                "type": "string"}
  ]
}`

// schemaSessionEvent — game.session-value (log-compacted; key determines record type)
const schemaSessionEvent = `{
  "type": "record",
  "name": "SessionEvent",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "key",   "type": "string"},
    {"name": "value", "type": "string"}
  ]
}`

// schemaBroadcastEvent — game.broadcast-value
const schemaBroadcastEvent = `{
  "type": "record",
  "name": "BroadcastEvent",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "type",  "type": "string"},
    {"name": "turn",  "type": "int"},
    {"name": "units",   "type": {"type": "array", "items": {"type": "map", "values": "string"}}},
    {"name": "regions", "type": {"type": "array", "items": {"type": "map", "values": "string"}}},
    {"name": "paths",   "type": {"type": "array", "items": {"type": "map", "values": "string"}}}
  ]
}`

// schemaRingPosition — game.ring.position-value (Light Side only)
const schemaRingPosition = `{
  "type": "record",
  "name": "RingBearerMoved",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "trueRegion", "type": "string"},
    {"name": "turn",       "type": "int"}
  ]
}`

// schemaRingDetection — game.ring.detection-value (Dark Side only)
const schemaRingDetection = `{
  "type": "record",
  "name": "RingBearerDetected",
  "namespace": "com.rotr.game",
  "fields": [
    {"name": "regionId", "type": "string"},
    {"name": "turn",     "type": "int"}
  ]
}`
