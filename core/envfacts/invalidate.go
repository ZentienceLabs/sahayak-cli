package envfacts

import "regexp"

// notFoundRe matches kubectl's "<resource> \"<name>\" not found" error, which is
// emitted when a named object (often a namespace) no longer exists. We use it to
// self-invalidate a cached fact the instant a stale name is used and fails — the
// guard that lets auto-learning be safe.
var notFoundRe = regexp.MustCompile(`(?i)(namespaces?|deployments?(?:\.apps)?|services?|statefulsets?(?:\.apps)?|configmaps?|nodes?|ingress(?:es)?)\s+"([^"]+)"\s+not found`)

// errResourceKind maps the resource word in a kubectl error to a Kind.
func errResourceKind(word string) (Kind, bool) {
	switch {
	case match(word, "namespace", "namespaces"):
		return KindNamespace, true
	case hasPrefix(word, "deployment"):
		return KindDeployment, true
	case match(word, "service", "services"):
		return KindService, true
	case hasPrefix(word, "statefulset"):
		return KindStatefulSet, true
	case match(word, "configmap", "configmaps"):
		return KindConfigMap, true
	case match(word, "node", "nodes"):
		return KindNode, true
	case hasPrefix(word, "ingress"):
		return KindIngress, true
	}
	return "", false
}

// InvalidateFromError scans a kubectl stderr for a "<resource> \"name\" not found"
// signal and drops the matching cached fact. Returns the number invalidated. Safe
// to call on any stderr — it no-ops when there is no such signal.
func (s *Store) InvalidateFromError(stderr string) int {
	total := 0
	for _, m := range notFoundRe.FindAllStringSubmatch(stderr, -1) {
		if kind, ok := errResourceKind(m[1]); ok {
			total += s.Invalidate(kind, m[2])
		}
	}
	return total
}

func match(s string, opts ...string) bool {
	for _, o := range opts {
		if equalFold(s, o) {
			return true
		}
	}
	return false
}

func hasPrefix(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	return equalFold(s[:len(prefix)], prefix)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
