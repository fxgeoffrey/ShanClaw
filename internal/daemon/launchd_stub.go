//go:build !darwin

package daemon

import "fmt"

func DaemonPlistPath() string                            { return "" }
func GenerateDaemonPlist(shanBin, logPath string) string { return "" }
func WriteDaemonPlist(path, content string) error {
	return fmt.Errorf("launchd not supported on this platform")
}
func RemoveDaemonPlist() error                  { return nil }
func LaunchctlBootstrap(plistPath string) error { return fmt.Errorf("launchd not supported on this platform") }
func LaunchctlBootout() error                   { return nil }
func IsDaemonServiceLoaded() bool               { return false }
func ShanBinary() string                        { return "shan" }
