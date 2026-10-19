//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDUX_SEC005_FileReadsRefuseSymlinksAndFIFO(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "stack")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := secureDockerRead(file); err != nil || string(got) != "services: {}\n" {
		t.Fatal("safe regular file refused", err)
	}
	if err := os.Symlink(file, filepath.Join(dir, "link.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := secureDockerRead(filepath.Join(dir, "link.yaml")); err == nil {
		t.Fatal("final symlink followed")
	}
	if err := os.Symlink(dir, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := secureDockerRead(filepath.Join(root, "link", "compose.yaml")); err == nil {
		t.Fatal("directory symlink followed")
	}
	fifo := filepath.Join(dir, "pipe.yaml")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := secureDockerRead(fifo); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO was treated as regular file")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO blocked secure read")
	}
}
func TestDUX_SEC005_FileReadByteCap(t *testing.T) {
	file := filepath.Join(t.TempDir(), "compose.yaml")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := secureDockerRead(file); err != errFileTooLarge {
		t.Fatal("file byte cap ignored", err)
	}
}
