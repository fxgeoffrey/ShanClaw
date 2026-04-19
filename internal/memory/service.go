package memory

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type ServiceStatus int32

const (
	StatusDisabled ServiceStatus = iota
	StatusInitializing
	StatusReady
	StatusDegraded
	StatusUnavailable
)

func (s ServiceStatus) String() string {
	return [...]string{"disabled", "initializing", "ready", "degraded", "unavailable"}[s]
}

// Service is the orchestrator that daemon code and the memory_recall tool
// talk to. It owns the sidecar lifecycle (in daemon mode; CLI/TUI use
// AttachPolicy + NewServiceAttached instead) and coordinates the bundle
// puller goroutine. Tool fallback is triggered whenever Status() != Ready.
type Service struct {
	cfg      Config
	audit    AuditLogger
	sidecar  *Sidecar
	puller   *Puller
	client   *Client
	status   atomic.Int32
	cancel   context.CancelFunc
	attached bool // true for NewServiceAttached path; never spawns

	// Test injection: extra positional args prepended to "serve --socket
	// --bundle-root" so unit tests can run a fake binary (e.g. python3 with
	// a script path). Production callers leave this nil.
	testExtraSpawnArgs []string
}

// NewService builds the daemon-mode Service that owns sidecar lifecycle.
func NewService(cfg Config, audit AuditLogger) *Service {
	return &Service{cfg: cfg, audit: audit}
}

// NewServiceAttached builds a Service for the CLI/TUI attach-only path.
// AttachPolicy must have already confirmed a reachable sidecar before this
// is constructed; the returned Service never spawns. (Wired in Task 19.)
func NewServiceAttached(cfg Config, audit AuditLogger) *Service {
	return &Service{cfg: cfg, audit: audit, attached: true}
}

func (s *Service) Status() ServiceStatus { return ServiceStatus(s.status.Load()) }

func (s *Service) logAudit(ev string, fields map[string]any) {
	if s.audit != nil {
		s.audit.Log(ev, fields)
	}
}

// tlmAvailable reports whether the configured (or PATH-resolved) sidecar
// binary is callable. A bare command name (e.g. "tlm" or "python3" in tests)
// is resolved via exec.LookPath; an absolute path is checked via os.Stat.
func (s *Service) tlmAvailable() bool {
	if s.cfg.TLMPath != "" {
		if _, err := os.Stat(s.cfg.TLMPath); err == nil {
			return true
		}
		if _, err := exec.LookPath(s.cfg.TLMPath); err == nil {
			return true
		}
		return false
	}
	_, err := exec.LookPath("tlm")
	return err == nil
}

// Start runs the cold-path gates from spec §3.6 (steps 1-3) and sets status
// accordingly. Sidecar spawn + supervisor + puller wiring lands in Task 14;
// this function returns nil after the gates without spawning anything.
//
// All failure modes are silent: the function returns nil even when the
// service is Unavailable or Disabled. Callers check Status() to decide
// whether to proceed.
func (s *Service) Start(ctx context.Context) error {
	if s.cfg.Provider == "disabled" || s.cfg.Provider == "" {
		s.status.Store(int32(StatusDisabled))
		return nil
	}
	if !s.tlmAvailable() {
		s.status.Store(int32(StatusUnavailable))
		s.logAudit("memory_tlm_missing", map[string]any{"tlm_path_set": s.cfg.TLMPath != ""})
		return nil
	}
	if s.cfg.Provider == "cloud" {
		if s.cfg.Endpoint == "" || s.cfg.APIKey == "" {
			s.status.Store(int32(StatusUnavailable))
			s.logAudit("memory_cloud_misconfigured", map[string]any{
				"endpoint_resolved": s.cfg.Endpoint != "",
				"api_key_present":   s.cfg.APIKey != "",
			})
			return nil
		}
	}
	s.status.Store(int32(StatusInitializing))

	// Spawn the supervisor goroutine. It owns the full spawn →
	// wait-ready → wait → backoff loop. Cold-start failures (failed first
	// WaitReady) are treated identically to runtime crashes — no daemon
	// restart required to recover from a slow-disk first boot.
	s.sidecar = NewSidecar(s.cfg, s.testExtraSpawnArgs)
	supCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	var pullerOnce sync.Once
	onReady := func() {
		s.status.Store(int32(StatusReady))
		s.client = NewClient(s.cfg.SocketPath, s.cfg.ClientRequestTimeout)
		if s.cfg.Provider == "cloud" {
			pullerOnce.Do(func() {
				s.puller = NewPuller(s.cfg, s.sidecar, s.audit)
				go s.runPullerLoop(supCtx)
			})
		}
	}

	sup := NewSupervisor(s.sidecar, s.cfg.SidecarRestartMax, onReady)
	go func() {
		final := sup.Run(supCtx)
		// Map supervisor's terminal state into service status.
		switch final {
		case StateDegraded:
			s.status.Store(int32(StatusDegraded))
			s.logAudit("memory_sidecar_degraded", map[string]any{})
		case StateStopped:
			// ctx cancel — Stop() was called; leave status alone.
		}
	}()
	return nil
}

// runPullerLoop runs the 24h bundle pull ticker. Honors
// BundlePullStartupDelay, exits on ctx cancel. Cloud-mode only (caller
// gates this in Start).
func (s *Service) runPullerLoop(ctx context.Context) {
	if s.cfg.BundlePullStartupDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.cfg.BundlePullStartupDelay):
		}
	}
	if err := s.puller.tick(ctx); err != nil {
		s.logAudit("memory_reload_failed", map[string]any{"reason": err.Error()})
	}
	interval := s.cfg.BundlePullInterval
	if interval <= 0 {
		return // misconfigured; no recurring ticks
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.puller.tick(ctx); err != nil {
				s.logAudit("memory_reload_failed", map[string]any{"reason": err.Error()})
			}
		}
	}
}

// Query is the only entry point the memory_recall tool needs. Returns
// ClassUnavailable whenever the service is not Ready (so the tool falls
// back instead of erroring).
func (s *Service) Query(ctx context.Context, intent QueryIntent) (*ResponseEnvelope, ErrorClass, error) {
	if s.Status() != StatusReady || s.client == nil {
		return nil, ClassUnavailable, nil
	}
	return s.client.Query(ctx, intent)
}

// Stop cancels the supervisor + puller goroutines and shuts the sidecar
// down within the configured grace period. Best-effort — daemon shutdown
// does not block on this.
func (s *Service) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.sidecar != nil {
		return s.sidecar.Shutdown(s.cfg.SidecarShutdownGrace)
	}
	return nil
}
