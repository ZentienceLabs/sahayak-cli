package llm

import "testing"

// FuzzParsePlan ensures the plan parser never panics on arbitrary model output —
// it consumes untrusted text from a possibly-misbehaving small model, so robust
// failure (an error, not a crash) is a safety requirement.
func FuzzParsePlan(f *testing.F) {
	seeds := []string{
		`{"summary":"x","steps":[{"command":"ls","args":["-la"],"explanation":"y"}]}`,
		"```json\n{\"summary\":\"x\",\"steps\":[]}\n```",
		`prose before {"summary":"x"} prose after`,
		`{"summary":"x","steps":[{"command":"echo","args":["{nested}"],"explanation":"z"}]}`,
		`{`,
		`}`,
		``,
		`{"steps": [ {"args": [ "\"" ] } ] }`,
		`{"need_more_info":"which namespace?"}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Must not panic; both a Plan and a Diagnosis parse path are exercised.
		_, _ = ParsePlan(s)
		_, _ = ParseDiagnosis(s)
	})
}
