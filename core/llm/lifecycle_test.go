package llm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestFindFreePortFallback(t *testing.T) {
	// Occupy a port, then ask findFreePort to prefer it — it must fall back.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	busy := l.Addr().(*net.TCPAddr).Port

	got, err := findFreePort(busy)
	if err != nil {
		t.Fatal(err)
	}
	if got == busy {
		t.Fatalf("expected fallback away from busy port %d", busy)
	}
	// The returned port must be bindable.
	l2, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", got))
	if err != nil {
		t.Fatalf("returned port %d not bindable: %v", got, err)
	}
	l2.Close()
}

func TestPortFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "inference.port")
	if err := writePortFile(path, 11923); err != nil {
		t.Fatal(err)
	}
	got, err := readPortFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 11923 {
		t.Fatalf("got %d, want 11923", got)
	}
}

func TestWaitHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()
	if err := waitHealthy(context.Background(), srv.URL, 3*time.Second); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
}

func TestWaitHealthyTimesOut(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable) // never ready
	}))
	defer srv.Close()
	if err := waitHealthy(context.Background(), srv.URL, 600*time.Millisecond); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestEmbeddedAssetsMissing(t *testing.T) {
	// With no bundled assets and no env overrides, Health must fail with guidance.
	t.Setenv("SAHAYAK_LLAMA_SERVER", "")
	t.Setenv("SAHAYAK_MODEL_PATH", "")
	e := NewEmbedded("")
	err := e.Health(context.Background())
	if err == nil {
		t.Fatal("expected an error when assets are missing")
	}
}

// Embedded must satisfy Provider.
var _ Provider = (*Embedded)(nil)
