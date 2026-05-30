package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lotr/option-b/internal/config"
	"github.com/lotr/option-b/internal/game"
	kafkaclient "github.com/lotr/option-b/internal/kafka"
	"github.com/lotr/option-b/internal/pipeline"
	"github.com/lotr/option-b/internal/router"
	"github.com/lotr/option-b/internal/state"
)

// CacheAccess is the interface the Server uses to read/write the world state.
type CacheAccess interface {
	Get() state.WorldStateCache
	Update(func(*state.WorldStateCache))
}

// SSEConnection represents one connected browser client.
type SSEConnection struct {
	PlayerID string
	Side     string
	WriteCh  chan []byte
}

// AnalysisRequest is sent from HTTP handlers to the analysis goroutine.
type AnalysisRequest struct {
	Type    string
	ReplyCh chan []byte
}

// Server holds all channels and HTTP mux. It owns the main select loop.
type Server struct {
	// lightSideSSECh and darkSideSSECh are fed by EventRouter — no raw eventCh.
	// EventRouter is the single reader of the raw Kafka eventCh and the single
	// enforcement point for information asymmetry. Server only sees pre-routed events.
	lightSideSSECh        <-chan router.Event
	darkSideSSECh         <-chan router.Event
	newConnectionCh       chan SSEConnection
	disconnectCh          chan string
	analysisRequestCh     chan AnalysisRequest
	cacheUpdateCh         chan func(*state.WorldStateCache)
	engineCh              <-chan router.Event // reads Kafka-validated orders from EventRouter
	cache                 CacheAccess
	tp                    *game.TurnProcessor
	producer              kafkaclient.MessageProducer
	cfg                   *config.Config
	routePipeline         *pipeline.RouteRiskPipeline
	interceptPipeline     *pipeline.InterceptPipeline
	mux                   *http.ServeMux
	port                  string

	// Sequential turn model: Light phase → Dark phase → process → repeat
	activeSide            string            // "FREE_PEOPLES" or "SHADOW"
	activeSideMu          sync.RWMutex
	kafkaValidatedOrders  []json.RawMessage // orders validated by Topology1 via game.orders.validated
	kafkaValidatedMu      sync.Mutex
	submitCh              chan string        // player sends their side when they dispatch
	playerSides           map[string]string // playerID → side (from SSE connections)
	playerSidesMu         sync.RWMutex
	ready                 atomic.Bool       // true once Kafka recovery + consumers are up

	// localOrderInject is non-nil in local/no-Kafka mode (NoopProducer).
	// handleOrder calls it to write order bytes directly and synchronously into
	// kafkaValidatedOrders, bypassing the entire async chain. This guarantees
	// every order is queued before the HTTP 202 response is returned to the browser
	// — and therefore before the browser can ever send /orders/dispatch.
	localOrderInject func(raw []byte)
}

func NewServer(
	lightSideSSECh <-chan router.Event,
	darkSideSSECh <-chan router.Event,
	cacheUpdateCh chan func(*state.WorldStateCache),
	engineCh <-chan router.Event,
	cache CacheAccess,
	port string,
	tp *game.TurnProcessor,
	producer kafkaclient.MessageProducer,
	cfg *config.Config,
) *Server {
	s := &Server{
		lightSideSSECh:    lightSideSSECh,
		darkSideSSECh:     darkSideSSECh,
		newConnectionCh:   make(chan SSEConnection, 16),
		disconnectCh:      make(chan string, 16),
		analysisRequestCh: make(chan AnalysisRequest, 8),
		cacheUpdateCh:     cacheUpdateCh,
		engineCh:          engineCh,
		cache:             cache,
		tp:                tp,
		producer:          producer,
		cfg:               cfg,
		routePipeline:     pipeline.NewRouteRiskPipeline(),
		interceptPipeline: pipeline.NewInterceptPipeline(),
		mux:               http.NewServeMux(),
		port:              port,
		activeSide:        config.SideLight,
		submitCh:          make(chan string, 4),
		playerSides:       make(map[string]string),
	}
	// In local mode (no Kafka), wire a direct synchronous inject so handleOrder
	// writes straight into kafkaValidatedOrders before returning HTTP 202.
	// With real Kafka (CGO build) producer is never a NoopProducer, so this stays nil.
	if noop, ok := producer.(*kafkaclient.NoopProducer); ok {
		_ = noop // keep reference so the compiler doesn't optimise it away
		s.localOrderInject = func(raw []byte) {
			s.kafkaValidatedMu.Lock()
			s.kafkaValidatedOrders = append(s.kafkaValidatedOrders, json.RawMessage(raw))
			s.kafkaValidatedMu.Unlock()
		}
	}
	s.registerRoutes()
	return s
}

// SetReady marks the server as ready to serve traffic (called from main after recovery).
func (s *Server) SetReady() { s.ready.Store(true) }

// BootstrapKafka publishes the initial state of the game to Kafka topics so that
// Topology 1 (Kafka Streams) has the data needed to validate orders on a fresh start.
func (s *Server) BootstrapKafka() {
	snap := s.cache.Get()
	if snap.Turn > 0 {
		return // Not a fresh start, state was recovered
	}

	log.Printf("[server] bootstrapping Kafka topics with initial state")

	pub := func(topic, key string, payload interface{}) {
		data, _ := json.Marshal(payload)
		if err := s.producer.ProduceSync(topic, key, data); err != nil {
			log.Printf("[bootstrap] publish %s %s: %v", topic, key, err)
		}
	}

	for pid, p := range snap.Paths {
		pcfg := s.cfg.PathsByID[pid]
		pub("game.events.path", pid, map[string]interface{}{
			"id":                pid,
			"status":            string(p.Status),
			"surveillanceLevel": p.SurveillanceLevel,
			"blockedBy":         p.BlockedBy,
			"from":              pcfg.From,
			"to":                pcfg.To,
		})
	}

	for rid, r := range snap.Regions {
		pub("game.events.region", rid, map[string]interface{}{
			"id":      rid,
			"control": string(r.Control),
		})
	}

	for uid, u := range snap.Units {
		ucfg := snap.UnitConfigs[uid]
		pub("game.events.unit", uid, map[string]interface{}{
			"id":       uid,
			"side":     ucfg.Side,
			"class":    ucfg.Class,
			"region":   u.Region,
			"status":   string(u.Status),
			"cooldown": u.Cooldown,
		})
	}
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/api/game/start",         s.handleGameStart)
	s.mux.HandleFunc("/api/order",              s.handleOrder)
	s.mux.HandleFunc("/api/game/state",         s.handleGameState)
	s.mux.HandleFunc("/api/orders/available",   s.handleOrdersAvailable)
	s.mux.HandleFunc("/api/analysis/routes",    s.handleAnalysisRoutes)
	s.mux.HandleFunc("/api/analysis/intercept", s.handleAnalysisIntercept)
	s.mux.HandleFunc("/api/events",             s.handleSSE)
	s.mux.HandleFunc("/api/orders/dispatch",    s.handleDispatch)
	s.mux.HandleFunc("/api/health",             s.handleHealth)
	s.mux.Handle("/debug/pprof/", http.DefaultServeMux)
	// Static UI files — works without Nginx (dev mode)
	uiDir := os.Getenv("UI_DIR")
	if uiDir == "" {
		uiDir = "../../ui"
	}
	s.mux.Handle("/", noCacheFS{http.FileServer(http.Dir(uiDir))})
}

// noCacheFS wraps FileServer to add no-cache headers on every response.
type noCacheFS struct{ fs http.Handler }

func (n noCacheFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	n.fs.ServeHTTP(w, r)
}

// Run is the main select loop (spec Section 31 — 7 cases).
func (s *Server) Run(ctx context.Context) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)

	clients := map[string]SSEConnection{}

	httpSrv := &http.Server{Addr: ":" + s.port, Handler: s.mux}
	go func() {
		log.Printf("[server] listening on :%s", s.port)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[server] http error: %v", err)
		}
	}()

	turnTimer := time.NewTimer(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
	defer turnTimer.Stop()

	for {
		// Priority: give dispatch (submitCh) higher priority than the turn timer.
		// Without this, Go's select randomly picks between a simultaneously-ready
		// timer and submitCh, which can cause a dispatch to be silently ignored
		// after the timer already advanced activeSide.
		select {
		case side := <-s.submitCh:
			s.processDispatch(side, clients, turnTimer)
			continue
		default:
		}

		select {

		// 1a. Pre-routed Light Side event from EventRouter (game.ring.position, game.broadcast full, etc.)
		case msg, ok := <-s.lightSideSSECh:
			if !ok {
				return
			}
			s.fanOutToSide(msg, clients, config.SideLight)

		// 1b. Pre-routed Dark Side event from EventRouter (game.ring.detection, game.broadcast stripped, etc.)
		case msg, ok := <-s.darkSideSSECh:
			if !ok {
				return
			}
			s.fanOutToSide(msg, clients, config.SideDark)

		// 1c. Validated order from Topology1 (game.orders.validated → EventRouter → engineCh)
		case event, ok := <-s.engineCh:
			if !ok {
				return
			}
			if event.Topic == "game.orders.validated" {
				s.kafkaValidatedMu.Lock()
				s.kafkaValidatedOrders = append(s.kafkaValidatedOrders, json.RawMessage(event.Payload))
				s.kafkaValidatedMu.Unlock()
			}

		// 2. New SSE client
		case conn := <-s.newConnectionCh:
			clients[conn.PlayerID] = conn
			s.playerSidesMu.Lock()
			s.playerSides[conn.PlayerID] = conn.Side
			s.playerSidesMu.Unlock()
			log.Printf("[sse] +%s (%s) total=%d", conn.PlayerID, conn.Side, len(clients))
			// Publish player→side mapping to game.session so Topology1 can enforce Rule 2.
			go s.publishPlayerSession(conn.PlayerID, conn.Side)
			// Push current state immediately on connect
			go s.pushStateToClient(conn)

		// 3. SSE client disconnected
		case pid := <-s.disconnectCh:
			if c, ok := clients[pid]; ok {
				close(c.WriteCh)
				delete(clients, pid)
				log.Printf("[sse] -%s total=%d", pid, len(clients))
			}

		// 4. Analysis request
		case req := <-s.analysisRequestCh:
			go s.runAnalysis(req)

		// 5. Cache update
		case fn, ok := <-s.cacheUpdateCh:
			if !ok {
				return
			}
			s.cache.Update(fn)

		// 6. Turn timer — auto-advance phase or process turn if time is up
		case <-turnTimer.C:
			s.activeSideMu.RLock()
			active := s.activeSide
			s.activeSideMu.RUnlock()
			if active == config.SideLight {
				// Light ran out of time — auto-advance to Dark's phase
				log.Printf("[turn] Light phase timed out — advancing to Dark phase")
				s.activeSideMu.Lock()
				s.activeSide = config.SideDark
				s.activeSideMu.Unlock()
				s.broadcastPhaseChange(clients, config.SideDark)
				turnTimer.Reset(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
			} else {
				// Dark ran out of time — switch phase immediately, then process the turn
				log.Printf("[turn] Dark phase timed out — processing turn")
				s.activeSideMu.Lock()
				s.activeSide = config.SideLight
				s.activeSideMu.Unlock()
				s.broadcastPhaseChange(clients, config.SideLight)
				s.processTurnEnd(clients)
				turnTimer.Reset(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
			}

		// 8. Player dispatched orders — advance phase or process turn.
		// The priority select above handles the common path; this case catches
		// dispatches that arrive while other cases (SSE, cache, etc.) are selected.
		case side := <-s.submitCh:
			s.processDispatch(side, clients, turnTimer)

		// 7. OS signal
		case sig := <-signalCh:
			log.Printf("[server] signal %v — shutting down", sig)
			shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(shutCtx)
			return
		}
	}
}

// processDispatch handles a player dispatch signal. It gives priority to dispatch
// over the timer: if the timer already advanced activeSide before this dispatch
// arrived, we re-broadcast the current phase so the client stays in sync.
func (s *Server) processDispatch(side string, clients map[string]SSEConnection, turnTimer *time.Timer) {
	s.activeSideMu.RLock()
	active := s.activeSide
	s.activeSideMu.RUnlock()
	if side != active {
		// Timer fired first — re-broadcast current phase so the client isn't stuck.
		log.Printf("[turn] late dispatch from %s (active=%s) — re-broadcasting current phase", side, active)
		s.broadcastPhaseChange(clients, active)
		return
	}
	if side == config.SideLight {
		// If Light submitted DESTROY_RING and all conditions are met, process the turn
		// immediately — no need to wait for Dark's phase.
		if s.destroyRingConditionMet() {
			log.Printf("[turn] Light DESTROY_RING conditions met — processing turn immediately")
			if !turnTimer.Stop() {
				select {
				case <-turnTimer.C:
				default:
				}
			}
			s.activeSideMu.Lock()
			s.activeSide = config.SideLight
			s.activeSideMu.Unlock()
			s.broadcastPhaseChange(clients, config.SideLight)
			s.processTurnEnd(clients)
			turnTimer.Reset(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
			return
		}
		// Normal case — advance to Dark's phase
		log.Printf("[turn] Light dispatched — starting Dark phase")
		s.activeSideMu.Lock()
		s.activeSide = config.SideDark
		s.activeSideMu.Unlock()
		if !turnTimer.Stop() {
			select {
			case <-turnTimer.C:
			default:
			}
		}
		turnTimer.Reset(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
		s.broadcastPhaseChange(clients, config.SideDark)
	} else {
		// Dark done — switch phase immediately so browser reacts without delay, then process turn
		log.Printf("[turn] Dark dispatched — processing turn now")
		if !turnTimer.Stop() {
			select {
			case <-turnTimer.C:
			default:
			}
		}
		s.activeSideMu.Lock()
		s.activeSide = config.SideLight
		s.activeSideMu.Unlock()
		s.broadcastPhaseChange(clients, config.SideLight)
		s.processTurnEnd(clients)
		turnTimer.Reset(time.Duration(s.cfg.Game.TurnDurationSeconds) * time.Second)
	}
}

// processTurnEnd runs the 13-step turn, updates cache, and pushes state to all SSE clients.
func (s *Server) processTurnEnd(clients map[string]SSEConnection) {
	cache := s.cache.Get()
	if cache.GameOver {
		return
	}

	// Drain any orders that are still in-flight in engineCh before snapshotting
	// kafkaValidatedOrders. This closes the race between order arrival and dispatch:
	// the HTTP handler for /order completes (and returns 202) before /orders/dispatch
	// is sent by the browser, so all orders are guaranteed to be in engineCh or
	// kafkaValidatedOrders by the time we get here. A non-blocking drain picks up
	// anything still buffered in the channel.
	for {
		select {
		case event := <-s.engineCh:
			if event.Topic == "game.orders.validated" {
				s.kafkaValidatedMu.Lock()
				s.kafkaValidatedOrders = append(s.kafkaValidatedOrders, json.RawMessage(event.Payload))
				s.kafkaValidatedMu.Unlock()
			}
		default:
			goto ordersReady
		}
	}
ordersReady:

	// Collect and clear Kafka-validated orders (arrived via game.orders.validated → engineCh)
	s.kafkaValidatedMu.Lock()
	rawOrders := make([]json.RawMessage, len(s.kafkaValidatedOrders))
	copy(rawOrders, s.kafkaValidatedOrders)
	s.kafkaValidatedOrders = nil
	s.kafkaValidatedMu.Unlock()

	// Enforce one-order-per-unit: first order wins, duplicates are dropped
	seenUnits := map[string]bool{}

	// Parse orders into batch
	batch := game.OrderBatch{Turn: cache.Turn}
	for _, raw := range rawOrders {
		var base game.BaseOrder
		if err := json.Unmarshal(raw, &base); err != nil {
			continue
		}
		if base.UnitID != "" {
			if seenUnits[base.UnitID] {
				log.Printf("[order] duplicate order for unit %s — skipped", base.UnitID)
				continue
			}
			seenUnits[base.UnitID] = true
		}
		switch base.OrderType {
		case game.OrderAssignRoute:
			var o game.AssignRouteOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.AssignRoutes = append(batch.AssignRoutes, o)
			}
		case game.OrderRedirectUnit:
			var o game.RedirectUnitOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.Redirects = append(batch.Redirects, o)
			}
		case game.OrderBlockPath:
			var o game.BlockPathOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.BlockPaths = append(batch.BlockPaths, o)
			}
		case game.OrderSearchPath:
			var o game.SearchPathOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.SearchPaths = append(batch.SearchPaths, o)
			}
		case game.OrderAttackRegion:
			var o game.AttackRegionOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.Attacks = append(batch.Attacks, o)
			}
		case game.OrderReinforceRegion:
			var o game.ReinforceRegionOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.Reinforces = append(batch.Reinforces, o)
			}
		case game.OrderFortifyRegion:
			var o game.FortifyRegionOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.Fortifies = append(batch.Fortifies, o)
			}
		case game.OrderMaiaAbility:
			var o game.MaiaAbilityOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.MaiaAbilities = append(batch.MaiaAbilities, o)
			}
		case game.OrderDeployNazgul:
			var o game.DeployNazgulOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.DeployNazguls = append(batch.DeployNazguls, o)
			}
		case game.OrderDestroyRing:
			var o game.DestroyRingOrder
			if json.Unmarshal(raw, &o) == nil {
				batch.DestroyRing = &o
			}
		}
	}

	events, err := s.tp.ProcessTurn(&cache, batch)
	if err != nil {
		log.Printf("[turn] error: %v", err)
		return
	}

	// Commit updated cache
	s.cache.Update(func(c *state.WorldStateCache) { *c = cache })

	// Determine Dark Side player ID for detection event keying (spec: key=playerId).
	darkPlayerID := s.darkPlayerID()

	// Publish turn-state to game.session synchronously so Topology1 always sees
	// the correct current turn before the next order is submitted.
	s.publishTurnState(cache.Turn)

	// Publish game events to Kafka asynchronously (unit moves, path changes, etc.)
	go s.publishEvents(events, cache.Turn, darkPlayerID)

	// Persist state snapshot to game.session for crash recovery (B2).
	go s.publishStateSnapshot(cache)

	log.Printf("[turn] T%d done — units moved=%d gameOver=%v", cache.Turn, len(events.UnitMoved), cache.GameOver)

	// ── Push WorldStateSnapshot to all connected SSE clients immediately ──
	s.pushStateToAllClients(clients, cache)

	// Push individual events (path changes, battles, detections)
	s.pushTurnEventsToClients(clients, events, cache)

	// ── Auto-trigger Pipeline 1 on RouteCompromised (spec §32) ──────────
	if len(events.RouteCompromised) > 0 {
		var lightChs []chan<- []byte
		for _, conn := range clients {
			if conn.Side == config.SideLight {
				lightChs = append(lightChs, conn.WriteCh)
			}
		}
		cacheSnap := s.cache.Get()
		go func() {
			result := s.routePipeline.Run(context.Background(), buildCanonicalRouteJobs(), cacheSnap)
			data, _ := json.Marshal(map[string]interface{}{"type": "RouteRiskUpdate", "result": result})
			for _, ch := range lightChs {
				select {
				case ch <- data:
				default:
				}
			}
		}()
	}

	// ── Auto-trigger Pipeline 2 on RingBearerDetected (spec §33) ────────
	if len(events.RingBearerDetected) > 0 {
		var darkChs []chan<- []byte
		for _, conn := range clients {
			if conn.Side == config.SideDark {
				darkChs = append(darkChs, conn.WriteCh)
			}
		}
		cacheSnap := s.cache.Get()
		go func() {
			jobs := pipeline.BuildInterceptJobs(cacheSnap, buildCanonicalRouteJobs())
			result := s.interceptPipeline.Run(context.Background(), jobs, cacheSnap)
			data, _ := json.Marshal(map[string]interface{}{"type": "InterceptPlanUpdate", "result": result})
			for _, ch := range darkChs {
				select {
				case ch <- data:
				default:
				}
			}
		}()
	}
}

// pushStateToAllClients sends a full WorldStateSnapshot to every connected client.
func (s *Server) pushStateToAllClients(clients map[string]SSEConnection, cache state.WorldStateCache) {
	for _, conn := range clients {
		s.pushStateToClient(conn)
	}
}

// pushStateToClient sends the current world state snapshot to one client.
func (s *Server) pushStateToClient(conn SSEConnection) {
	cache := s.cache.Get()

	units := make([]map[string]interface{}, 0, len(cache.Units))
	for _, u := range cache.Units {
		cfg := cache.UnitConfigs[u.ID]
		region := u.Region
		if cfg.Class == config.ClassRingBearer {
			if conn.Side == config.SideLight {
				region = cache.RingBearer.TrueRegion
			} else {
				region = ""
			}
		}
		units = append(units, map[string]interface{}{
			"id":            u.ID,
			"class":         cfg.Class,
			"side":          cfg.Side,
			"currentRegion": region,
			"strength":      u.Strength,
			"status":        string(u.Status),
			"cooldown":      u.Cooldown,
		})
	}

	regions := make([]map[string]interface{}, 0, len(cache.Regions))
	for _, r := range cache.Regions {
		regions = append(regions, map[string]interface{}{
			"id":          r.ID,
			"control":     string(r.Control),
			"threatLevel": r.ThreatLevel,
			"fortified":   r.Fortified,
		})
	}

	paths := make([]map[string]interface{}, 0, len(cache.Paths))
	for _, p := range cache.Paths {
		paths = append(paths, map[string]interface{}{
			"id":                p.ID,
			"status":            string(p.Status),
			"surveillanceLevel": p.SurveillanceLevel,
			"blockedBy":         p.BlockedBy,
		})
	}

	payload := map[string]interface{}{
		"type":       "WorldStateSnapshot",
		"turn":       cache.Turn,
		"activeSide": s.getActiveSide(),
		"units":      units,
		"regions":    regions,
		"paths":      paths,
	}
	data, _ := json.Marshal(payload)

	select {
	case conn.WriteCh <- data:
	default:
	}
}

// pushTurnEventsToClients sends individual events after turn processing.
func (s *Server) pushTurnEventsToClients(clients map[string]SSEConnection, events *game.TurnEvents, cache state.WorldStateCache) {
	sendAll := func(payload map[string]interface{}) {
		data, _ := json.Marshal(payload)
		for _, conn := range clients {
			select {
			case conn.WriteCh <- data:
			default:
			}
		}
	}

	sendLight := func(payload map[string]interface{}) {
		data, _ := json.Marshal(payload)
		for _, conn := range clients {
			if conn.Side == config.SideLight {
				select {
				case conn.WriteCh <- data:
				default:
				}
			}
		}
	}

	sendDark := func(payload map[string]interface{}) {
		data, _ := json.Marshal(payload)
		for _, conn := range clients {
			if conn.Side == config.SideDark {
				select {
				case conn.WriteCh <- data:
				default:
				}
			}
		}
	}

	for _, e := range events.UnitMoved {
		sendAll(map[string]interface{}{"type": "UnitMoved", "unitId": e.UnitID, "from": e.From, "to": e.To, "turn": e.Turn})
	}
	for _, e := range events.PathStatusChanged {
		sendAll(map[string]interface{}{"type": "PathStatusChanged", "pathId": e.PathID, "newStatus": e.NewStatus, "turn": e.Turn})
	}
	for _, e := range events.PathCorrupted {
		sendAll(map[string]interface{}{"type": "PathCorrupted", "pathId": e.PathID, "turn": e.Turn})
	}
	for _, e := range events.BattleResolved {
		sendAll(map[string]interface{}{"type": "BattleResolved", "regionId": e.RegionID, "attackerWon": e.AttackerWon, "turn": e.Turn})
	}
	for _, e := range events.RouteBlocked {
		sendAll(map[string]interface{}{"type": "RouteBlocked", "unitId": e.UnitID, "pathId": e.PathID, "turn": e.Turn})
	}
	for _, e := range events.MaiaAbilityUsed {
		sendAll(map[string]interface{}{"type": "MaiaAbilityUsed", "unitId": e.UnitID, "pathId": e.PathID, "eventType": e.EventType, "turn": e.Turn})
	}
	if events.RingBearerMoved != nil {
		// Light Side only
		sendLight(map[string]interface{}{"type": "RingBearerMoved", "trueRegion": events.RingBearerMoved.TrueRegion, "turn": events.RingBearerMoved.Turn})
	}
	for _, e := range events.RingBearerDetected {
		sendDark(map[string]interface{}{"type": "RingBearerDetected", "regionId": e.RegionID, "turn": e.Turn})
	}
	for _, e := range events.RingBearerSpotted {
		sendDark(map[string]interface{}{"type": "RingBearerSpotted", "pathId": e.PathID, "turn": e.Turn})
	}
	if events.GameOver != nil {
		sendAll(map[string]interface{}{"type": "GameOver", "winner": events.GameOver.Winner, "cause": events.GameOver.Cause, "turn": events.GameOver.Turn})
	}
}

// fanOutToSide sends a pre-routed event (already filtered by EventRouter) to all
// clients of the given side. No topic-based filtering needed here — EventRouter
// guarantees that lightSideSSECh never carries ring.detection and darkSideSSECh
// never carries ring.position or un-stripped broadcast payloads.
func (s *Server) fanOutToSide(event router.Event, clients map[string]SSEConnection, side string) {
	for _, conn := range clients {
		if conn.Side != side {
			continue
		}
		select {
		case conn.WriteCh <- event.Payload:
		default:
		}
	}
}

func (s *Server) runAnalysis(req AnalysisRequest) {
	cache := s.cache.Get()
	ctx := context.Background()
	jobs := buildCanonicalRouteJobs()
	switch req.Type {
	case "routes":
		result := s.routePipeline.Run(ctx, jobs, cache)
		req.ReplyCh <- marshalJSON(result)
	case "intercept":
		iJobs := pipeline.BuildInterceptJobs(cache, jobs)
		result := s.interceptPipeline.Run(ctx, iJobs, cache)
		req.ReplyCh <- marshalJSON(result)
	}
}

// darkPlayerID returns the player ID of the first known Dark Side player, or "dark" as fallback.
func (s *Server) darkPlayerID() string {
	s.playerSidesMu.RLock()
	defer s.playerSidesMu.RUnlock()
	for pid, side := range s.playerSides {
		if side == config.SideDark {
			return pid
		}
	}
	return "dark" // fallback before any player connects
}

// publishTurnState emits the current turn number to game.session synchronously.
// Topology1 must see the new turn before the next order arrives, so this runs
// on the main goroutine rather than in the async publishEvents goroutine.
func (s *Server) publishTurnState(turn int) {
	data, _ := json.Marshal(map[string]interface{}{"currentTurn": turn})
	if err := s.producer.ProduceSync("game.session", "turn-state", data); err != nil {
		log.Printf("[publish] turn-state: %v", err)
	}
}

func (s *Server) publishEvents(events *game.TurnEvents, turn int, darkPlayerID string) {
	pub := func(topic, key string, v interface{}) {
		data, _ := json.Marshal(v)
		if err := s.producer.Produce(topic, key, data); err != nil {
			log.Printf("[publish] %s: %v", topic, err)
		}
	}

	// Snapshot of current cache used to enrich events with side/class/cooldown/endpoints.
	snap := s.cache.Get()

	// Unit events — also keep Topology1 UnitKTable current (cooldowns, regions, class, side).
	for _, e := range events.UnitMoved {
		u := snap.Units[e.UnitID]
		ucfg := snap.UnitConfigs[e.UnitID]
		pub("game.events.unit", e.UnitID, map[string]interface{}{
			"id":       e.UnitID,
			"side":     ucfg.Side,
			"class":    ucfg.Class,
			"region":   u.Region,
			"status":   string(u.Status),
			"cooldown": u.Cooldown,
		})
	}

	for _, e := range events.PathStatusChanged {
		p := snap.Paths[e.PathID]
		pcfg := s.cfg.PathsByID[e.PathID]
		pub("game.events.path", e.PathID, map[string]interface{}{
			"id":                e.PathID,
			"status":            string(p.Status),
			"surveillanceLevel": p.SurveillanceLevel,
			"blockedBy":         p.BlockedBy,
			"from":              pcfg.From,
			"to":                pcfg.To,
		})
	}
	for _, e := range events.PathCorrupted { pub("game.events.path", e.PathID, e) }
	for _, e := range events.BattleResolved {
		pub("game.events.region", e.RegionID, e)
	}
	for _, e := range events.RegionControlChanged {
		pub("game.events.region", e.RegionID, e)
	}
	// Publish updated unit states after battle so UnitKTable reflects new cooldowns/strengths.
	for uid, u := range snap.Units {
		ucfg := snap.UnitConfigs[uid]
		pub("game.events.unit", uid, map[string]interface{}{
			"id":       uid,
			"side":     ucfg.Side,
			"class":    ucfg.Class,
			"region":   u.Region,
			"status":   string(u.Status),
			"cooldown": u.Cooldown,
		})
	}
	if events.RingBearerMoved != nil            { pub("game.ring.position", "rb",          events.RingBearerMoved) }
	for _, e := range events.RingBearerDetected { pub("game.ring.detection", darkPlayerID, e) }

	// Publish WorldStateSnapshot to game.broadcast (spec §9).
	// EventRouter strips RB region before routing to Dark Side SSE.
	s.publishWorldStateSnapshot(snap, turn)

	if events.GameOver != nil {
		data, _ := json.Marshal(events.GameOver)
		if err := s.producer.ProduceSync("game.broadcast", "game-over", data); err != nil {
			log.Printf("[publish] GameOver: %v", err)
		}
	}
}

// publishWorldStateSnapshot sends the full WorldStateSnapshot to game.broadcast.
// The EventRouter handles stripping the RB region for Dark Side consumers.
func (s *Server) publishWorldStateSnapshot(snap state.WorldStateCache, turn int) {
	units := make([]map[string]interface{}, 0, len(snap.Units))
	for _, u := range snap.Units {
		cfg := snap.UnitConfigs[u.ID]
		units = append(units, map[string]interface{}{
			"id":            u.ID,
			"class":         cfg.Class,
			"side":          cfg.Side,
			"currentRegion": snap.RingBearer.TrueRegion, // full region — EventRouter strips for DS
			"strength":      u.Strength,
			"status":        string(u.Status),
			"cooldown":      u.Cooldown,
		})
		// Overwrite for non-RB units: use their actual region.
		if cfg.Class != config.ClassRingBearer {
			units[len(units)-1]["currentRegion"] = u.Region
		}
	}

	regions := make([]map[string]interface{}, 0, len(snap.Regions))
	for _, r := range snap.Regions {
		regions = append(regions, map[string]interface{}{
			"id":           r.ID,
			"control":      string(r.Control),
			"threatLevel":  r.ThreatLevel,
			"fortified":    r.Fortified,
			"unitsPresent": r.UnitsPresent,
		})
	}

	paths := make([]map[string]interface{}, 0, len(snap.Paths))
	for _, p := range snap.Paths {
		paths = append(paths, map[string]interface{}{
			"id":                p.ID,
			"status":            string(p.Status),
			"surveillanceLevel": p.SurveillanceLevel,
			"blockedBy":         p.BlockedBy,
		})
	}

	payload := map[string]interface{}{
		"type":    "WorldStateSnapshot",
		"turn":    turn,
		"units":   units,
		"regions": regions,
		"paths":   paths,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[publish] WorldStateSnapshot marshal: %v", err)
		return
	}
	if err := s.producer.Produce("game.broadcast", "world-snapshot", data); err != nil {
		log.Printf("[publish] WorldStateSnapshot: %v", err)
	}
}

func marshalJSON(v interface{}) []byte { b, _ := json.Marshal(v); return b }

// publishPlayerSession writes a player→side mapping to game.session so that
// Topology1 can enforce Rule 2 (NOT_YOUR_UNIT).
func (s *Server) publishPlayerSession(playerID, side string) {
	data, _ := json.Marshal(map[string]string{"playerId": playerID, "side": side})
	if err := s.producer.Produce("game.session", playerID, data); err != nil {
		log.Printf("[session] publish player session: %v", err)
	}
}

// publishStateSnapshot persists the full game state to game.session (key="world-state").
// game.session is log-compacted so only the latest snapshot is retained.
// On startup, recoverState() reads this record to rebuild the cache after a crash.
func (s *Server) publishStateSnapshot(cache state.WorldStateCache) {
	// Inline serialisation to avoid importing the kafka package here.
	type persistedState struct {
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
	ps := persistedState{
		Turn:            cache.Turn,
		MaxTurns:        cache.MaxTurns,
		HiddenUntilTurn: cache.HiddenUntilTurn,
		Units:           cache.Units,
		Regions:         cache.Regions,
		Paths:           cache.Paths,
		RingBearer:      cache.RingBearer,
		GameOver:        cache.GameOver,
		Winner:          cache.Winner,
		LightView:       cache.LightView,
	}
	data, err := json.Marshal(ps)
	if err != nil {
		log.Printf("[session] marshal snapshot: %v", err)
		return
	}
	if err := s.producer.Produce("game.session", "world-state", data); err != nil {
		log.Printf("[session] publish snapshot: %v", err)
	}
}

// destroyRingConditionMet returns true if a DESTROY_RING order is queued and
// the win conditions are satisfied in the current cache: ring bearer is at the
// destruction site and no Dark unit is present there.
func (s *Server) destroyRingConditionMet() bool {
	s.kafkaValidatedMu.Lock()
	hasDestroyRing := false
	for _, raw := range s.kafkaValidatedOrders {
		var base game.BaseOrder
		if json.Unmarshal(raw, &base) == nil && base.OrderType == game.OrderDestroyRing {
			hasDestroyRing = true
			break
		}
	}
	s.kafkaValidatedMu.Unlock()
	if !hasDestroyRing {
		return false
	}
	cache := s.cache.Get()
	site := cache.RingDestructionSiteID
	if site == "" || cache.RingBearer.TrueRegion != site {
		return false
	}
	for _, u := range cache.Units {
		ucfg := cache.UnitConfigs[u.ID]
		if ucfg.Side == config.SideDark && u.Region == site && u.Status == state.StatusActive {
			return false
		}
	}
	return true
}

// getActiveSide returns the side whose turn it currently is.
func (s *Server) getActiveSide() string {
	s.activeSideMu.RLock()
	defer s.activeSideMu.RUnlock()
	return s.activeSide
}

// broadcastPhaseChange sends PhaseChanged immediately so the browser reacts without delay,
// then pushes a full WorldStateSnapshot so unit positions are up-to-date.
func (s *Server) broadcastPhaseChange(clients map[string]SSEConnection, side string) {
	// 1. Signal the phase change first — browser can react immediately
	data, _ := json.Marshal(map[string]interface{}{
		"type":       "PhaseChanged",
		"activeSide": side,
	})
	for _, conn := range clients {
		select {
		case conn.WriteCh <- data:
		default:
		}
	}
	// 2. Then push fresh state so unit positions are current
	for _, conn := range clients {
		s.pushStateToClient(conn)
	}
}

func buildCanonicalRouteJobs() []pipeline.RouteJob {
	return []pipeline.RouteJob{
		{RouteID:"route-1-fellowship",       StartRegion:"the-shire", PathIDs:[]string{"shire-to-bree","bree-to-weathertop","weathertop-to-rivendell","rivendell-to-moria","moria-to-lothlorien","lothlorien-to-emyn-muil","emyn-muil-to-ithilien","ithilien-to-cirith-ungol","cirith-ungol-to-mount-doom"}},
		{RouteID:"route-2-northern-bypass",  StartRegion:"the-shire", PathIDs:[]string{"shire-to-bree","bree-to-rivendell","rivendell-to-lothlorien","lothlorien-to-emyn-muil","emyn-muil-to-dead-marshes","dead-marshes-to-ithilien","ithilien-to-cirith-ungol","cirith-ungol-to-mount-doom"}},
		{RouteID:"route-3-dark-route",       StartRegion:"the-shire", PathIDs:[]string{"shire-to-bree","bree-to-rivendell","rivendell-to-lothlorien","lothlorien-to-emyn-muil","emyn-muil-to-dead-marshes","dead-marshes-to-mordor","mordor-to-mount-doom"}},
		{RouteID:"route-4-southern-corridor",StartRegion:"the-shire", PathIDs:[]string{"shire-to-tharbad","tharbad-to-fords-of-isen","fords-of-isen-to-edoras","edoras-to-minas-tirith","minas-tirith-to-osgiliath","osgiliath-to-minas-morgul","minas-morgul-to-cirith-ungol","cirith-ungol-to-mount-doom"}},
	}
}
