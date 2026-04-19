package memory

import (
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Provider               string
	Endpoint               string
	APIKey                 string
	SocketPath             string
	BundleRoot             string
	TLMPath                string
	BundlePullInterval     time.Duration
	BundlePullStartupDelay time.Duration
	SidecarReadyTimeout    time.Duration
	SidecarShutdownGrace   time.Duration
	SidecarRestartMax      int
	ClientRequestTimeout   time.Duration
}

// LoadConfig produces a typed view of memory.* viper keys. Defaults are
// registered in internal/config/config.go (single source of truth); this
// package's accessors must not register defaults.
func LoadConfig(v *viper.Viper) Config {
	return Config{
		Provider:               v.GetString("memory.provider"),
		Endpoint:               v.GetString("memory.endpoint"),
		APIKey:                 v.GetString("memory.api_key"),
		SocketPath:             expandHome(v.GetString("memory.socket_path")),
		BundleRoot:             expandHome(v.GetString("memory.bundle_root")),
		TLMPath:                expandHome(v.GetString("memory.tlm_path")),
		BundlePullInterval:     v.GetDuration("memory.bundle_pull_interval"),
		BundlePullStartupDelay: v.GetDuration("memory.bundle_pull_startup_delay"),
		SidecarReadyTimeout:    v.GetDuration("memory.sidecar_ready_timeout"),
		SidecarShutdownGrace:   v.GetDuration("memory.sidecar_shutdown_grace"),
		SidecarRestartMax:      v.GetInt("memory.sidecar_restart_max"),
		ClientRequestTimeout:   v.GetDuration("memory.client_request_timeout"),
	}
}

// ResolveAPIKey is the ONLY call site that reads memory.api_key / cloud.api_key.
// memory.api_key wins as override; falls back to cloud.api_key.
func ResolveAPIKey(v *viper.Viper) string {
	if k := strings.TrimSpace(v.GetString("memory.api_key")); k != "" {
		return k
	}
	return strings.TrimSpace(v.GetString("cloud.api_key"))
}

// ResolveEndpoint mirrors ResolveAPIKey for the endpoint pair.
func ResolveEndpoint(v *viper.Viper) string {
	if e := strings.TrimSpace(v.GetString("memory.endpoint")); e != "" {
		return e
	}
	return strings.TrimSpace(v.GetString("cloud.endpoint"))
}

func expandHome(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "$HOME") {
		return os.Getenv("HOME") + p[len("$HOME"):]
	}
	if strings.HasPrefix(p, "~") {
		return os.Getenv("HOME") + p[1:]
	}
	return p
}
