package main

import (
	"context"
	"log"
	"os"

	"github.com/lotr/option-b/internal/api"
	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/game"
	kafkaclient "github.com/lotr/option-b/internal/kafka"
	"github.com/lotr/option-b/internal/router"
	"github.com/lotr/option-b/internal/state"
)

func main() {
	brokers := env("KAFKA_BROKERS", "localhost:9092")
	registryURL := env("SCHEMA_REGISTRY_URL", "http://localhost:8081")
	port := env("PORT", "8080")
	nodeID := env("NODE_ID", "go-1")
	unitsPath := env("UNITS_CONF", "../config/units.conf")
	mapPath := env("MAP_CONF", "../config/map.conf")

	log.Printf("[main] node=%s brokers=%s port=%s", nodeID, brokers, port)

	// ── 0. Schema Registry — register all Avro schemas at startup ─────────
	kafkaclient.RegisterSchemas(registryURL)

	// ── 1. Config ─────────────────────────────────────────────────────────
	cfg, err := config.Load(unitsPath, mapPath)
	if err != nil {
		log.Fatalf("[main] config: %v", err)
	}
	log.Printf("[main] loaded %d units, %d regions, %d paths",
		len(cfg.Game.Units), len(cfg.Map.Regions), len(cfg.Map.Paths))

	// ── 2. Initial cache (from config) ────────────────────────────────────
	initialCache := game.InitCache(cfg)

	// ── 3. State recovery — replay game.session to restore after crash ────
	recoverState(brokers, nodeID, &initialCache)

	// ── 4. Channels ───────────────────────────────────────────────────────
	eventCh := make(chan router.Event, 100)
	cacheUpdateCh := make(chan func(*state.WorldStateCache), 32)
	engineCh := make(chan router.Event, 64)

	// ── 5. CacheManager ───────────────────────────────────────────────────
	cm := state.NewCacheManager(initialCache)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cm.Run(ctx, cacheUpdateCh)

	// ── 6. Kafka (only available when CGO is enabled, i.e. in Docker) ─────
	var producer kafkaclient.MessageProducer
	producer = buildProducer(brokers)
	consumer := buildConsumer(brokers, nodeID, eventCh)
	go consumer.Run(ctx)
	defer consumer.Close()
	defer producer.Close()

	// ── 7. EventRouter ────────────────────────────────────────────────────
	lightSideSSECh := make(chan router.Event, 100)
	darkSideSSECh := make(chan router.Event, 100)
	evtRouter := router.NewEventRouter(eventCh, lightSideSSECh, darkSideSSECh, cacheUpdateCh, engineCh)
	go evtRouter.Run(ctx)

	// ── 8. TurnProcessor ──────────────────────────────────────────────────
	tp := game.NewTurnProcessor(cfg.PathsByID, cfg.RegionsByID)

	// ── 9. HTTP Server + select loop ──────────────────────────────────────
	// Server reads from EventRouter's pre-routed channels — NOT from raw eventCh.
	// EventRouter is the sole reader of eventCh, eliminating the race condition.
	srv := api.NewServer(lightSideSSECh, darkSideSSECh, cacheUpdateCh, engineCh, cm, port, tp, producer, cfg)
	srv.BootstrapKafka()
	srv.SetReady()
	srv.Run(ctx)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
