package exec

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		args []string
		want Risk
	}{
		{"ls is read-only", "ls", []string{"-la", "/var"}, ReadOnly},
		{"nginx -t validates (read-only)", "nginx", []string{"-t"}, ReadOnly},
		{"systemctl status read-only", "systemctl", []string{"status", "nginx"}, ReadOnly},
		{"kubectl get read-only", "kubectl", []string{"get", "pods", "-n", "prod"}, ReadOnly},
		{"git status read-only", "git", []string{"status"}, ReadOnly},

		{"kubectl rollout status read-only", "kubectl", []string{"rollout", "status", "deploy/web", "-n", "prod"}, ReadOnly},
		{"kubectl rollout history read-only", "kubectl", []string{"rollout", "history", "deploy/web"}, ReadOnly},

		{"systemctl reload mutating", "systemctl", []string{"reload", "nginx"}, Mutating},
		{"kubectl apply mutating", "kubectl", []string{"apply", "-f", "deploy.yaml"}, Mutating},
		{"kubectl rollout restart mutating", "kubectl", []string{"rollout", "restart", "deploy/web", "-n", "prod"}, Mutating},
		{"apt install mutating", "apt", []string{"install", "-y", "curl"}, Mutating},
		{"unknown binary defaults mutating", "frobnicate", []string{"now"}, Mutating},

		{"rm -rf destructive", "rm", []string{"-rf", "/tmp/x"}, Destructive},
		{"dd destructive on sight", "dd", []string{"if=/dev/zero", "of=/dev/sda"}, Destructive},
		{"kubectl delete destructive", "kubectl", []string{"delete", "ns", "prod"}, Destructive},
		{"git reset --hard destructive", "git", []string{"reset", "--hard"}, Destructive},
		{"docker prune destructive", "docker", []string{"prune", "-a"}, Destructive},
		{"reboot destructive", "reboot", nil, Destructive},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.cmd, c.args); got != c.want {
				t.Errorf("Classify(%q, %v) = %s, want %s", c.cmd, c.args, got, c.want)
			}
		})
	}
}
