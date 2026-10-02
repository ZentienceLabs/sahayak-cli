package llm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// This file implements the embedded engine's process lifecycle (see project.md
// §3.3): prefer a fixed loopback port, fall back to an OS-assigned one if busy,
// publish the chosen port atomically, gate on /health, and reuse a warm server.

// findFreePort returns preferred if it's bindable on loopback, otherwise an
// OS-assigned free port. (Bind-then-close has a small race before the child
// re-binds; the /health poll + reuse logic recover from a lost race.)
func findFreePort(preferred int) (int, error) {
	if l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred)); err == nil {
		_ = l.Close()
		return preferred, nil
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("no free loopback port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// writePortFile atomically records the running server's port (temp + rename).
func writePortFile(path string, port int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strconv.Itoa(port)), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readPortFile reads a previously published port (0, nil-error semantics handled
// by the caller via the returned error).
func readPortFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

// httpHealthy reports whether base/health returns 200 within a short timeout.
// llama-server returns 503 while the model loads and 200 once ready.
func httpHealthy(ctx context.Context, base string) bool {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(c, http.MethodGet, base+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// waitHealthy polls base/health until 200 or the deadline.
func waitHealthy(ctx context.Context, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if httpHealthy(ctx, base) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("llama-server did not become healthy within %s", timeout)
			}
		}
	}
}

// resolveServerBinary finds the bundled/extracted llama-server. Resolution order:
// SAHAYAK_LLAMA_SERVER env, then assets/llama-server/llama-server[.exe] next to the
// executable or in the working tree. (In a release build this is go:embed-extracted.)
func resolveServerBinary() (string, error) {
	name := "llama-server"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p := os.Getenv("SAHAYAK_LLAMA_SERVER"); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("SAHAYAK_LLAMA_SERVER=%s not found", p)
	}
	for _, dir := range assetSearchDirs() {
		cand := filepath.Join(dir, "llama-server", name)
		if fileExists(cand) {
			return cand, nil
		}
	}
	return "", fmt.Errorf("embedded %s not bundled (Phase 6 ships it via go:embed; for dev set SAHAYAK_LLAMA_SERVER or drop it in assets/llama-server/)", name)
}

// resolveModel finds the GGUF weights: explicit path, SAHAYAK_MODEL_PATH, or the
// first *.gguf under assets/models/.
func resolveModel(explicit string) (string, error) {
	if explicit != "" {
		if fileExists(explicit) {
			return explicit, nil
		}
		return "", fmt.Errorf("model path %s not found", explicit)
	}
	if p := os.Getenv("SAHAYAK_MODEL_PATH"); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("SAHAYAK_MODEL_PATH=%s not found", p)
	}
	for _, dir := range assetSearchDirs() {
		matches, _ := filepath.Glob(filepath.Join(dir, "models", "*.gguf"))
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("no GGUF model bundled (Phase 6 embeds Granite 4.0-micro; for dev set SAHAYAK_MODEL_PATH or drop a *.gguf in assets/models/)")
}

// assetSearchDirs returns directories to look for bundled assets: next to the
// binary and ./assets in the working tree.
func assetSearchDirs() []string {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "assets"))
	}
	dirs = append(dirs, "assets")
	return dirs
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
