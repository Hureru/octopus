//go:build !windows

package cmd

import (
	"github.com/bestruirui/octopus/internal/utils/log"
)

func cmdLogError(action string, err error) {
	log.Warnf("%s: %v", action, err)
}

// runTray 在非 Windows 平台为 no-op（桌面模式仅 Windows 提供托盘）。
func runTray(addr string) {
	log.Infof("desktop: system tray is only available on Windows")
}
