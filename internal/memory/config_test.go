package memory

import (
	"testing"

	"github.com/spf13/viper"
)

func TestResolveAPIKey(t *testing.T) {
	cases := []struct{ mem, cloud, want string }{
		{"", "", ""},
		{"", "c", "c"},
		{"m", "", "m"},
		{"m", "c", "m"},
	}
	for _, tc := range cases {
		v := viper.New()
		v.Set("memory.api_key", tc.mem)
		v.Set("cloud.api_key", tc.cloud)
		if got := ResolveAPIKey(v); got != tc.want {
			t.Fatalf("mem=%q cloud=%q got %q want %q", tc.mem, tc.cloud, got, tc.want)
		}
	}
}

func TestResolveEndpoint(t *testing.T) {
	v := viper.New()
	v.Set("memory.endpoint", "")
	v.Set("cloud.endpoint", "https://c")
	if got := ResolveEndpoint(v); got != "https://c" {
		t.Fatalf("got %q", got)
	}
	v.Set("memory.endpoint", "https://m")
	if got := ResolveEndpoint(v); got != "https://m" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadConfig_HomeExpansion(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	v := viper.New()
	v.Set("memory.bundle_root", "$HOME/.shannon/memory")
	v.Set("memory.socket_path", "$HOME/.shannon/memory.sock")
	v.Set("memory.bundle_pull_interval", "24h")
	v.Set("memory.sidecar_ready_timeout", "10s")
	cfg := LoadConfig(v)
	if cfg.BundleRoot != "/tmp/fakehome/.shannon/memory" {
		t.Fatalf("BundleRoot=%q", cfg.BundleRoot)
	}
	if cfg.SocketPath != "/tmp/fakehome/.shannon/memory.sock" {
		t.Fatalf("SocketPath=%q", cfg.SocketPath)
	}
}
