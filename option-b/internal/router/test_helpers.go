package router

import "github.com/lotr/option-b/internal/state"

// NewTestRouter creates an EventRouter wired to test channels.
// The cacheUpdateCh and engineCh are dummies — tests only verify routing.
func NewTestRouter(
	lightSideSSECh chan<- Event,
	darkSideSSECh chan<- Event,
) *EventRouter {
	dummyCacheUpdateCh := make(chan func(*state.WorldStateCache), 4)
	dummyEngineCh := make(chan Event, 4)
	return &EventRouter{
		eventCh:        make(chan Event, 4),
		lightSideSSECh: lightSideSSECh,
		darkSideSSECh:  darkSideSSECh,
		cacheUpdateCh:  dummyCacheUpdateCh,
		engineCh:       dummyEngineCh,
	}
}

// RouteForTest exposes the private route method for unit tests.
func (r *EventRouter) RouteForTest(event Event) {
	r.route(event)
}
