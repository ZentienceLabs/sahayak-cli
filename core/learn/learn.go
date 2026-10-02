// Package learn is Sahayak's self-learning layer — assisted authoring from DETERMINISTIC
// signals. It records what happened (routed/ran/succeeded, an operator ad-hoc command, or
// a request nothing matched) and turns repeated patterns into SUGGESTIONS the human
// reviews. The judge of "did it work" is always deterministic (exit codes, routing hits)
// — never the model judging itself, which would poison the system. Learning produces
// drafts; a human promotes them into a cartridge. See CARTRIDGE-ARCHITECTURE.md.
//
// Static base (command templates + curated KB) is never mutated here; the loop only
// observes and proposes. Promotion to the dynamic overlay is an explicit human action.
package learn

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event is one recorded, deterministically-judged observation.
type Event struct {
	Time      time.Time `json:"time"`
	Kind      string    `json:"kind"` // "routed" (cartridge ran) | "adhoc" (! command) | "missed" (nothing matched)
	Request   string    `json:"request,omitempty"`
	Command   string    `json:"command,omitempty"`
	Args      []string  `json:"args,omitempty"`
	Cartridge string    `json:"cartridge,omitempty"`
	Intent    string    `json:"intent,omitempty"`
	Success   bool      `json:"success"`
}

// Store appends events to ~/.sahayak/learn.jsonl and reads them back.
type Store struct{ path string }

// NewStore returns a Store at dir (or ~/.sahayak when dir is "").
func NewStore(dir string) *Store {
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".sahayak")
		}
	}
	return &Store{path: filepath.Join(dir, "learn.jsonl")}
}

// Record appends one event (best-effort; learning must never break the main flow).
func (s *Store) Record(e Event) error {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	_, err = f.Write(append(b, '\n'))
	return err
}

// Events reads all recorded events (empty if none yet).
func (s *Store) Events() ([]Event, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Event
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Clear removes the learning log.
func (s *Store) Clear() error {
	err := os.Remove(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Suggestion is a human-reviewable draft produced from the events.
type Suggestion struct {
	Kind   string // "promote-template" | "fix-template" | "cover-gap"
	Title  string
	Detail string
	Count  int
}

// MinOccurrences is how many times a pattern must repeat before it's suggested — so a
// one-off doesn't become noise.
const MinOccurrences = 2

// Suggest turns events into drafts, all from deterministic signals:
//   - promote-template: an operator ad-hoc command that SUCCEEDED repeatedly → a candidate
//     to templatize ("this works — make it a playbook").
//   - fix-template:    a cartridge intent that FAILED repeatedly → flag it ("this doesn't
//     work — fix it"), with the failing command.
//   - cover-gap:       requests nothing matched → uncovered phrasings/ops to consider.
func Suggest(events []Event) []Suggestion {
	adhocOK := map[string]int{}
	adhocSample := map[string]Event{}
	failIntent := map[string]int{}
	failSample := map[string]Event{}
	var missed []Event

	for _, e := range events {
		switch e.Kind {
		case "adhoc":
			if e.Success {
				sig := commandSig(e.Command, e.Args)
				adhocOK[sig]++
				if _, ok := adhocSample[sig]; !ok {
					adhocSample[sig] = e
				}
			}
		case "routed":
			if !e.Success {
				key := e.Cartridge + "." + e.Intent
				failIntent[key]++
				failSample[key] = e
			}
		case "missed":
			missed = append(missed, e)
		}
	}

	var out []Suggestion
	for sig, n := range adhocOK {
		if n >= MinOccurrences {
			s := adhocSample[sig]
			out = append(out, Suggestion{
				Kind:   "promote-template",
				Title:  "Templatize a command you run a lot",
				Detail: "ran `" + s.Command + " " + strings.Join(s.Args, " ") + "` successfully " + itoa(n) + "× — consider a command template",
				Count:  n,
			})
		}
	}
	for key, n := range failIntent {
		if n >= MinOccurrences {
			s := failSample[key]
			out = append(out, Suggestion{
				Kind:   "fix-template",
				Title:  "A playbook keeps failing",
				Detail: key + " failed " + itoa(n) + "× (e.g. `" + s.Command + " " + strings.Join(s.Args, " ") + "`) — review the template",
				Count:  n,
			})
		}
	}
	if len(missed) >= MinOccurrences {
		recent := missed
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		var reqs []string
		for _, e := range recent {
			reqs = append(reqs, "“"+e.Request+"”")
		}
		out = append(out, Suggestion{
			Kind:   "cover-gap",
			Title:  "Requests nothing matched",
			Detail: itoa(len(missed)) + " request(s) matched no tool; recent: " + strings.Join(reqs, ", ") + " — consider a phrasing or template",
			Count:  len(missed),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// TopAdhocCommand returns the most frequently-succeeded operator ad-hoc command (the best
// candidate to promote into a template), or ok=false if none. Ties break toward the most
// recent. This grounds promotion in what actually worked.
func TopAdhocCommand(events []Event) (command string, args []string, ok bool) {
	count := map[string]int{}
	last := map[string]Event{}
	for _, e := range events {
		if e.Kind == "adhoc" && e.Success {
			sig := commandSig(e.Command, e.Args)
			count[sig]++
			last[sig] = e
		}
	}
	best, bestN := "", 0
	for sig, n := range count {
		if n > bestN {
			bestN, best = n, sig
		}
	}
	if best == "" {
		return "", nil, false
	}
	e := last[best]
	return e.Command, e.Args, true
}

// commandSig groups similar commands: command + up to two leading non-flag args, with
// "/name" suffixes and value-ish tokens normalized, so per-target variations collapse
// (e.g. `kubectl logs deploy/web -n a` ~ `kubectl logs deploy/api -n b`).
func commandSig(command string, args []string) string {
	parts := []string{command}
	for _, a := range args {
		if len(parts) >= 3 {
			break
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i := strings.IndexByte(a, '/'); i >= 0 {
			a = a[:i] // deploy/web -> deploy
		}
		parts = append(parts, a)
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
