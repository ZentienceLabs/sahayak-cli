package envfacts

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCacheableDecider(t *testing.T) {
	cases := []struct {
		kind Kind
		name string
		want bool
	}{
		{KindNamespace, "acme-dev", true},
		{KindDeployment, "acme-worker", true},
		{KindNode, "aks-nodepool1-2", true},
		// volatile kind: never cacheable
		{"pod", "acme-worker-7d9f8b6c5-x2kqp", false},
		// stable kind but instance-shaped name: refused by the pod-name guard
		{KindDeployment, "acme-worker-7d9f8b6c5-x2kqp", false},
		{KindNamespace, "", false},
	}
	for _, c := range cases {
		if got := Cacheable(c.kind, c.name); got != c.want {
			t.Errorf("Cacheable(%s,%q)=%v want %v", c.kind, c.name, got, c.want)
		}
	}
}

func TestExtractNamespacesAndDeployments(t *testing.T) {
	store := newTestStore(t)

	nsOut := `NAME              STATUS   AGE
acme-dev       Active   30d
kube-system       Active   90d`
	if n := ExtractFromKubectl([]string{"get", "namespaces"}, nsOut, store); n != 2 {
		t.Fatalf("expected 2 namespaces learned, got %d", n)
	}

	deployOut := `NAME             READY   UP-TO-DATE   AVAILABLE   AGE
acme-worker   1/1     1            1           5d
acme-api      2/2     2            2           5d`
	if n := ExtractFromKubectl([]string{"get", "deployments", "-n", "acme-dev"}, deployOut, store); n != 2 {
		t.Fatalf("expected 2 deployments learned, got %d", n)
	}

	// Deployments must be scoped to their namespace.
	deps := store.Fresh(KindDeployment, "acme-dev")
	if len(deps) != 2 {
		t.Fatalf("expected 2 deployments in acme-dev, got %d", len(deps))
	}
	if got := store.Fresh(KindDeployment, "other-ns"); len(got) != 0 {
		t.Fatalf("deployments leaked into wrong namespace: %v", got)
	}
}

func TestExtractRefusesPods(t *testing.T) {
	store := newTestStore(t)
	podOut := `NAME                              READY   STATUS    RESTARTS   AGE
acme-worker-7d9f8b6c5-x2kqp    1/1     Running   0          5d
acme-api-6c4b9f7d8-abcde       1/1     Running   0          5d`
	if n := ExtractFromKubectl([]string{"get", "pods", "-n", "acme-dev"}, podOut, store); n != 0 {
		t.Fatalf("pods must never be learned, got %d", n)
	}
}

func TestExtractAllNamespaces(t *testing.T) {
	store := newTestStore(t)
	out := `NAMESPACE     NAME             READY   UP-TO-DATE   AVAILABLE   AGE
acme-dev   acme-worker   1/1     1            1           5d
kube-system   coredns          2/2     2            2           90d`
	if n := ExtractFromKubectl([]string{"get", "deploy", "-A"}, out, store); n != 2 {
		t.Fatalf("expected 2 learned with -A, got %d", n)
	}
	if got := store.Fresh(KindDeployment, "kube-system"); len(got) != 1 || got[0].Name != "coredns" {
		t.Fatalf("NAMESPACE column not parsed: %v", got)
	}
}

func TestTTLExpiryAndPrune(t *testing.T) {
	store := newTestStore(t)
	base := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return base }
	store.Learn(KindDeployment, "acme-worker", "acme-dev") // 24h TTL

	// Within TTL: visible.
	store.now = func() time.Time { return base.Add(12 * time.Hour) }
	if len(store.Fresh(KindDeployment, "acme-dev")) != 1 {
		t.Fatal("fact should be fresh within TTL")
	}
	// Past TTL: hidden, and pruned.
	store.now = func() time.Time { return base.Add(48 * time.Hour) }
	if len(store.Fresh(KindDeployment, "acme-dev")) != 0 {
		t.Fatal("fact should be stale past TTL")
	}
	if store.Prune() != 1 {
		t.Fatal("expired fact should be pruned")
	}
}

func TestInvalidateOnFailedUse(t *testing.T) {
	store := newTestStore(t)
	store.Learn(KindNamespace, "acme-dev", "")
	store.Learn(KindNamespace, "kube-system", "")
	if store.Invalidate(KindNamespace, "acme-dev") != 1 {
		t.Fatal("expected to invalidate the used-then-failed namespace")
	}
	if len(store.Fresh(KindNamespace, "")) != 1 {
		t.Fatal("only the failed namespace should be gone")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "envfacts.json")
	s1 := NewStore(path)
	s1.Learn(KindNamespace, "acme-dev", "")
	s1.Learn(KindDeployment, "acme-worker", "acme-dev")
	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}
	s2 := NewStore(path)
	if len(s2.Fresh(KindNamespace, "")) != 1 {
		t.Fatal("namespace did not survive round-trip")
	}
	if len(s2.Fresh(KindDeployment, "acme-dev")) != 1 {
		t.Fatal("deployment did not survive round-trip")
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "envfacts.json"))
}

func TestInvalidateFromError(t *testing.T) {
	store := newTestStore(t)
	store.Learn(KindNamespace, "acme-dev", "")
	store.Learn(KindDeployment, "worker", "acme-dev")

	// kubectl's classic message when a namespace is gone.
	n := store.InvalidateFromError(`Error from server (NotFound): namespaces "acme-dev" not found`)
	if n != 1 {
		t.Fatalf("expected to invalidate the missing namespace, got %d", n)
	}
	if len(store.Fresh(KindNamespace, "")) != 0 {
		t.Fatal("stale namespace should be gone after failed use")
	}
	// A deployment NotFound with the .apps suffix.
	if store.InvalidateFromError(`Error from server (NotFound): deployments.apps "worker" not found`) != 1 {
		t.Fatal("expected to invalidate the missing deployment")
	}
	// Unrelated stderr is a no-op.
	if store.InvalidateFromError(`some other error`) != 0 {
		t.Fatal("unrelated stderr must not invalidate anything")
	}
}
