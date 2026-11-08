//go:build darwin

package secrets

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const securityBin = "/usr/bin/security"

type keychain struct{}

// Default returns the macOS Keychain store.
func Default() Store { return keychain{} }

func (keychain) Get(account string) (string, error) {
	out, err := exec.Command(securityBin, "find-generic-password", "-s", Service, "-a", account, "-w").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 44 {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain lookup: %w", err)
	}
	return decode(string(out)), nil
}

// Set passes the secret over stdin (hex encoded) so it never shows up in the process list.
func (keychain) Set(account, secret string) error {
	cmd := exec.Command(securityBin, "-i")
	cmd.Stdin = strings.NewReader(addCommand(account, secret))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("keychain store: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (keychain) Delete(account string) error {
	err := exec.Command(securityBin, "delete-generic-password", "-s", Service, "-a", account).Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 44 {
		return nil
	}
	return err
}

func addCommand(account, secret string) string {
	return fmt.Sprintf("add-generic-password -U -s %s -a %s -X %s\n",
		Service, sanitize(account), hex.EncodeToString([]byte(encode(secret))))
}
