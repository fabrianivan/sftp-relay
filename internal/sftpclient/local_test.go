package sftpclient

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDialerReadsAFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl, closer, err := LocalDialer(dir, true)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	defer cl.Close()

	entries, err := cl.ReadDir(".")
	if err != nil || len(entries) != 1 || entries[0].Name() != "a.txt" || entries[0].Size() != 5 {
		t.Fatalf("listing = %v, %v", entries, err)
	}
	f, err := cl.Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "hello" {
		t.Fatalf("content = %q", got)
	}
}

func TestLocalDialerReadOnlyRefusesChanges(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(p, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	cl, closer, err := LocalDialer(dir, true)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	defer cl.Close()

	if err := cl.Remove("keep.txt"); err == nil {
		t.Fatal("remove must be refused")
	}
	if err := cl.Rename("keep.txt", "moved.txt"); err == nil {
		t.Fatal("rename must be refused")
	}
	if _, err := cl.Create("new.txt"); err == nil {
		t.Fatal("create must be refused")
	}
	if got, err := os.ReadFile(p); err != nil || string(got) != "keep" {
		t.Fatalf("file changed: %q, %v", got, err)
	}
}

func TestLocalDialerMissingFolder(t *testing.T) {
	if _, _, err := LocalDialer(filepath.Join(t.TempDir(), "nope"), true)(context.Background()); err == nil {
		t.Fatal("expected an error for a missing folder")
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	if _, _, err := LocalDialer(file, true)(context.Background()); err == nil {
		t.Fatal("expected an error when the path is a file")
	}
}
