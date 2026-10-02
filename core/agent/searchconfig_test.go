package agent

import (
	"strings"
	"testing"
)

func TestSearchConfigmaps(t *testing.T) {
	// Two namespaces; the workflow flag lives as a KEY in one configmap and the word
	// also appears in a configmap NAME in another.
	js := `{"items":[
    {"metadata":{"name":"acme-web-config","namespace":"acme-dev"},
     "data":{"CONSOLE_WORKFLOW_REDESIGN":"true","API_URL":"https://x"}},
    {"metadata":{"name":"workflow-engine","namespace":"acme-demo"},
     "data":{"TIMEOUT":"30"}},
    {"metadata":{"name":"coredns","namespace":"kube-system"},
     "data":{"Corefile":".:53 { forward . /etc/resolv.conf }"}}
  ]}`
	out := searchConfigmaps(js, "workflow")
	if !strings.Contains(out, "CONSOLE_WORKFLOW_REDESIGN=true") {
		t.Errorf("should surface the matching key+value:\n%s", out)
	}
	if !strings.Contains(out, "acme-web-config") || !strings.Contains(out, "acme-dev") {
		t.Errorf("should name the configmap + namespace:\n%s", out)
	}
	if !strings.Contains(out, "workflow-engine") || !strings.Contains(out, "matches the configmap name") {
		t.Errorf("should match by configmap name too:\n%s", out)
	}
	if strings.Contains(out, "coredns") {
		t.Errorf("unrelated configmap leaked:\n%s", out)
	}
}

func TestSearchConfigmapsNoMatch(t *testing.T) {
	js := `{"items":[{"metadata":{"name":"a","namespace":"b"},"data":{"X":"y"}}]}`
	out := searchConfigmaps(js, "nope")
	if !strings.Contains(out, "No configmap key or value matching") {
		t.Errorf("want a true-absence message:\n%s", out)
	}
}

func TestSearchConfigmapsBadJSON(t *testing.T) {
	if out := searchConfigmaps("not json", "x"); !strings.Contains(out, "could not parse") {
		t.Errorf("want a parse-error message:\n%s", out)
	}
}
