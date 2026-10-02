// Package slots is the tool-agnostic slot-extraction engine for the cartridge
// architecture (see CARTRIDGE-ARCHITECTURE.md). A cartridge command template declares
// its slots as DATA — each slot names a primitive extractor and (for enums) its values
// — and this package grounds them from the user's request. It generalizes the
// hand-written k8s extractors (appEntity, envVarRe, selectorEntity, resource aliases,
// content keywords) into named primitives that any cartridge can reference, so adding a
// tool needs no new extraction code.
//
// Deterministic by design: an extractor returns ok=false when it can't ground a value,
// so the template runner declines rather than firing a half-blind command. A
// grammar-constrained LLM fallback (the brain) handles slots no primitive fits — that
// lives in the agent layer, not here, so this package stays pure and hermetic.
package slots

import (
	"regexp"
	"strings"
)

// Spec declares one slot of a command template (cartridge data).
type Spec struct {
	Name      string            `json:"name"`
	Extractor string            `json:"extractor"`        // see Extract
	Values    map[string]string `json:"values,omitempty"` // for "enum": surface token -> canonical value
	Verbs     []string          `json:"verbs,omitempty"`  // for "after-verb": action verbs to look past
	Required  bool              `json:"required,omitempty"`
}

// Extract grounds a single slot from request. Supported extractors:
//
//	"hyphenated-token"  first identifier-like token containing '-' (e.g. "acme-web")
//	"upper-snake"       a shell-style ENV_VAR (UPPER with an underscore), from the raw text
//	"after-preposition" the entity after for/in/of/on/… (falls back to hyphenated-token)
//	"content-keyword"   the longest distinctive non-stopword token (e.g. "workflow")
//	"enum"              a value from Spec.Values present in the request, returned canonical
//
// ok=false means the slot could not be grounded (the caller should decline or ask).
func Extract(spec Spec, request string) (string, bool) {
	toks := tokenize(request)
	switch spec.Extractor {
	case "hyphenated-token":
		return firstNonEmpty(hyphenatedToken(toks))
	case "upper-snake":
		return firstNonEmpty(envVarRe.FindString(request))
	case "after-preposition":
		if v := afterPreposition(toks); v != "" {
			return v, true
		}
		return firstNonEmpty(hyphenatedToken(toks))
	case "content-keyword":
		return firstNonEmpty(contentKeyword(toks))
	case "enum":
		return enumValue(toks, spec.Values)
	case "after-verb":
		return firstNonEmpty(afterVerb(toks, spec.Verbs))
	default:
		return "", false
	}
}

// ExtractAll grounds every spec from request. ok=false if any REQUIRED slot is missing;
// the returned map holds whatever grounded (optional slots may be absent).
func ExtractAll(specs []Spec, request string) (map[string]string, bool) {
	out := map[string]string{}
	for _, s := range specs {
		v, found := Extract(s, request)
		if found {
			out[s.Name] = v
		} else if s.Required {
			return nil, false
		}
	}
	return out, true
}

func firstNonEmpty(s string) (string, bool) { return s, s != "" }

// envVarRe matches a shell-style env var: uppercase with at least one underscore (so it
// skips bare acronyms like AKS/URL). Read from the ORIGINAL request (tokenize lowercases).
var envVarRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+\b`)

// hyphenatedToken returns the first identifier-like token containing '-'.
func hyphenatedToken(toks []string) string {
	for _, t := range toks {
		if identifierLike(t) && strings.Contains(t, "-") {
			return t
		}
	}
	return ""
}

// afterPreposition returns the last entity that follows a preposition and is itself a
// plausible identifier (mirrors the k8s selectorEntity rule, minus k8s specifics).
func afterPreposition(toks []string) string {
	sel := ""
	for i := 0; i < len(toks)-1; i++ {
		if !prepositions[toks[i]] {
			continue
		}
		// Scan forward past stopwords/prepositions to the first content token
		// ("of the nginx service" → "nginx"). Last preposition window wins.
		for j := i + 1; j < len(toks); j++ {
			c := toks[j]
			if prepositions[c] || stopwords[c] {
				continue
			}
			if len(c) >= 3 {
				sel = c
			}
			break
		}
	}
	return sel
}

// contentKeyword returns the longest distinctive token (not a stopword/preposition).
func contentKeyword(toks []string) string {
	best := ""
	for _, t := range toks {
		if len(t) < 4 || stopwords[t] || prepositions[t] {
			continue
		}
		if len(t) > len(best) {
			best = t
		}
	}
	return best
}

// afterVerb returns the first identifier-like token after any of the given action verbs,
// skipping stopwords ("restart the nginx service" → "nginx"). It handles bare-word
// entities (service/container names) that have no hyphen and follow no preposition —
// the shape `hyphenated-token`/`after-preposition` miss. Verbs are cartridge data, so the
// primitive stays tool-agnostic (systemd, docker, … each supply their own verbs).
func afterVerb(toks []string, verbs []string) string {
	vset := map[string]bool{}
	for _, v := range verbs {
		vset[strings.ToLower(v)] = true
	}
	for i := 0; i < len(toks)-1; i++ {
		if !vset[toks[i]] {
			continue
		}
		for j := i + 1; j < len(toks); j++ {
			if identifierLike(toks[j]) && !prepositions[toks[j]] {
				return toks[j]
			}
		}
	}
	return ""
}

// enumValue returns the canonical value for the first request token present in values.
func enumValue(toks []string, values map[string]string) (string, bool) {
	for _, t := range toks {
		if canon, ok := values[t]; ok {
			return canon, true
		}
	}
	return "", false
}

// identifierLike reports whether a token could be a resource/app name (not filler).
func identifierLike(t string) bool {
	return len(t) >= 4 && !stopwords[t] && !prepositions[t]
}

// tokenize lowercases and splits on any rune that can't appear in a CLI identifier
// (so "acme-web" stays one token but punctuation is dropped).
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_')
	})
}

// prepositions frame an entity ("…for acme-web"). Generic English, tool-agnostic.
var prepositions = map[string]bool{
	"for": true, "in": true, "of": true, "on": true, "to": true, "from": true,
	"with": true, "about": true, "into": true, "at": true, "by": true, "named": true,
	"called": true, "matching": true,
}

// stopwords are common filler/command words that are never a slot value. Deliberately
// generic (no tool nouns) so the engine stays domain-agnostic.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "is": true, "are": true, "was": true, "were": true,
	"please": true, "can": true, "could": true, "you": true, "we": true, "i": true,
	"what": true, "which": true, "show": true, "list": true, "get": true, "give": true,
	"me": true, "my": true, "all": true, "there": true, "do": true, "does": true,
	"have": true, "has": true, "it": true, "that": true, "this": true, "these": true,
	"those": true, "and": true, "or": true, "any": true, "some": true, "tell": true,
	"current": true, "running": true, "set": true, "status": true, "now": true,
	"how": true, "why": true, "where": true, "when": true,
}
