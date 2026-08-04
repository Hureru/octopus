package cmd

import (
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// openBrowser 在本地默认浏览器打开管理面板地址。
// Windows 用 rundll32 避免控制台闪现；其他平台用 open/xdg-open。
func openBrowser(addr string) {
	target := "http://127.0.0.1" + normalizeAddrPort(addr)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		cmd = exec.Command("open", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		cmdLogError("open browser", err)
	}
}

// normalizeAddrPort extracts the port from a listen address for a loopback URL.
func normalizeAddrPort(addr string) string {
	addr = strings.TrimSpace(addr)
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		return ":" + port
	}
	if _, err := strconv.ParseUint(addr, 10, 16); err == nil {
		return ":" + addr
	}
	if separator := strings.LastIndexByte(addr, ':'); separator >= 0 {
		port := addr[separator+1:]
		if _, err := strconv.ParseUint(port, 10, 16); err == nil {
			return ":" + port
		}
	}
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return addr
}
