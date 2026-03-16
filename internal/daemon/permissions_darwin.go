package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

// probePermissions checks current TCC permission status without triggering dialogs.
func probePermissions(ctx context.Context) permissionStatus {
	return permissionStatus{
		ScreenRecording: probeScreenRecording(ctx),
		Accessibility:   probeAccessibility(ctx),
	}
}

// requestPermission triggers the macOS permission dialog for the given permission.
func requestPermission(ctx context.Context, permission string) permissionResult {
	switch permission {
	case "screen_recording":
		return requestScreenRecording(ctx)
	case "accessibility":
		return requestAccessibility(ctx)
	default:
		return permissionResult{Permission: permission, Status: "unknown", Message: "unsupported permission"}
	}
}

// probeScreenRecording checks if screen recording is granted by running
// a silent screencapture and checking if the output is a valid image.
func probeScreenRecording(ctx context.Context) string {
	tmpFile := filepath.Join(os.TempDir(), ".shan-screen-probe.png")
	defer os.Remove(tmpFile)

	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-C", tmpFile)
	if err := cmd.Run(); err != nil {
		return "denied"
	}

	info, err := os.Stat(tmpFile)
	if err != nil || info.Size() < 100 {
		// screencapture succeeds but produces a tiny/blank file when denied
		return "denied"
	}
	return "granted"
}

// probeAccessibility checks if accessibility is granted by attempting to
// read the frontmost app name via AppleScript + System Events.
func probeAccessibility(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		`tell application "System Events" to get name of first process whose frontmost is true`)
	if err := cmd.Run(); err != nil {
		return "denied"
	}
	return "granted"
}

// requestScreenRecording triggers the screen recording permission dialog
// by attempting a screen capture. On first run, macOS shows the dialog.
func requestScreenRecording(ctx context.Context) permissionResult {
	tmpFile := filepath.Join(os.TempDir(), ".shan-screen-probe.png")
	defer os.Remove(tmpFile)

	cmd := exec.CommandContext(ctx, "screencapture", "-x", "-C", tmpFile)
	cmd.Run()

	info, err := os.Stat(tmpFile)
	if err == nil && info.Size() >= 100 {
		return permissionResult{Permission: "screen_recording", Status: "granted"}
	}
	return permissionResult{
		Permission: "screen_recording",
		Status:     "prompted",
		Message:    "Permission dialog shown. Enable in: System Settings > Privacy & Security > Screen Recording",
	}
}

// requestAccessibility triggers the accessibility permission dialog
// by attempting to use System Events via AppleScript.
func requestAccessibility(ctx context.Context) permissionResult {
	cmd := exec.CommandContext(ctx, "osascript", "-e",
		`tell application "System Events" to get name of first process whose frontmost is true`)
	if err := cmd.Run(); err == nil {
		return permissionResult{Permission: "accessibility", Status: "granted"}
	}
	return permissionResult{
		Permission: "accessibility",
		Status:     "prompted",
		Message:    "Permission dialog shown. Enable in: System Settings > Privacy & Security > Accessibility",
	}
}
