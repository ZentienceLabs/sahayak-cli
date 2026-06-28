package agent

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// knownTools are programs Sahayak hints to the model as available, when present
// on PATH, so it proposes commands the host can actually run.
var knownTools = []string{
	"bash", "sh", "powershell", "pwsh",
	"systemctl", "journalctl", "service",
	"kubectl", "helm", "docker", "podman",
	"git", "az", "aws", "gcloud",
	"apt", "apt-get", "yum", "dnf", "apk", "brew", "winget", "choco",
	"nginx", "apache2", "httpd", "ss", "netstat", "curl", "dig",
}

// machineContext describes the host to the model: OS, shell, cwd, and which
// known tools are installed. This keeps suggestions grounded in reality.
func machineContext() string {
	var b strings.Builder
	fmt.Fprintf(&b, "OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if shell := detectShell(); shell != "" {
		fmt.Fprintf(&b, "Shell: %s\n", shell)
	}
	if cwd, err := os.Getwd(); err == nil {
		fmt.Fprintf(&b, "Working directory: %s\n", cwd)
	}
	fmt.Fprintf(&b, "Available tools: %s\n", strings.Join(availableTools(), ", "))
	return b.String()
}

func detectShell() string {
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("pwsh"); err == nil {
			return "pwsh"
		}
		return "powershell"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "sh"
}

func availableTools() []string {
	var found []string
	for _, t := range knownTools {
		if _, err := exec.LookPath(t); err == nil {
			found = append(found, t)
		}
	}
	if len(found) == 0 {
		found = append(found, "(none of the common DevOps tools detected on PATH)")
	}
	return found
}
