//go:build darwin

package skills

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// keychainWrite stores a password in the default keychain.
// -U updates the item if it already exists.
func keychainWrite(service, account, password string) error {
	cmd := exec.Command("/usr/bin/security",
		"add-generic-password",
		"-U",
		"-s", service,
		"-a", account,
		"-w", password,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("security add-generic-password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// keychainRead retrieves a password. Returns an error if the item is not found.
// -w prints only the password value on stdout.
func keychainRead(service, account string) (string, error) {
	cmd := exec.Command("/usr/bin/security",
		"find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("security find-generic-password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// keychainDelete removes a password. Returns nil if the item doesn't exist.
func keychainDelete(service, account string) error {
	cmd := exec.Command("/usr/bin/security",
		"delete-generic-password",
		"-s", service,
		"-a", account,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// security exits with code 44 when the item is not found.
		// Treat as success (idempotent delete).
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return nil
		}
		return fmt.Errorf("security delete-generic-password: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
