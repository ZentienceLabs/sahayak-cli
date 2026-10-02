// Package version holds build identity, injected at release time via -ldflags.
package version

// These are overridden at build time:
//
//	go build -ldflags "-X github.com/ZentienceLabs/sahayak-cli/core/version.Version=v0.1.0 ..."
var (
	// Version is the semantic version of the build.
	Version = "0.0.0-dev"
	// Commit is the git SHA of the build.
	Commit = "none"
	// Date is the build timestamp.
	Date = "unknown"
)
