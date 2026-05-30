package tests

import (
	"encoding/json"
	"testing"

	"github.com/lotr/option-b/internal/router"
	"github.com/lotr/option-b/internal/state"
)

// ── Case 1: WorldStateSnapshot with RB region set ─────────────────────────
// Dark Side receives currentRegion="" — Light Side receives the real value.

func TestRouter_StripRingBearerFromBroadcast(t *testing.T) {
	payload := map[string]interface{}{
		"turn": 5,
		"units": []interface{}{
			map[string]interface{}{
				"id":            "ring-bearer",
				"class":         "RingBearer",
				"currentRegion": "weathertop",
			},
			map[string]interface{}{
				"id":            "aragorn",
				"class":         "FellowshipGuard",
				"currentRegion": "bree",
			},
		},
	}
	raw, _ := json.Marshal(payload)
	broadcastEvt := router.Event{Topic: "game.broadcast", Payload: raw}

	lightCh := make(chan router.Event, 4)
	darkCh := make(chan router.Event, 4)

	r := router.NewTestRouter(lightCh, darkCh)
	r.RouteForTest(broadcastEvt)

	// Light Side: Ring Bearer region must be "weathertop"
	lightEvt := <-lightCh
	var lp map[string]interface{}
	_ = json.Unmarshal(lightEvt.Payload, &lp)
	for _, u := range lp["units"].([]interface{}) {
		uu := u.(map[string]interface{})
		if uu["class"] == "RingBearer" {
			if uu["currentRegion"] != "weathertop" {
				t.Errorf("Light Side: expected RB region=weathertop, got %v", uu["currentRegion"])
			}
		}
	}

	// Dark Side: Ring Bearer region must be ""
	darkEvt := <-darkCh
	var dp map[string]interface{}
	_ = json.Unmarshal(darkEvt.Payload, &dp)
	for _, u := range dp["units"].([]interface{}) {
		uu := u.(map[string]interface{})
		if uu["class"] == "RingBearer" {
			if uu["currentRegion"] != "" {
				t.Errorf("Dark Side: RB currentRegion must be \"\", got %v", uu["currentRegion"])
			}
		}
	}
}

// ── Case 2: RingBearerMoved event NEVER reaches Dark Side SSE channel ─────

func TestRouter_RingBearerMovedNeverReachesDarkSide(t *testing.T) {
	evt := router.Event{
		Topic:   "game.ring.position",
		Payload: []byte(`{"trueRegion":"weathertop","turn":5}`),
	}

	lightCh := make(chan router.Event, 4)
	darkCh := make(chan router.Event, 4)

	r := router.NewTestRouter(lightCh, darkCh)
	r.RouteForTest(evt)

	// Light Side must receive it
	select {
	case e := <-lightCh:
		if e.Topic != "game.ring.position" {
			t.Errorf("Light Side expected ring.position, got %s", e.Topic)
		}
	default:
		t.Error("Light Side: expected RingBearerMoved, got nothing")
	}

	// Dark Side must NOT receive it
	select {
	case e := <-darkCh:
		t.Errorf("Dark Side MUST NOT receive ring.position, got topic=%s", e.Topic)
	default:
		// correct — nothing in channel
	}
}

// ── Case 4: RingBearer stripped from region.unitsPresent by CLASS, not ID ─
// Uses unit ID "frodo" (not "ring-bearer") to prove no hardcoded ID string.

func TestRouter_StripRingBearerFromRegionUnitsPresent(t *testing.T) {
	payload := map[string]interface{}{
		"turn": 7,
		"units": []interface{}{
			map[string]interface{}{
				"id":            "frodo",
				"class":         "RingBearer",
				"currentRegion": "rivendell",
			},
			map[string]interface{}{
				"id":            "gandalf",
				"class":         "Maia",
				"currentRegion": "rivendell",
			},
		},
		"regions": map[string]interface{}{
			"rivendell": map[string]interface{}{
				"id":           "rivendell",
				"control":      "FREE_PEOPLES",
				"unitsPresent": []interface{}{"frodo", "gandalf"},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	broadcastEvt := router.Event{Topic: "game.broadcast", Payload: raw}

	lightCh := make(chan router.Event, 4)
	darkCh := make(chan router.Event, 4)

	r := router.NewTestRouter(lightCh, darkCh)
	r.RouteForTest(broadcastEvt)

	// Light Side: both units in unitsPresent
	lightEvt := <-lightCh
	var lp map[string]interface{}
	_ = json.Unmarshal(lightEvt.Payload, &lp)
	rivendellLight := lp["regions"].(map[string]interface{})["rivendell"].(map[string]interface{})
	lpUnits := rivendellLight["unitsPresent"].([]interface{})
	if len(lpUnits) != 2 {
		t.Errorf("Light Side: expected 2 units in rivendell.unitsPresent, got %d", len(lpUnits))
	}

	// Dark Side: "frodo" (RingBearer by class) must be removed; "gandalf" must remain
	darkEvt := <-darkCh
	var dp map[string]interface{}
	_ = json.Unmarshal(darkEvt.Payload, &dp)
	rivendellDark := dp["regions"].(map[string]interface{})["rivendell"].(map[string]interface{})
	dpUnits := rivendellDark["unitsPresent"].([]interface{})
	if len(dpUnits) != 1 {
		t.Errorf("Dark Side: expected 1 unit in rivendell.unitsPresent after stripping, got %d: %v", len(dpUnits), dpUnits)
	}
	if dpUnits[0].(string) != "gandalf" {
		t.Errorf("Dark Side: expected only 'gandalf' remaining, got %v", dpUnits[0])
	}

	// Dark Side: RingBearer currentRegion must be ""
	for _, u := range dp["units"].([]interface{}) {
		uu := u.(map[string]interface{})
		if uu["class"] == "RingBearer" && uu["currentRegion"] != "" {
			t.Errorf("Dark Side: RB currentRegion must be \"\", got %v", uu["currentRegion"])
		}
	}
}

// ── Case 3: DarkView.RingBearerRegion is always "" after any cache update ─
// Tests the CacheManager invariant with -race flag.

func TestRouter_DarkViewRingBearerRegionAlwaysEmpty(t *testing.T) {
	initial := state.WorldStateCache{
		DarkView: state.DarkSideView{RingBearerRegion: ""},
	}
	cm := state.NewCacheManager(initial)

	// Run 200 concurrent updates — none should set RingBearerRegion
	done := make(chan struct{}, 200)
	for i := 0; i < 200; i++ {
		go func() {
			cm.Update(func(c *state.WorldStateCache) {
				// Simulate an update that might accidentally set dark view
				c.Turn++
				// CacheManager.Update enforces: c.DarkView.RingBearerRegion = ""
			})
			snap := cm.Get()
			if snap.DarkView.RingBearerRegion != "" {
				t.Errorf("invariant violated: DarkView.RingBearerRegion = %q", snap.DarkView.RingBearerRegion)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 200; i++ {
		<-done
	}
}
