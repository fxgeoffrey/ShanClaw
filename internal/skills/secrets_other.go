//go:build !darwin

package skills

import "fmt"

func keychainWrite(service, account, password string) error {
	return fmt.Errorf("skill secrets are only supported on macOS")
}

func keychainRead(service, account string) (string, error) {
	return "", fmt.Errorf("skill secrets are only supported on macOS")
}

func keychainDelete(service, account string) error {
	return nil
}
