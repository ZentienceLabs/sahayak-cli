// Package envfacts is Sahayak's environment-topology cache: the durable, slowly-
// changing facts about the operator's world (which namespaces exist, which
// deployments live where) so the agent can skip re-discovering them every run.
//
// The decision of WHAT is worth caching is deliberately NOT left to the model. It
// is a deterministic property of the resource KIND — the same on every cluster,
// every run — so a small/unreliable model never gets a vote. Namespaces and
// deployments are stable and cached; pods, replicasets, IPs and events are
// volatile and refused. This is the same philosophy as risk classification: the
// things that must be reliable are kept away from the model.
//
// Two safeguards make caching safe (they are why we can auto-learn again after the
// earlier memory-poisoning incident): every fact carries a per-kind TTL, and a
// fact that is USED and then fails (kubectl NotFound) is invalidated immediately —
// a typo'd or deleted name cannot survive a single failed use.
package envfacts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is a coarse classification of a discovered entity, used to decide
// cacheability and TTL.
type Kind string

const (
	KindNamespace   Kind = "namespace"
	KindDeployment  Kind = "deployment"
	KindService     Kind = "service"
	KindStatefulSet Kind = "statefulset"
	KindConfigMap   Kind = "configmap"
	KindNode        Kind = "node"
	KindIngress     Kind = "ingress"
)

// ttlByKind sets how long a cached fact of each kind is trusted before it must be
// re-verified. Topology changes slowly, so these are generous; the self-
// invalidation guard handles the rare mid-TTL deletion.
var ttlByKind = map[Kind]time.Duration{
	KindNamespace:   7 * 24 * time.Hour,
	KindNode:        7 * 24 * time.Hour,
	KindDeployment:  24 * time.Hour,
	KindStatefulSet: 24 * time.Hour,
	KindService:     24 * time.Hour,
	KindConfigMap:   24 * time.Hour,
	KindIngress:     24 * time.Hour,
}

// stableKinds are the resource kinds worth caching. Anything not listed here is
// treated as volatile and never stored.
var stableKinds = map[Kind]bool{
	KindNamespace: true, KindDeployment: true, KindService: true,
	KindStatefulSet: true, KindConfigMap: true, KindNode: true, KindIngress: true,
}

// podNameRe matches the generated-pod-name shape `<name>-<replicaset-hash>-<suffix>`
// (e.g. acme-worker-7d9f8b6c5-x2kqp). A token of this shape is volatile even if
// the surrounding kind is unknown — it is an instance, not a topology entity.
var podNameRe = regexp.MustCompile(`^.+-[a-f0-9]{8,10}-[a-z0-9]{5}$`)

// Cacheable reports whether a (kind, name) pair is worth storing. This is the core
// decider the rest of the system asks. It is deterministic and model-free.
func Cacheable(kind Kind, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if !stableKinds[kind] {
		return false
	}
	// Even under a stable kind, refuse anything shaped like a generated instance
	// name (defense against mislabeled rows).
	if podNameRe.MatchString(name) {
		return false
	}
	return true
}

// Fact is one cached topology entity. Namespace scopes deployments/services to the
// namespace they were found in ("" for cluster-scoped kinds like namespace/node).
type Fact struct {
	Kind      Kind      `json:"kind"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	LearnedAt time.Time `json:"learned_at"`
	LastSeen  time.Time `json:"last_seen"`
}

func (f Fact) key() string { return string(f.Kind) + "/" + f.Namespace + "/" + f.Name }

// fresh reports whether the fact is still within its kind's TTL relative to now.
func (f Fact) fresh(now time.Time) bool {
	ttl, ok := ttlByKind[f.Kind]
	if !ok {
		return false
	}
	return now.Sub(f.LastSeen) < ttl
}

// Store is the on-disk, in-memory cache of environment facts, safe for concurrent
// use by the foreground agent and the background curator.
type Store struct {
	path string
	now  func() time.Time // injectable clock for tests

	mu      sync.Mutex
	facts   map[string]Fact
	loaded  bool
	changed bool
}

// NewStore opens (lazily) a fact store at path. If path is empty it defaults to
// ~/.sahayak/envfacts.json.
func NewStore(path string) *Store {
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".sahayak", "envfacts.json")
		} else {
			path = filepath.Join(".sahayak", "envfacts.json")
		}
	}
	return &Store{path: path, now: time.Now, facts: map[string]Fact{}}
}

func (s *Store) load() {
	if s.loaded {
		return
	}
	s.loaded = true
	b, err := os.ReadFile(s.path)
	if err != nil {
		return // missing file is fine — empty cache
	}
	var list []Fact
	if json.Unmarshal(b, &list) == nil {
		for _, f := range list {
			s.facts[f.key()] = f
		}
	}
}

// Learn records (or refreshes) a fact if it is Cacheable. Non-cacheable kinds are
// silently ignored — that refusal is the whole point. Returns true if the fact was
// stored or refreshed.
func (s *Store) Learn(kind Kind, name, namespace string) bool {
	if !Cacheable(kind, name) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now()
	f := Fact{Kind: kind, Name: name, Namespace: namespace, LearnedAt: now, LastSeen: now}
	if existing, ok := s.facts[f.key()]; ok {
		existing.LastSeen = now
		s.facts[f.key()] = existing
	} else {
		s.facts[f.key()] = f
	}
	s.changed = true
	return true
}

// Invalidate removes a fact (any namespace) by kind+name — called when a cached
// name is used and the command fails with NotFound, so stale topology can't
// survive a single failed use. Returns the number removed.
func (s *Store) Invalidate(kind Kind, name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	removed := 0
	for k, f := range s.facts {
		if f.Kind == kind && f.Name == name {
			delete(s.facts, k)
			removed++
		}
	}
	if removed > 0 {
		s.changed = true
	}
	return removed
}

// Fresh returns the non-expired facts of a given kind, optionally filtered to a
// namespace (namespace=="" means any). Results are name-sorted for stable output.
func (s *Store) Fresh(kind Kind, namespace string) []Fact {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now()
	var out []Fact
	for _, f := range s.facts {
		if f.Kind != kind || !f.fresh(now) {
			continue
		}
		if namespace != "" && f.Namespace != namespace {
			continue
		}
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Prune drops every expired fact and returns how many were removed. The curator
// calls this during idle to keep the cache honest.
func (s *Store) Prune() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now()
	removed := 0
	for k, f := range s.facts {
		if !f.fresh(now) {
			delete(s.facts, k)
			removed++
		}
	}
	if removed > 0 {
		s.changed = true
	}
	return removed
}

// Len returns the number of currently-fresh facts. Used by the curator to decide
// whether there is anything worth consolidating.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now()
	n := 0
	for _, f := range s.facts {
		if f.fresh(now) {
			n++
		}
	}
	return n
}

// Summary renders the fresh facts as a compact, grouped text block — the input the
// curator hands to the model for distillation, and a useful grounding snippet on
// its own. Returns "" when the cache is empty.
func (s *Store) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load()
	now := s.now()

	byNS := map[string][]string{}  // namespace -> "kind name"
	cluster := map[Kind][]string{} // cluster-scoped kind -> names
	for _, f := range s.facts {
		if !f.fresh(now) {
			continue
		}
		if clusterScopedKind(f.Kind) {
			cluster[f.Kind] = append(cluster[f.Kind], f.Name)
		} else {
			byNS[f.Namespace] = append(byNS[f.Namespace], string(f.Kind)+" "+f.Name)
		}
	}
	if len(byNS) == 0 && len(cluster) == 0 {
		return ""
	}

	var b strings.Builder
	for _, k := range []Kind{KindNamespace, KindNode} {
		if names := cluster[k]; len(names) > 0 {
			sort.Strings(names)
			b.WriteString(string(k) + "s: " + strings.Join(names, ", ") + "\n")
		}
	}
	nss := make([]string, 0, len(byNS))
	for ns := range byNS {
		nss = append(nss, ns)
	}
	sort.Strings(nss)
	for _, ns := range nss {
		items := byNS[ns]
		sort.Strings(items)
		label := ns
		if label == "" {
			label = "(cluster)"
		}
		b.WriteString("in " + label + ": " + strings.Join(items, ", ") + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// Save persists the cache to disk if anything changed since the last save. Atomic
// via temp-file rename.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.changed {
		return nil
	}
	list := make([]Fact, 0, len(s.facts))
	for _, f := range s.facts {
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].key() < list[j].key() })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.changed = false
	return nil
}
