//go:build darwin || linux

package desktop

import (
	"os/exec"
	"sync"
	"syscall"
)

func prepareOwnedProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func ownProcess(cmd *exec.Cmd) (func(), error) {
	var once sync.Once
	return func() { once.Do(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }) }, nil
}
