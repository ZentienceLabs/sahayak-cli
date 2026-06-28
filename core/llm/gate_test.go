package llm

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ctlProvider lets a test observe and control each Chat: every call announces its
// label on `enter` and then blocks until the test sends on `proceed`.
type ctlProvider struct {
	label   string
	enter   chan string
	proceed chan struct{}
}

func (c *ctlProvider) Name() string                     { return c.label }
func (c *ctlProvider) Health(ctx context.Context) error { return nil }
func (c *ctlProvider) Chat(ctx context.Context, _ ChatRequest) (ChatResponse, error) {
	c.enter <- c.label
	select {
	case <-c.proceed:
		return ChatResponse{Content: c.label}, nil
	case <-ctx.Done():
		return ChatResponse{}, ctx.Err()
	}
}

// TestGateMutualExclusion: only one inference runs at a time even under contention.
func TestGateMutualExclusion(t *testing.T) {
	g := NewGate()
	cp := &ctlProvider{label: "x", enter: make(chan string, 4), proceed: make(chan struct{})}
	fg := g.Foreground(cp)

	var running, maxRunning int32
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = fg.Chat(context.Background(), ChatRequest{}) }()
	}

	for i := 0; i < 3; i++ {
		<-cp.enter
		if cur := atomic.AddInt32(&running, 1); cur > atomic.LoadInt32(&maxRunning) {
			atomic.StoreInt32(&maxRunning, cur)
		}
		time.Sleep(3 * time.Millisecond)
		atomic.AddInt32(&running, -1)
		cp.proceed <- struct{}{}
	}
	wg.Wait()
	if maxRunning != 1 {
		t.Fatalf("expected at most 1 concurrent inference, saw %d", maxRunning)
	}
}

// TestGateForegroundPreempts: a background call holds the gate; while it holds, a
// background #2 and a foreground both queue. When the holder releases, the
// foreground must run before the queued background.
func TestGateForegroundPreempts(t *testing.T) {
	g := NewGate()
	hold := &ctlProvider{label: "bg1", enter: make(chan string, 1), proceed: make(chan struct{})}
	other := &ctlProvider{label: "rest", enter: make(chan string, 2), proceed: make(chan struct{})}

	// bg1 acquires and holds the gate.
	go func() { _, _ = g.Background(hold).Chat(context.Background(), ChatRequest{}) }()
	<-hold.enter // bg1 is now holding

	order := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	// Background #2 queues first.
	go func() {
		defer wg.Done()
		_, _ = g.Background(other).Chat(context.Background(), ChatRequest{})
		order <- "bg2"
	}()
	// Ensure bg2 is parked waiting on the gate before the foreground arrives.
	for !g.hasBackgroundWaiter() {
		time.Sleep(time.Millisecond)
	}
	// Foreground arrives and queues.
	go func() {
		defer wg.Done()
		_, _ = g.Foreground(other).Chat(context.Background(), ChatRequest{})
		order <- "fg"
	}()
	for !g.ForegroundWaiting() {
		time.Sleep(time.Millisecond)
	}

	// Release the holder; foreground should win the next slot.
	hold.proceed <- struct{}{}
	<-other.enter               // whichever runs next has entered Chat
	other.proceed <- struct{}{} // let it finish
	first := <-order
	if first != "fg" {
		t.Fatalf("expected foreground to preempt queued background, got %q first", first)
	}
	// Drain the remaining one.
	<-other.enter
	other.proceed <- struct{}{}
	wg.Wait()
}

// TestGateContextCancel: a caller blocked waiting on the gate returns ctx.Err.
func TestGateContextCancel(t *testing.T) {
	g := NewGate()
	hold := &ctlProvider{label: "hold", enter: make(chan string, 1), proceed: make(chan struct{})}
	go func() { _, _ = g.Foreground(hold).Chat(context.Background(), ChatRequest{}) }()
	<-hold.enter // gate occupied

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := g.Background(hold).Chat(ctx, ChatRequest{})
		errc <- err
	}()
	for !g.hasBackgroundWaiter() {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-errc:
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked caller did not unblock on cancel")
	}
	hold.proceed <- struct{}{} // release the holder
}
