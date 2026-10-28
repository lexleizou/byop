package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPiClientArgsAddsOnlyExplicitExtensions(t *testing.T) {
	t.Setenv("BYOP_PLUGINS_DIR", t.TempDir())
	t.Setenv("BYOP_PI_EPHEMERAL", "")
	t.Setenv("BYOP_PI_EXTENSIONS", "")
	if got, want := piClientArgs(), []string{"--mode", "rpc"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("piClientArgs() = %#v, want %#v", got, want)
	}

	first := filepath.Join(t.TempDir(), "lightweight-table.mjs")
	second := filepath.Join(t.TempDir(), "probe.mjs")
	t.Setenv("BYOP_PI_EPHEMERAL", "1")
	t.Setenv("BYOP_PI_EXTENSIONS", first+string(os.PathListSeparator)+second)
	want := []string{"--mode", "rpc", "--no-session", "--extension", first, "--extension", second}
	if got := piClientArgs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("piClientArgs() = %#v, want %#v", got, want)
	}
}
