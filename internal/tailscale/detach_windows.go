//go:build windows

package tailscale

import "os/exec"

// detach Windows 上无需额外处理。
func detach(cmd *exec.Cmd) {}
