//go:build !darwin

package main

// pluginSwapDirectories has no verified one-step directory exchange on this
// platform yet. Callers must not fall back to remove-then-rename, because a
// reclaimed MCP proxy can respawn the backend inside that gap and see a missing
// plugin folder. Linux and Windows support stays unverified until a helper is
// confirmed on those systems.
func pluginSwapDirectories(candidate, target string) error {
	return errPluginSwapUnsupported
}
