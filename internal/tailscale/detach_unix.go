//go:build !windows

package tailscale

import (
	"os/exec"
	"syscall"
)

// detach 让子进程脱离当前会话，Octopus 退出后 tailscaled 仍存活。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
