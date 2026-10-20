package update

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// runVersion runs "<binary> -version" and returns what it prints.
func runVersion(binary string) (string, error) {
	cmd := exec.Command(binary, "-version")
	done := make(chan struct{})
	var out []byte
	var err error
	go func() { out, err = cmd.Output(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("no answer in 10 s")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func freeBytes(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
