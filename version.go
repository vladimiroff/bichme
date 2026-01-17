package bichme

import "runtime/debug"

// Version of the release.
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}

	return info.Main.Version
}
