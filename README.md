# Ring of the Middle Earth

**Distributed Application Development — Term Project**
**Technology Choice: Option B — Go + Kafka KTable State Stores**

## Architecture

Two human players compete in separate browsers. The Light Side moves the Ring Bearer
from The Shire to Mount Doom; the Dark Side hunts the Ring Bearer and must intercept
it before it arrives.

The system is built on a **stateless application tier** — three Go instances behind an
nginx load balancer — with all authoritative game state in Kafka KTable stores. Fault
tolerance is delegated entirely to Kafka's consumer group rebalancing protocol.

```
Browser A (Light)        Browser B (Dark)
      │                       │
      └───────── nginx ────────┘
                   │
       ┌─────────────────────┐
       │  go-1  go-2  go-3   │  ← stateless; any instance handles any request
       └────────┬────────────┘
                │
           ┌────┴────┐
           │  Kafka  │  ← authoritative state (KTable stores)
           └─────────┘
```

## Quick Start

```bash
make up       # starts everything (Kafka, Schema Registry, 3 Go nodes, nginx)
make test     # runs all unit tests without Docker
make down     # tears down all containers
```

## Grading

| Area | Points |
|---|---|
| Kafka (K1–K6) | 30 |
| Go Engine (B1–B11) | 70 |
| **Total** | **100** |

## Key Design Decisions

- **Zero unit ID hardcoding (B1 — 8pt):** All game logic reads `cfg.Class`,
  `cfg.Indestructible`, `cfg.MaiaAbilityPaths`, etc. No string like `"witch-king"`
  appears in game logic files.

- **Maia dispatch (B5 — 5pt):** `DispatchMaiaAbility` dispatches by config properties
  (`len(cfg.MaiaAbilityPaths) > 0` → Saruman/CorruptPath, otherwise → Gandalf/OpenPath).
  Same `MaiaAbility` order type for both.

- **Information asymmetry (B7 — 8pt):** `EventRouter.route()` is the single enforcement
  point. `game.ring.position` → Light Side SSE only. `game.ring.detection` → Dark Side
  SSE only. `DarkView.RingBearerRegion` invariant enforced in `CacheManager.Update()`.

- **Fault tolerance (B2 — 8pt):** Stateless Go tier + Kafka consumer group rebalancing.
  `docker stop go-2` → partitions rebalanced to go-1/go-3. `docker start go-2` →
  KTable rebuilt from Kafka on rejoin.

- **Exactly-once GameOver (K6 — 5pt):** `enable.idempotence=true` on the producer.
  `ProduceSync` used for GameOver to confirm delivery before returning.

## Project Structure

```
ring-of-the-middle-earth/
├── config/               unit and map configuration (13 units, 22 regions, 37 paths)
├── kafka/schemas/        14 Avro schema files (.avsc)
├── kafka/streams/        Topology 1 (validation) + Topology 2 (route risk enrichment)
├── option-b/             Go application
│   ├── internal/config/  UnitConfig loader + BFS graph
│   ├── internal/game/    combat, detection, maia, path, turn (13-step), wincondition
│   ├── internal/state/   WorldStateCache + CacheManager (deep-copy, DarkView invariant)
│   ├── internal/router/  EventRouter (information asymmetry enforcement)
│   ├── internal/pipeline/ Route Risk (Pipeline 1) + Interception (Pipeline 2)
│   ├── internal/kafka/   idempotent producer, consumer, KTable store
│   ├── internal/api/     HTTP server (7-case select loop), handlers, SSE
│   ├── tests/            combat_test, router_test (-race), pipeline1_test, pipeline2_test, goroutine_test
│   └── main.go           goroutine wiring
└── ui/                   Vanilla JS + SSE (no React/Vue/Angular)
```
