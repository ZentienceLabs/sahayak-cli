package llm

import (
	"context"
	"sync"
)

// Gate serializes access to a single, scarce inference backend and gives the
// FOREGROUND (the operator's in-flight request) strict priority over BACKGROUND
// work (the curator building knowledge). On a sovereign CPU box the bottleneck is
// memory bandwidth — one global pipe — so two inferences at once slow each other
// down even with idle cores. The honest model is therefore: exactly ONE inference
// runs at a time, and the moment a foreground request wants the model, no new
// background request may start; the foreground takes the next slot.
//
// Preemption granularity is one Chat call. We cannot interrupt a single in-flight
// completion on a remote server, so background work must be structured as many
// small calls; between them, a waiting foreground always wins. Background callers
// can also poll ForegroundWaiting to bail out of a unit of work voluntarily.
type Gate struct {
	mu        sync.Mutex
	cond      *sync.Cond
	held      bool // an inference is currently in flight
	fgPending int  // foreground callers waiting to acquire (not yet holding)
	bgPending int  // background callers waiting to acquire (not yet holding)
}

// NewGate returns an unheld gate.
func NewGate() *Gate {
	g := &Gate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// acquire blocks until this caller may run its inference. Foreground callers wait
// only for the gate to be free; background callers additionally wait until no
// foreground caller is pending, so a foreground request can never sit behind a
// freshly-started background one. Honors ctx cancellation.
func (g *Gate) acquire(ctx context.Context, foreground bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if foreground {
		g.fgPending++
	} else {
		g.bgPending++
	}
	// Wake this waiter if its context is cancelled while parked in cond.Wait.
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()

	dec := func() {
		if foreground {
			g.fgPending--
		} else {
			g.bgPending--
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			dec()
			return err
		}
		canGo := !g.held && (foreground || g.fgPending == 0)
		if canGo {
			g.held = true
			dec() // promoted from pending to holding
			return nil
		}
		g.cond.Wait()
	}
}

// release hands the gate back. A broadcast lets a waiting foreground (preferred)
// or background caller proceed.
func (g *Gate) release() {
	g.mu.Lock()
	g.held = false
	g.cond.Broadcast()
	g.mu.Unlock()
}

// ForegroundWaiting reports whether a foreground request is waiting for the model.
// A long-running background unit can poll this to yield early and let the operator
// through, instead of holding the gate for its full duration.
func (g *Gate) ForegroundWaiting() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fgPending > 0
}

// hasBackgroundWaiter reports whether a background caller is parked on the gate.
// Used by tests to remove timing races; not part of the public scheduling logic.
func (g *Gate) hasBackgroundWaiter() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.bgPending > 0
}

// Foreground wraps p so its Chat calls take strict priority on this gate. Use it
// for the operator-facing agent.
func (g *Gate) Foreground(p Provider) Provider {
	return &gatedProvider{inner: p, gate: g, foreground: true}
}

// Background wraps p so its Chat calls yield to foreground work on this gate. Use
// it for the curator and any other always-on background agent.
func (g *Gate) Background(p Provider) Provider {
	return &gatedProvider{inner: p, gate: g, foreground: false}
}

// gatedProvider is a Provider that acquires the shared gate around each Chat call.
type gatedProvider struct {
	inner      Provider
	gate       *Gate
	foreground bool
}

func (gp *gatedProvider) Name() string { return gp.inner.Name() }

// Chat acquires the gate at this provider's priority, runs the underlying Chat,
// then releases. Health and other non-inference work is intentionally not gated.
func (gp *gatedProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if err := gp.gate.acquire(ctx, gp.foreground); err != nil {
		return ChatResponse{}, err
	}
	defer gp.gate.release()
	return gp.inner.Chat(ctx, req)
}

func (gp *gatedProvider) Health(ctx context.Context) error { return gp.inner.Health(ctx) }
