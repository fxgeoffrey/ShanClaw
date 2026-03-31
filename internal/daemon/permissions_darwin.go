package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/Kocoro-lab/ShanClaw/internal/tools"
)


// probePermissions checks current TCC permission status via ax_server.
func probePermissions(ctx context.Context) permissionStatus {
	binPath, bundlePath, err := tools.AXServerPaths()
	if err != nil {
		return unknownStatus()
	}

	// Bundled mode: use persistent ax_server launched via LaunchServices
	if bundlePath != "" {
		return probePermissionsViaSocket(ctx)
	}

	// Fallback: one-shot exec.Command
	cmd := exec.CommandContext(ctx, binPath, "--check-permissions")
	out, err := cmd.Output()
	if err != nil {
		return unknownStatus()
	}

	var status map[string]string
	if err := json.Unmarshal(out, &status); err != nil {
		return unknownStatus()
	}

	return permissionStatus{
		ScreenRecording: statusOrUnknown(status, "screen_recording"),
		Accessibility:   statusOrUnknown(status, "accessibility"),
		Automation:      statusOrUnknown(status, "automation"),
	}
}

// requestPermission triggers macOS permission dialogs via ax_server.
func requestPermission(ctx context.Context, permission string) permissionResult {
	switch permission {
	case "screen_recording", "accessibility", "automation":
		// valid
	default:
		return permissionResult{Permission: permission, Status: "unknown", Message: "unsupported permission"}
	}

	binPath, bundlePath, err := tools.AXServerPaths()
	if err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    fmt.Sprintf("ax_server not found: %v", err),
		}
	}

	// Bundled mode: use persistent ax_server launched via LaunchServices
	if bundlePath != "" {
		return requestPermissionViaSocket(ctx, permission)
	}

	// Fallback: one-shot exec.Command
	cmd := exec.CommandContext(ctx, binPath, "--request-permission", permission)
	out, err := cmd.Output()
	if err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    fmt.Sprintf("ax_server request failed: %v", err),
		}
	}

	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    "failed to parse ax_server response",
		}
	}

	return permissionResult{
		Permission: result["permission"],
		Status:     result["status"],
		Message:    result["message"],
	}
}

func probePermissionsViaSocket(ctx context.Context) permissionStatus {
	client := tools.SharedAXClient()
	if err := client.Ensure(ctx); err != nil {
		return unknownStatus()
	}

	result, err := client.Call(ctx, "check_permissions", nil)
	if err != nil {
		return unknownStatus()
	}

	var status map[string]string
	if err := json.Unmarshal(result, &status); err != nil {
		return unknownStatus()
	}

	return permissionStatus{
		ScreenRecording: statusOrUnknown(status, "screen_recording"),
		Accessibility:   statusOrUnknown(status, "accessibility"),
		Automation:      statusOrUnknown(status, "automation"),
	}
}

func requestPermissionViaSocket(ctx context.Context, permission string) permissionResult {
	client := tools.SharedAXClient()
	if err := client.Ensure(ctx); err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    fmt.Sprintf("ax_server not reachable: %v", err),
		}
	}

	result, err := client.Call(ctx, "request_permission", map[string]string{"value": permission})
	if err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    fmt.Sprintf("ax_server request failed: %v", err),
		}
	}

	var resp map[string]string
	if err := json.Unmarshal(result, &resp); err != nil {
		return permissionResult{
			Permission: permission,
			Status:     "unknown",
			Message:    "failed to parse ax_server response",
		}
	}

	return permissionResult{
		Permission: resp["permission"],
		Status:     resp["status"],
		Message:    resp["message"],
	}
}

func unknownStatus() permissionStatus {
	return permissionStatus{
		ScreenRecording: "unknown",
		Accessibility:   "unknown",
		Automation:      "unknown",
	}
}

func statusOrUnknown(m map[string]string, key string) string {
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return "unknown"
}

