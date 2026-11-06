//go:build !darwin && !linux && !windows

package desktop

import "os/exec"

func prepareOwnedProcess(*exec.Cmd)            {}
func ownProcess(cmd *exec.Cmd) (func(), error) { return func() { _ = cmd.Process.Kill() }, nil }
