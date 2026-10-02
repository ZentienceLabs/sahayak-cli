package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ZentienceLabs/sahayak-cli/core/llm"
	"github.com/ZentienceLabs/sahayak-cli/core/playbook"
)

var errNoJSON = errors.New("no JSON object in model reply")

// classifySystemPrompt asks the model to do the ONE thing a small model is reliable
// at — route + extract — and nothing else. No planning, no commands.
const classifySystemPrompt = `You are Sahayak's intent router. Map the operator's request to ONE intent and extract the target. Do NOT plan or propose commands.

Intents:
- "list": list a kind of resource for an app/namespace (e.g. "configmaps for acme-web"). Set "resource" (configmaps|services|deployments|pods|secrets|ingress|statefulsets|jobs) and "app" (the app or namespace keyword).
- "logs": find why an app is failing / read its error logs. Set "app".
- "image": what container image an app runs. Set "app".
- "rollout": an app's rollout/deployment status. Set "app".
- "restart": restart/redeploy an app. Set "app".
- "verifyenv": check whether an environment variable is set in an app. Set "app" and "env_var" (the VARIABLE_NAME).
- "none": anything else, ambiguous, pod-level crash analysis, or a request that needs multi-step investigation.

Rules:
- "app" MUST be copied verbatim from the request (e.g. "acme-web"). Never invent a name. If you can't find one, use "none".
- Prefer "none" when unsure. Respond with a single JSON object only.`

// classification is the model's routing output.
type classification struct {
	Intent   string `json:"intent"`
	App      string `json:"app"`
	Resource string `json:"resource"`
	EnvVar   string `json:"env_var"`
}

// classifyIntent is the fallback router: when the deterministic matchers miss, it
// makes ONE small, schema-constrained model call to route the request into a known
// playbook. The result is validated deterministically (playbook.FromClassification:
// known intent + entity grounded in the request), so a wrong or hallucinated answer
// degrades to "no plan" rather than a bad command. Returns ok=false to continue to
// the adaptive loop.
func (a *Agent) classifyIntent(ctx context.Context, request string) (playbook.Plan, bool) {
	resp, err := a.Provider.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: classifySystemPrompt},
			{Role: llm.RoleUser, Content: "Request: " + request},
		},
		Temperature: 0,
		JSONOnly:    true,
		JSONSchema:  llm.ClassifySchema,
	})
	if err != nil {
		return playbook.Plan{}, false
	}
	body, err := extractJSONObject(resp.Content)
	if err != nil {
		return playbook.Plan{}, false
	}
	var c classification
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		return playbook.Plan{}, false
	}
	if strings.EqualFold(strings.TrimSpace(c.Intent), "none") {
		return playbook.Plan{}, false
	}
	return playbook.FromClassification(request, c.Intent, c.App, c.Resource, c.EnvVar)
}

// extractJSONObject returns the first balanced top-level JSON object in s (small
// models sometimes wrap it in prose despite schema mode).
func extractJSONObject(s string) (string, error) {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", errNoJSON
	}
	depth, inStr, esc := 0, false, false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return s[start : i+1], nil
			}
		}
	}
	return "", errNoJSON
}
