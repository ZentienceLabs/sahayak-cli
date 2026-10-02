package cartridge

import (
	"context"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
)

func newTestIndex(t *testing.T) *Index {
	t.Helper()
	carts, err := Builtins()
	if err != nil {
		t.Fatalf("Builtins: %v", err)
	}
	ix, err := NewIndex(context.Background(), embed.NewHashEmbedder(256), carts)
	if err != nil {
		t.Fatalf("NewIndex: %v", err)
	}
	return ix
}

func TestIndexRoutes(t *testing.T) {
	ix := newTestIndex(t)
	cases := []struct {
		req       string
		cartridge string
		intent    string
	}{
		{"list the configmaps for acme-web", "k8s", "list"},
		{"is there a config flag for telemetry", "k8s", "searchcfg"},
		{"what image is acme-web running", "k8s", "image"},
		{"is acme-web rolled out", "k8s", "rollout"},
		// systemd, the second tool — proves cross-cartridge PEER routing (no primary).
		{"restart the nginx service", "systemd", "restart"},
		{"status of the sshd service", "systemd", "status"},
		{"show the journal for the docker service", "systemd", "logs"},
	}
	for _, c := range cases {
		h, ok, err := ix.Route(context.Background(), c.req)
		if err != nil {
			t.Fatalf("Route(%q): %v", c.req, err)
		}
		if !ok {
			t.Errorf("Route(%q) did not fire", c.req)
			continue
		}
		if h.Cartridge.Name != c.cartridge || h.Intent != c.intent {
			t.Errorf("Route(%q) = %s/%s @ %.2f, want %s/%s", c.req, h.Cartridge.Name, h.Intent, h.Score, c.cartridge, c.intent)
		}
	}
}

func TestIndexDeclinesOffTopic(t *testing.T) {
	ix := newTestIndex(t)
	if _, ok, _ := ix.Route(context.Background(), "what is the capital of france"); ok {
		t.Error("off-topic request should not route")
	}
}

func TestIndexDeclinesWhenSlotMissing(t *testing.T) {
	ix := newTestIndex(t)
	// Matches "list" by meaning but has no resource to ground → decline.
	if _, ok, _ := ix.Route(context.Background(), "show me everything for acme-web"); ok {
		t.Error("should decline when the resource slot can't ground")
	}
}
