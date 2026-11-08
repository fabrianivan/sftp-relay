//go:build !darwin && !windows

package secrets

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// secretTool uses libsecret's secret-tool CLI (GNOME Keyring, KWallet via Secret Service).
type secretTool struct{ bin string }

// Default returns the Secret Service store, or an unsupported store if
// secret-tool is not installed (e.g. on a headless server — use env vars there).
func Default() Store {
	if bin, err := exec.LookPath("secret-tool"); err == nil {
		return secretTool{bin: bin}
	}
	return unsupported{}
}

func (s secretTool) Get(account string) (string, error) {
	out, err := exec.Command(s.bin, "lookup", "service", Service, "account", account).Output()
	if err != nil || len(out) == 0 {
		var ee *exec.ExitError
		if err == nil || errors.As(err, &ee) {
			return "", ErrNotFound
		}
		return "", err
	}
	return decode(string(out)), nil
}

func (s secretTool) Set(account, secret string) error {
	cmd := exec.Command(s.bin, "store", "--label", "SFTP Relay "+account, "service", Service, "account", account)
	cmd.Stdin = strings.NewReader(encode(secret))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("secret-tool store: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (s secretTool) Delete(account string) error {
	return exec.Command(s.bin, "clear", "service", Service, "account", account).Run()
}

type unsupported struct{}

func (unsupported) Get(string) (string, error) { return "", ErrUnsupported }
func (unsupported) Set(string, string) error   { return ErrUnsupported }
func (unsupported) Delete(string) error        { return ErrUnsupported }
