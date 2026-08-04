//go:build windows

package cmd

import (
	"os"
	"os/exec"

	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/shutdown"
	"github.com/getlantern/systray"
	"golang.org/x/sys/windows/registry"
)

// autoStartKey 是 Windows 注册表 HKCU Run 键下的启动项名称。
const autoStartKey = "Octopus"

const autoStartRunPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func cmdLogError(action string, err error) {
	log.Warnf("%s: %v", action, err)
}

// runTray 在 Windows 上启动系统托盘：打开面板 / 开机自启开关 / 退出。
func runTray(addr string) {
	target := "http://127.0.0.1" + normalizeAddrPort(addr)
	systray.Run(func() {
		systray.SetTitle("Octopus")
		systray.SetTooltip("Octopus - LLM API 聚合服务")
		mOpen := systray.AddMenuItem("打开面板", "在浏览器中打开管理面板")
		mAutoStart := systray.AddMenuItem("开机自启", "登录 Windows 后自动启动")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 Octopus")

		updateAutoStartLabel(mAutoStart)

		go func() {
			for {
				select {
				case <-mOpen.ClickedCh:
					if err := exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start(); err != nil {
						log.Warnf("tray: open browser: %v", err)
					}
				case <-mAutoStart.ClickedCh:
					toggleAutoStart(mAutoStart)
				case <-mQuit.ClickedCh:
					shutdown.Request()
					systray.Quit()
					return
				}
			}
		}()
	}, nil)
}

// updateAutoStartLabel 根据注册表当前状态刷新菜单文字。
func updateAutoStartLabel(item *systray.MenuItem) {
	if isAutoStartEnabled() {
		item.SetTitle("开机自启 ✓")
	} else {
		item.SetTitle("开机自启")
	}
}

func toggleAutoStart(item *systray.MenuItem) {
	enabled := isAutoStartEnabled()
	if err := setAutoStart(!enabled); err != nil {
		log.Warnf("tray: toggle auto start: %v", err)
		return
	}
	updateAutoStartLabel(item)
}

// autoStartCommand 返回用于开机自启的启动命令：当前可执行文件 + desktop 子命令。
func autoStartCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return ""
	}
	return `"` + exe + `" desktop`
}

func isAutoStartEnabled() bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	_, _, err = key.GetStringValue(autoStartKey)
	return err == nil
}

func setAutoStart(enabled bool) error {
	key, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if enabled {
		command := autoStartCommand()
		if command == "" {
			return nil
		}
		return key.SetStringValue(autoStartKey, command)
	}
	if isAutoStartEnabled() {
		return key.DeleteValue(autoStartKey)
	}
	return nil
}
