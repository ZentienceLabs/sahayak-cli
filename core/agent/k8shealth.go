package agent

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/exec"
)

// k8s output is where small models stumble most: they can't reliably read a pod
// table. So Sahayak parses it deterministically and hands the model a plain-language
// HEALTH SUMMARY — which pods are failing, which have restarts — so even a weak
// model focuses on the right pod (or concludes "no errors") instead of guessing.

// isKubectlGetPods reports whether a result is a `kubectl get pods` listing.
func isKubectlGetPods(res exec.Result) bool {
	if strings.ToLower(res.Command) != "kubectl" {
		return false
	}
	hasGet, hasPods := false, false
	for _, a := range res.Args {
		switch strings.ToLower(a) {
		case "get":
			hasGet = true
		case "pods", "pod", "po":
			hasPods = true
		}
	}
	return hasGet && hasPods
}

// podHealthSummary parses a `kubectl get pods` table and returns a deterministic
// one-line health assessment, or "" if the output isn't a recognizable pod table.
func podHealthSummary(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return ""
	}
	header := strings.Fields(lines[0])
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToUpper(h)] = i
	}
	si, hasStatus := col["STATUS"]
	if !hasStatus {
		return ""
	}
	ri, hasReady := col["READY"]
	rsi, hasRestarts := col["RESTARTS"]

	var failing, restarted []string
	total := 0
	for _, l := range lines[1:] {
		f := strings.Fields(l)
		if len(f) <= si {
			continue
		}
		total++
		name := f[0]
		status := f[si]
		ready := ""
		if hasReady && ri < len(f) {
			ready = f[ri]
		}
		restarts := 0
		if hasRestarts && rsi < len(f) {
			restarts = leadingInt(f[rsi])
		}
		if !healthyStatus(status) || !readyOK(ready) {
			failing = append(failing, fmt.Sprintf("%s (%s %s)", name, ready, status))
		} else if restarts > 0 {
			restarted = append(restarted, fmt.Sprintf("%s=%d", name, restarts))
		}
	}
	if total == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "HEALTH SUMMARY (computed by Sahayak): %d pods. ", total)
	switch {
	case len(failing) > 0:
		fmt.Fprintf(&b, "FAILING pods: %s — investigate these (describe / logs, in this same namespace). ", strings.Join(failing, ", "))
	default:
		b.WriteString("All pods are Running/Ready. ")
	}
	if len(restarted) > 0 {
		fmt.Fprintf(&b, "Restarted before (RESTARTS>0, so 'logs --previous' is valid for these, in this namespace): %s. ", strings.Join(restarted, ", "))
	}
	if len(failing) == 0 && len(restarted) == 0 {
		b.WriteString("No failing pods and no restarts → NO ERRORS. Conclude now.")
	} else if len(failing) == 0 {
		b.WriteString("No pod is currently failing; the restarts may be old/benign. If the goal is current errors, you can conclude none — or check the restarted pod's previous logs once.")
	}
	return strings.TrimSpace(b.String())
}

func healthyStatus(s string) bool {
	switch s {
	case "Running", "Completed", "Succeeded":
		return true
	default:
		return false
	}
}

// readyOK returns true for "" or an x/y where x == y (all containers ready).
func readyOK(ready string) bool {
	if ready == "" {
		return true
	}
	parts := strings.SplitN(ready, "/", 2)
	if len(parts) != 2 {
		return true
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return true
	}
	return a == b
}

// leadingInt parses the integer prefix of a field like "9" or "9 (11h ago)".
func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}
