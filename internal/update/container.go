package update

import (
	"os"
	"strings"
)

// InContainer 检测当前进程是否运行在容器中。
// Docker/K8s 容器内自动更新会覆盖镜像内二进制并在重启容器时丢失，
// 且可能破坏自构建版本，因此容器环境必须禁用自动更新。
func InContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	for _, path := range []string{"/proc/self/cgroup", "/proc/1/cgroup"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, "docker") ||
			strings.Contains(content, "containerd") ||
			strings.Contains(content, "kubepods") {
			return true
		}
	}
	return false
}
