package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Checkpoints implement the short-term tier: a thread-scoped snapshot of agent
// state, written after key transitions so a crashed or interrupted session can be
// resumed. (The resume UX is wired in a later phase; the durable store is here.)

func (s *Store) checkpointDir() string { return filepath.Join(s.Dir, "checkpoints") }

// SaveCheckpoint atomically persists state for a thread id.
func (s *Store) SaveCheckpoint(thread string, state any) error {
	if err := os.MkdirAll(s.checkpointDir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.checkpointDir(), sanitizeThread(thread)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// LoadCheckpoint reads a thread's checkpoint into out. Returns os.ErrNotExist when
// there is no checkpoint for the thread.
func (s *Store) LoadCheckpoint(thread string, out any) error {
	path := filepath.Join(s.checkpointDir(), sanitizeThread(thread)+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// ClearCheckpoint removes a thread's checkpoint (e.g. on clean completion).
func (s *Store) ClearCheckpoint(thread string) error {
	path := filepath.Join(s.checkpointDir(), sanitizeThread(thread)+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func sanitizeThread(t string) string {
	if t == "" {
		return "default"
	}
	out := make([]rune, 0, len(t))
	for _, r := range t {
		switch r {
		case '/', '\\', ':', ' ', '.':
			out = append(out, '-')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
