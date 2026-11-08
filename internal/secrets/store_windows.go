//go:build windows

package secrets

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows Credential Manager via advapi32 (CredWriteW / CredReadW / CredDeleteW).
var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredRead   = advapi32.NewProc("CredReadW")
	procCredWrite  = advapi32.NewProc("CredWriteW")
	procCredDelete = advapi32.NewProc("CredDeleteW")
	procCredFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type credManager struct{}

// Default returns the Windows Credential Manager store.
func Default() Store { return credManager{} }

func target(account string) string { return Service + ":" + account }

func (credManager) Get(account string) (string, error) {
	name, err := windows.UTF16PtrFromString(target(account))
	if err != nil {
		return "", err
	}
	var pcred *credential
	r, _, callErr := procCredRead.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&pcred)))
	if r == 0 {
		if errors.Is(callErr, windows.ERROR_NOT_FOUND) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("CredRead: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(pcred)))
	blob := unsafe.Slice(pcred.CredentialBlob, pcred.CredentialBlobSize)
	return decode(string(blob)), nil
}

func (credManager) Set(account, secret string) error {
	name, err := windows.UTF16PtrFromString(target(account))
	if err != nil {
		return err
	}
	user, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return err
	}
	blob := []byte(encode(secret))
	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         name,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
		UserName:           user,
	}
	r, _, callErr := procCredWrite.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if r == 0 {
		return fmt.Errorf("CredWrite: %w", callErr)
	}
	return nil
}

func (credManager) Delete(account string) error {
	name, err := windows.UTF16PtrFromString(target(account))
	if err != nil {
		return err
	}
	r, _, callErr := procCredDelete.Call(uintptr(unsafe.Pointer(name)), credTypeGeneric, 0)
	if r == 0 && !errors.Is(callErr, windows.ERROR_NOT_FOUND) {
		return fmt.Errorf("CredDelete: %w", callErr)
	}
	return nil
}
