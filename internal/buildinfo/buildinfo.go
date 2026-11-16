// Package buildinfo holds version metadata injected at build time via ldflags.
package buildinfo

import "runtime/debug"

var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

func init() {
	if Version != "dev" {
		return
	}
	// `go install` builds carry no ldflags, but the module version is still known.
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}
}
