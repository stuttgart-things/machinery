package main

// Build-time variables injected via -ldflags by GoReleaser
// (see .goreleaser.yaml, build id "machinery-client").
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)
