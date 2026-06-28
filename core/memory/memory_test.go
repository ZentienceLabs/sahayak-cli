package memory

import (
	"context"
	"testing"

	"github.com/ZentienceLabs/sahayak-cli/core/embed"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(t.TempDir(), embed.NewHashEmbedder(256))
}

func TestRememberRecall(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	must(t, s.Remember(ctx, "history", "restarted nginx after editing the server block"))
	must(t, s.Remember(ctx, "history", "scaled the payments deployment to 5 replicas"))
	must(t, s.Remember(ctx, "notes", "prod database is on host db-01 port 5432"))

	hits, err := s.Recall(ctx, "", "how do I restart nginx", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !contains(hits[0].Text, "nginx") {
		t.Fatalf("expected nginx memory, got %+v", hits)
	}
}

func TestRecallNamespaceFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	must(t, s.Remember(ctx, "notes", "prod database host db-01"))
	must(t, s.Remember(ctx, "history", "did something unrelated"))

	hits, err := s.Recall(ctx, "notes", "database", 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.Namespace != "notes" {
			t.Fatalf("namespace filter leaked: %+v", h)
		}
	}
}

func TestPersistenceAcrossStores(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s1 := NewStore(dir, embed.NewHashEmbedder(256))
	must(t, s1.Remember(ctx, "notes", "remember the alamo"))

	// A fresh store over the same dir must see the persisted memory.
	s2 := NewStore(dir, embed.NewHashEmbedder(256))
	all, err := s2.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].Text != "remember the alamo" {
		t.Fatalf("persistence failed: %+v", all)
	}
}

func TestForget(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	must(t, s.Remember(ctx, "notes", "keep this"))
	must(t, s.Remember(ctx, "notes", "delete this secret note"))
	n, err := s.Forget("secret")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 removed, got %d", n)
	}
	all, _ := s.All()
	if len(all) != 1 || all[0].Text != "keep this" {
		t.Fatalf("forget removed wrong entries: %+v", all)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	s := newTestStore(t)
	type state struct {
		Step int
		Note string
	}
	in := state{Step: 2, Note: "awaiting approval"}
	if err := s.SaveCheckpoint("session-1", in); err != nil {
		t.Fatal(err)
	}
	var out state
	if err := s.LoadCheckpoint("session-1", &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("checkpoint mismatch: %+v vs %+v", out, in)
	}
	if err := s.ClearCheckpoint("session-1"); err != nil {
		t.Fatal(err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
