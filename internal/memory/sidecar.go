package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"
)

type SidecarState int32

const (
	StateStopped SidecarState = iota
	StateStarting
	StateReady
	StateRestarting
	StateDegraded
)

func (s SidecarState) String() string {
	return [...]string{"stopped", "starting", "ready", "restarting", "degraded"}[s]
}

// ErrTLMNotFound is the sentinel returned when neither memory.tlm_path nor
// PATH yields a usable sidecar binary. Service treats it as terminal — no
// restart loop, just status=Unavailable.
var ErrTLMNotFound = errors.New("memory: tlm binary not found in PATH and memory.tlm_path empty")

// Sidecar is the managed child-process handle for the local memory sidecar.
// Daemon-only: only Service.Start (in daemon mode) constructs and operates a
// Sidecar. CLI/TUI use AttachPolicy instead — they never spawn.
type Sidecar struct {
	cfg      Config
	extraArg []string // test injection: prefix args before "serve --socket --bundle-root"
	cmd      *exec.Cmd
	state    atomic.Int32
}

// NewSidecar builds a Sidecar bound to cfg. extraArg lets tests prepend
// positional args (e.g. a python script path when TLMPath="python3").
// Production callers pass nil.
func NewSidecar(cfg Config, extraArg []string) *Sidecar {
	return &Sidecar{cfg: cfg, extraArg: extraArg}
}

func (s *Sidecar) Status() SidecarState { return SidecarState(s.state.Load()) }

func (s *Sidecar) resolveBinary() (string, error) {
	if s.cfg.TLMPath != "" {
		// Honor explicit configuration. Existence is checked at exec time;
		// returning the path here lets ErrTLMNotFound surface from Start().
		if _, err := os.Stat(s.cfg.TLMPath); err == nil {
			return s.cfg.TLMPath, nil
		}
		// If the configured path is "python3" or a bare command, fall through
		// to PATH lookup so test rigs work.
		if p, err := exec.LookPath(s.cfg.TLMPath); err == nil {
			return p, nil
		}
		return "", ErrTLMNotFound
	}
	p, err := exec.LookPath("tlm")
	if err != nil {
		return "", ErrTLMNotFound
	}
	return p, nil
}

// Spawn starts the sidecar child process. Removes any stale socket first.
// Sets PGID so Shutdown can SIGTERM the whole process group.
func (s *Sidecar) Spawn(ctx context.Context) error {
	bin, err := s.resolveBinary()
	if err != nil {
		return err
	}
	if s.cfg.SocketPath != "" {
		_ = os.Remove(s.cfg.SocketPath)
	}
	args := append([]string{}, s.extraArg...)
	args = append(args, "serve", "--socket", s.cfg.SocketPath, "--bundle-root", s.cfg.BundleRoot)
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	s.cmd = cmd
	s.state.Store(int32(StateStarting))
	return nil
}

// Wait blocks until the child exits. Used by the supervisor (Task 8) to
// detect crashes. Returns nil if the cmd was never started.
func (s *Sidecar) Wait() error {
	if s.cmd == nil {
		return nil
	}
	return s.cmd.Wait()
}

// WaitReady polls /health every 100ms until ready=true, ctx is canceled,
// or ceiling elapses (whichever first).
func (s *Sidecar) WaitReady(ctx context.Context, ceiling time.Duration) error {
	c := NewClient(s.cfg.SocketPath, 1*time.Second)
	deadline := time.Now().Add(ceiling)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sidecar wait_ready ceiling exceeded after %s", ceiling)
		}
		probeCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
		h, err := c.Health(probeCtx)
		cancel()
		if err == nil && h.Ready {
			s.state.Store(int32(StateReady))
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Shutdown sends SIGTERM to the process group, waits up to grace, then SIGKILLs
// if needed. Best-effort socket unlink at the end.
func (s *Sidecar) Shutdown(grace time.Duration) error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(s.cmd.Process.Pid)
	if err != nil {
		// Fallback: signal the process directly.
		pgid = s.cmd.Process.Pid
	}
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(grace):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(1 * time.Second):
		}
	}
	if s.cfg.SocketPath != "" {
		_ = os.Remove(s.cfg.SocketPath)
	}
	s.state.Store(int32(StateStopped))
	return nil
}

// AttachPolicy probes /health on the given socket. Never spawns. CLI/TUI use
// this — they get (ready, _) and decide whether to enable the memory tool or
// fall back. err is reserved for unexpected probe failures worth logging
// (currently always returned as nil; reserved for future use).
func AttachPolicy(ctx context.Context, socket string) (bool, error) {
	c := NewClient(socket, 1*time.Second)
	pctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	h, err := c.Health(pctx)
	if err != nil {
		return false, nil
	}
	return h.Ready, nil
}
