// Package version holds build information, set with -ldflags at release time.
package version

var (
	// Version is the release tag, for example "v0.1.0".
	Version = "dev"
	// Commit is the git commit the binary was built from.
	Commit = "none"
)

// String returns "<version> (<commit>)".
func String() string {
	return Version + " (" + Commit + ")"
}
