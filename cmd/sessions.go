package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/Kocoro-lab/ShanClaw/internal/audit"
	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/sync"
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "Manage local sessions (sync to Cloud, search, etc.)",
}

var sessionsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Upload modified sessions to Shannon Cloud (opt-in via sync.enabled)",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("locate home dir: %w", err)
		}
		shannonHome := filepath.Join(home, ".shannon")

		cfg := sync.LoadConfig(viper.GetViper())
		if !cfg.Enabled && !cfg.DryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "sync is disabled (sync.enabled=false). Set sync.enabled=true to enable.")
			return nil
		}

		var uploader sync.Uploader
		outboxDir := filepath.Join(shannonHome, "sync_outbox")
		if cfg.DryRun {
			uploader = &sync.DryRunUploader{OutboxDir: outboxDir, Now: time.Now}
		} else {
			endpoint := sync.ResolveEndpoint(cfg, viper.GetViper())
			apiKey := viper.GetString("cloud.api_key")
			if endpoint == "" || apiKey == "" {
				return fmt.Errorf("upload endpoint (sync.endpoint or cloud.endpoint) and cloud.api_key must be configured (or set sync.dry_run=true)")
			}
			gw := client.NewGatewayClient(endpoint, apiKey)
			uploader = &sync.CloudUploader{Client: gw}
		}

		auditLogger, err := audit.NewAuditLogger(filepath.Join(shannonHome, "logs"))
		if err != nil {
			return fmt.Errorf("open audit log: %w", err)
		}
		defer auditLogger.Close()

		loader := func(dir, id string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, id+".json"))
		}

		ver := readBuildInfo()

		deps := sync.Deps{
			Cfg:       cfg,
			HomeDir:   shannonHome,
			ClientVer: ver,
			Uploader:  uploader,
			Loader:    loader,
			Audit:     &auditAdapter{logger: auditLogger},
			Now:       time.Now,
		}
		return sync.Run(context.Background(), deps)
	},
}

// auditAdapter bridges *audit.AuditLogger (which logs structured AuditEntry
// rows) to sync.AuditLogger (event + free-form fields). The fields map is
// JSON-encoded into AuditEntry.InputSummary so the JSON-lines audit log keeps
// a single shape.
type auditAdapter struct {
	logger *audit.AuditLogger
}

func (a *auditAdapter) Log(event string, fields map[string]any) {
	if a == nil || a.logger == nil {
		return
	}
	summary := ""
	if len(fields) > 0 {
		if b, err := json.Marshal(fields); err == nil {
			summary = string(b)
		}
	}
	a.logger.Log(audit.AuditEntry{
		Timestamp:    time.Now(),
		ToolName:     event,
		InputSummary: summary,
		Decision:     "info",
	})
}

func readBuildInfo() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return "shanclaw/" + info.Main.Version
	}
	return "shanclaw/dev"
}

func init() {
	sessionsCmd.AddCommand(sessionsSyncCmd)
	rootCmd.AddCommand(sessionsCmd)
}
