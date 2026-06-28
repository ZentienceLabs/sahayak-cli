package llm

import "encoding/json"

// Schemas constrain weak-model output to the right SHAPE via Ollama structured
// outputs. They guarantee the reply parses and that fields have the right types —
// e.g. "args" is ALWAYS an array of strings, never a bare string, a number, or a
// missing field. This kills the malformed/truncated-JSON failure class we measured
// on small models (e.g. a string where an array was expected, or a cut-off object).
// Semantics are still repaired by Step.Normalized and the deterministic guards.
//
// A nested Step shape, reused by NextAction and Diagnosis.
const stepSchema = `{
  "type": "object",
  "properties": {
    "command": { "type": "string" },
    "args": { "type": "array", "items": { "type": "string" } },
    "explanation": { "type": "string" }
  },
  "required": ["command", "args"]
}`

// NextActionSchema constrains one investigate-loop turn.
var NextActionSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "thought": { "type": "string" },
    "action": ` + stepSchema + `,
    "done": { "type": "boolean" },
    "final_answer": { "type": "string" }
  },
  "required": ["thought", "done"]
}`)

// PlanSchema constrains the one-shot planner output.
var PlanSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary": { "type": "string" },
    "steps": { "type": "array", "items": ` + stepSchema + ` },
    "need_more_info": { "type": "string" }
  },
  "required": ["summary", "steps"]
}`)

// ClassifySchema constrains the tiny intent-classifier fallback: the model picks one
// known intent and extracts the grounded entity, nothing more. The constraints mirror
// playbook.FromClassification's Go guards so the GRAMMAR rejects what the guard would
// reject anyway — closing the gap at decode time instead of after:
//   - intent enum  → an out-of-set intent is impossible (searchcfg is regex/router-only,
//     so it is deliberately NOT a classifier intent).
//   - resource enum → only real, aliasable resource nouns (kills "env_var=configmaps"
//     style mis-reads at the source).
//   - env_var pattern → must be shell-env-shaped (^[A-Z][A-Z0-9_]{2,}$), matching
//     classify.go's envVarShapeRe. Honored by engines whose schema→grammar
//     supports `pattern` (llama.cpp); the Go guard still enforces it on the rest.
var ClassifySchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "intent": { "type": "string", "enum": ["list","logs","image","rollout","restart","verifyenv","none"] },
    "app": { "type": "string" },
    "resource": { "type": "string", "enum": ["configmaps","services","deployments","pods","secrets","ingress","statefulsets","daemonsets","replicasets","jobs","cronjobs","endpoints","persistentvolumeclaims",""] },
    "env_var": { "type": "string", "pattern": "^[A-Z][A-Z0-9_]{2,}$" }
  },
  "required": ["intent"]
}`)

// DiagnosisSchema constrains the failure-diagnosis output.
var DiagnosisSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "root_cause": { "type": "string" },
    "confidence": { "type": "string", "enum": ["high", "medium", "low"] },
    "next_step": ` + stepSchema + `
  },
  "required": ["root_cause", "confidence"]
}`)
