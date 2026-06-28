package envfacts

import "strings"

// resourceKind maps a kubectl resource word (and its common aliases) to a stable
// Kind. Unknown or volatile resources return ("", false) and are not learned.
func resourceKind(resource string) (Kind, bool) {
	switch strings.ToLower(resource) {
	case "namespace", "namespaces", "ns":
		return KindNamespace, true
	case "deployment", "deployments", "deploy":
		return KindDeployment, true
	case "service", "services", "svc":
		return KindService, true
	case "statefulset", "statefulsets", "sts":
		return KindStatefulSet, true
	case "configmap", "configmaps", "cm":
		return KindConfigMap, true
	case "node", "nodes", "no":
		return KindNode, true
	case "ingress", "ingresses", "ing":
		return KindIngress, true
	}
	return "", false
}

// clusterScopedKind is true for kinds that have no namespace (don't scope them).
func clusterScopedKind(k Kind) bool { return k == KindNamespace || k == KindNode }

// ExtractFromKubectl mines durable topology facts from the output of a successful
// read-only `kubectl get <resource>` and Learns the cacheable ones. It is the free,
// inline path: deterministic table parsing, no model call. Returns the number of
// facts learned. Anything volatile (pods, replicasets, …) is filtered by Cacheable
// inside Learn, so this can be called on any get without special-casing.
func ExtractFromKubectl(args []string, stdout string, store *Store) int {
	verb, resource, nsFlag, allNS := parseGet(args)
	if verb != "get" || resource == "" {
		return 0
	}
	kind, ok := resourceKind(resource)
	if !ok {
		return 0 // volatile or unknown resource — nothing durable to learn
	}

	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) < 2 {
		return 0
	}
	header := strings.Fields(lines[0])
	if len(header) == 0 {
		return 0
	}
	// With -A/--all-namespaces the first column is NAMESPACE, then NAME.
	nsCol, nameCol := -1, 0
	if strings.EqualFold(header[0], "NAMESPACE") {
		nsCol, nameCol = 0, 1
	}

	learned := 0
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) <= nameCol {
			continue
		}
		name := fields[nameCol]
		ns := ""
		switch {
		case clusterScopedKind(kind):
			ns = "" // namespaces/nodes are cluster-scoped
		case nsCol >= 0 && len(fields) > nsCol:
			ns = fields[nsCol] // from the -A NAMESPACE column
		case allNS:
			ns = "" // -A but no NAMESPACE column parsed; leave unscoped
		default:
			ns = nsFlag // from -n/--namespace
		}
		if store.Learn(kind, name, ns) {
			learned++
		}
	}
	return learned
}

// parseGet pulls (verb, resource, namespace, allNamespaces) out of kubectl args.
// resource is the first non-flag token after the verb.
func parseGet(args []string) (verb, resource, namespace string, allNS bool) {
	nonflags := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-A" || a == "--all-namespaces":
			allNS = true
		case a == "-n" || a == "--namespace":
			if i+1 < len(args) {
				namespace = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "--namespace="):
			namespace = strings.TrimPrefix(a, "--namespace=")
		case strings.HasPrefix(a, "-n="):
			namespace = strings.TrimPrefix(a, "-n=")
		case strings.HasPrefix(a, "-"):
			// other flag; skip (and skip its value if it's a known value-flag)
			if a == "-o" || a == "--output" || a == "-l" || a == "--selector" {
				i++
			}
		default:
			nonflags = append(nonflags, a)
		}
	}
	if len(nonflags) > 0 {
		verb = strings.ToLower(nonflags[0])
	}
	if len(nonflags) > 1 {
		resource = nonflags[1]
	}
	return verb, resource, namespace, allNS
}
