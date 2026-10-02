package agent

import (
	"strings"
	"testing"
)

func TestDeployPodHealth(t *testing.T) {
	table := `NAME                            READY   STATUS    RESTARTS   AGE
acme-web-64897f7557-7p8jf    0/1     Pending   0          5m
acme-web-64897f7557-nklbl    1/1     Running   0          5m
acme-worker-abc123-xyz       1/1     Running   2          3d`

	// acme-web has one Pending pod → unhealthy, and only its own pods count.
	healthy, detail := deployPodHealth(table, "acme-web")
	if healthy {
		t.Errorf("acme-web should be unhealthy (a Pending pod): %q", detail)
	}
	if !strings.Contains(detail, "Pending") || strings.Contains(detail, "acme-worker") {
		t.Errorf("detail wrong (should cite the Pending web pod, not worker): %q", detail)
	}

	// acme-worker is all Running → healthy (restarts noted but not degrading).
	if h, _ := deployPodHealth(table, "acme-worker"); !h {
		t.Errorf("acme-worker should be healthy")
	}

	// A deployment with no pods is not flagged here (rollout signal judges that).
	if h, d := deployPodHealth(table, "nonexistent"); !h || !strings.Contains(d, "no pods") {
		t.Errorf("absent deployment should be (healthy, 'no pods scheduled'), got (%v, %q)", h, d)
	}
}

func TestComposeStatusConclusionVerdict(t *testing.T) {
	statuses := []deployStatus{
		{name: "acme-web", namespace: "acme-dev", image: "img:1", rolledOut: true, podsHealthy: true, podDetail: "2/2 pods Running", logsClean: true},
		{name: "acme-web", namespace: "acme", image: "img:0", rolledOut: false, rolloutDetail: "exceeded progress deadline", podsHealthy: false, podDetail: "2/2 pods unhealthy: x (0/1 Pending)", logsClean: true},
	}
	got := composeStatusConclusion("acme-web", statuses, 0)

	if !strings.Contains(got, "1/2 healthy") || !strings.Contains(got, "DEGRADED in acme") {
		t.Errorf("TL;DR summary wrong: %q", got)
	}
	if !strings.Contains(got, "✓ HEALTHY") || !strings.Contains(got, "⚠ DEGRADED") {
		t.Errorf("per-namespace verdicts missing: %q", got)
	}
	// Pod health must surface in the degraded namespace.
	if !strings.Contains(got, "0/1 Pending") {
		t.Errorf("pod detail not shown: %q", got)
	}
}

// TestComposeAllHealthy: when every deployment is healthy the TL;DR says so.
func TestComposeAllHealthy(t *testing.T) {
	statuses := []deployStatus{
		{name: "x", namespace: "a", rolledOut: true, podsHealthy: true, logsClean: true},
		{name: "x", namespace: "b", rolledOut: true, podsHealthy: true, logsClean: true},
	}
	got := composeStatusConclusion("x", statuses, 0)
	if !strings.Contains(got, "all 2 deployment(s) HEALTHY") {
		t.Errorf("all-healthy TL;DR wrong: %q", got)
	}
	if strings.Contains(got, "DEGRADED") {
		t.Errorf("should not say DEGRADED when all healthy: %q", got)
	}
}
