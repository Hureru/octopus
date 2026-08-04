package cmd

import (
	"net/url"
	"os/exec"
	"runtime"
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

// normalizeAddrPort 把 "127.0.0.1:8080" / "[::1]:8080" 归一为 "[:8080]" 形式，
// 供拼接 URL 使用。
func normalizeAddrPort(addr string) string {
	if len(addr) > 0 && addr[0] == '[' {
		if end := indexByte(addr, ']'); end >= 0 {
			return addr[end:]
		}
	}
	if colon := lastIndexByte(addr, ':'); colon >= 0 {
		return addr[colon:]
	}
	return addr
}

// 内联小工具避免引入额外依赖
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// ensureURL 校验并修正 URL（预留，便于后续扩展）
func ensureURL(target string) string {
	if parsed, err := url.Parse(target); err == nil && parsed.Scheme != "" {
		return parsed.String()
	}
	return target
}
