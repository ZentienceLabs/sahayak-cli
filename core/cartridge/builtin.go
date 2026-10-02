package cartridge

import _ "embed"

//go:embed k8s.json
var k8sJSON []byte

//go:embed systemd.json
var systemdJSON []byte

// Builtins returns the cartridges shipped in the binary. They are PEERS: k8s and systemd
// are both just data, proving the engine is tool-agnostic (systemd needed zero new
// tool-specific Go — only the shared, reusable after-verb slot primitive). Installed
// .sahayakpack cartridges layer on top at load time.
func Builtins() ([]Cartridge, error) {
	var out []Cartridge
	for _, raw := range [][]byte{k8sJSON, systemdJSON} {
		c, err := Parse(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
