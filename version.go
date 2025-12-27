package bichme

// version can be overriden build time via -ldflags.
var version = "dev"

// Version of the release.
func Version() string { return version }
