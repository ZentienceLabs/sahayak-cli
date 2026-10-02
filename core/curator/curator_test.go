package curator

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
)

// fakeFacts is an in-memory Facts for testing the curator without disk or a model.
type fakeFacts struct {
	mu      sync.Mutex
	summary string
	length  int
	pruned  int
	saves   int
}

func (f *fakeFacts) Prune() int { f.mu.Lock(); defer f.mu.Unlock(); f.pruned++; return 0 }
func (f *fakeFacts) Len() int   { f.mu.Lock(); defer f.mu.Unlock(); return f.length }
func (f *fakeFacts) Summary() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.summary
}
func (f *fakeFacts) Save() error { f.mu.Lock(); defer f.mu.Unlock(); f.saves++; return nil }

// recordingProvider records each distill call and returns a canned note.
type recordingProvider struct {
	mu    sync.Mutex
	calls int
	reply string
	err   error
}

func (p *recordingProvider) Name() string                     { return "rec" }
func (p *recordingProvider) Health(ctx context.Context) error { return nil }
func (p *recordingProvider) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.err != nil {
		return llm.ChatResponse{}, p.err
	}
	return llm.ChatResponse{Content: p.reply}, nil
}

type recordingNotes struct {
	mu    sync.Mutex
	notes []string
	ns    []string
}

func (n *recordingNotes) Remember(ctx context.Context, namespace, text string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.ns = append(n.ns, namespace)
	n.notes = append(n.notes, text)
	return nil
}

func TestStepDeterministicMaintenanceAlwaysRuns(t *testing.T) {
	facts := &fakeFacts{}
	c := New(nil, nil, facts, nil) // no provider/notes: distillation disabled
	c.step(context.Background())
	if facts.pruned != 1 || facts.saves != 1 {
		t.Fatalf("expected prune+save each tick, got pruned=%d saves=%d", facts.pruned, facts.saves)
	}
}

func TestStepDistillsAndWritesToTopologyNamespace(t *testing.T) {
	gate := llm.NewGate()
	facts := &fakeFacts{summary: "namespaces: acme-dev", length: 1}
	prov := &recordingProvider{reply: "Cluster has the acme-dev namespace."}
	notes := &recordingNotes{}
	c := New(gate, gate.Background(prov), facts, notes)

	c.step(context.Background())
	if prov.calls != 1 {
		t.Fatalf("expected 1 distill call, got %d", prov.calls)
	}
	if len(notes.notes) != 1 || notes.ns[0] != TopologyNamespace {
		t.Fatalf("note not written to topology namespace: %+v", notes)
	}

	// Second step with unchanged summary must NOT call the model again (de-dupe).
	c.step(context.Background())
	if prov.calls != 1 {
		t.Fatalf("expected distillation to be skipped for unchanged facts, calls=%d", prov.calls)
	}
}

func TestStepYieldsWhenForegroundWaiting(t *testing.T) {
	gate := llm.NewGate()
	facts := &fakeFacts{summary: "namespaces: acme-dev", length: 1}
	prov := &recordingProvider{reply: "note"}
	notes := &recordingNotes{}
	c := New(gate, gate.Background(prov), facts, notes)

	// Simulate a foreground request occupying the gate (held) so a background
	// caller would block; the curator must detect the waiter and skip.
	hold := make(chan struct{})
	entered := make(chan struct{})
	go func() {
		fg := gate.Foreground(blockOnce{entered: entered, release: hold})
		_, _ = fg.Chat(context.Background(), llm.ChatRequest{})
	}()
	<-entered // foreground now holds the gate

	// Queue a second foreground so ForegroundWaiting() is true while the first holds.
	go func() {
		fg := gate.Foreground(blockOnce{entered: make(chan struct{}, 1), release: hold})
		_, _ = fg.Chat(context.Background(), llm.ChatRequest{})
	}()
	for !gate.ForegroundWaiting() {
	}

	c.step(context.Background())
	if prov.calls != 0 {
		t.Fatalf("curator should yield when foreground is waiting, but made %d calls", prov.calls)
	}
	close(hold) // let the foreground holders finish
}

// blockOnce holds the gate until release is closed.
type blockOnce struct {
	entered chan struct{}
	release chan struct{}
}

func (b blockOnce) Name() string                     { return "block" }
func (b blockOnce) Health(ctx context.Context) error { return nil }
func (b blockOnce) Chat(ctx context.Context, _ llm.ChatRequest) (llm.ChatResponse, error) {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return llm.ChatResponse{}, nil
}

func TestDistillErrorIsSwallowed(t *testing.T) {
	gate := llm.NewGate()
	facts := &fakeFacts{summary: "namespaces: acme-dev", length: 1}
	prov := &recordingProvider{err: errors.New("model down")}
	notes := &recordingNotes{}
	c := New(gate, gate.Background(prov), facts, notes)
	c.step(context.Background()) // must not panic or write a note
	if len(notes.notes) != 0 {
		t.Fatalf("no note should be written on distill error, got %v", notes.notes)
	}
}
