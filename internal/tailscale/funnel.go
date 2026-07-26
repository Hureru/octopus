package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

const (
	publicHTTPSPort = "443"
	envBinaryPath   = "OCTOPUS_TAILSCALE_BIN"
	statusTimeout   = 5 * time.Second
	actionTimeout   = 30 * time.Second
	installTimeout  = 5 * time.Minute
	maxErrorLength  = 2048
)

type Phase string

const (
	PhaseUnsupported       Phase = "unsupported"
	PhaseNotInstalled      Phase = "not_installed"
	PhaseNeedsLogin        Phase = "needs_login"
	PhaseStopped           Phase = "stopped"
	PhaseRunning           Phase = "running"
	PhaseConflict          Phase = "conflict"
	PhaseCredentialsNeeded Phase = "credentials_needed"
	PhaseError             Phase = "error"
)

var (
	ErrUnsupported       = errors.New("Tailscale Funnel is not supported on this platform")
	ErrNotInstalled      = errors.New("tailscale CLI is not installed; install Tailscale or set " + envBinaryPath)
	ErrNeedsLogin        = errors.New("tailscale is not logged in; run 'tailscale login' first")
	ErrConflict          = errors.New("HTTPS port 443 already has a Tailscale Serve or Funnel configuration")
	ErrDefaultCredential = errors.New("change the default admin password before enabling Tailscale Funnel")
	ErrSudoRequired      = errors.New("sudo password is required to install Tailscale")
	ErrWindowsInstall    = errors.New("automatic install is not supported on Windows; download Tailscale from https://tailscale.com/download")
	ErrBrewMissing       = errors.New("Homebrew is required to install Tailscale on macOS; install it from https://brew.sh")

	operationMu sync.Mutex
	recent      struct {
		sync.RWMutex
		message     string
		approvalURL string
	}

	loginURLPattern = regexp.MustCompile(`https://login\.tailscale\.com/[^\s"'<>]+`)
)

type Status struct {
	Supported         bool   `json:"supported"`
	Installed         bool   `json:"installed"`
	DaemonRunning     bool   `json:"daemon_running"`
	LoggedIn          bool   `json:"logged_in"`
	Running           bool   `json:"running"`
	FunnelActive      bool   `json:"funnel_active"`
	ConfigConflict    bool   `json:"config_conflict"`
	DefaultCredential bool   `json:"default_credentials"`
	Phase             Phase  `json:"phase"`
	BinaryPath        string `json:"binary_path,omitempty"`
	TargetURL         string `json:"target_url"`
	PublicURL         string `json:"public_url,omitempty"`
	APIURL            string `json:"api_url,omitempty"`
	ApprovalURL       string `json:"approval_url,omitempty"`
	LastError         string `json:"last_error,omitempty"`

	safeToStop bool
}

type nodeStatus struct {
	BackendState string `json:"BackendState"`
	AuthURL      string `json:"AuthURL"`
	Self         *struct {
		DNSName string `json:"DNSName"`
	} `json:"Self"`
}

type serveConfig struct {
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string `json:"Proxy"`
		} `json:"Handlers"`
	} `json:"Web"`
	AllowFunnel map[string]bool `json:"AllowFunnel"`
}

type funnelInspection struct {
	active         bool
	running        bool
	configConflict bool
	safeToStop     bool
	publicURL      string
}

func GetStatus(ctx context.Context) Status {
	message, approvalURL := recentError()
	status := Status{
		Supported:   supportedPlatform(),
		Phase:       PhaseUnsupported,
		TargetURL:   localTargetURL(),
		LastError:   message,
		ApprovalURL: approvalURL,
	}
	if !status.Supported {
		status.LastError = ErrUnsupported.Error()
		return status
	}

	binaryPath, err := resolveBinary()
	if err != nil {
		status.Phase = PhaseNotInstalled
		status.LastError = err.Error()
		return status
	}
	status.Installed = true
	status.BinaryPath = binaryPath

	output, err := runCLI(ctx, statusTimeout, binaryPath, "status", "--json")
	if err != nil {
		status.LastError = cliError(output, err)
		status.ApprovalURL = extractApprovalURL(status.LastError)
		if isLoginError(status.LastError) {
			status.DaemonRunning = true
			status.Phase = PhaseNeedsLogin
		} else {
			status.Phase = PhaseError
		}
		return status
	}

	var node nodeStatus
	if err := decodeCLIJSON(output, &node); err != nil {
		status.Phase = PhaseError
		status.LastError = fmt.Sprintf("failed to parse tailscale status: %v", err)
		return status
	}
	status.DaemonRunning = true
	if node.BackendState != "Running" {
		if node.BackendState == "NeedsLogin" || node.BackendState == "NoState" {
			status.Phase = PhaseNeedsLogin
			status.LastError = ErrNeedsLogin.Error()
			status.ApprovalURL = node.AuthURL
		} else {
			status.LastError = fmt.Sprintf("tailscale backend is %s", strings.ToLower(node.BackendState))
		}
		return status
	}
	status.LoggedIn = true

	output, err = runCLI(ctx, statusTimeout, binaryPath, "funnel", "status", "--json")
	if err != nil {
		status.Phase = PhaseError
		status.LastError = cliError(output, err)
		status.ApprovalURL = extractApprovalURL(status.LastError)
		return status
	}

	inspection, err := inspectFunnel(output, status.TargetURL)
	if err != nil {
		status.Phase = PhaseError
		status.LastError = fmt.Sprintf("failed to parse Tailscale Funnel status: %v", err)
		return status
	}
	status.FunnelActive = inspection.active
	status.ConfigConflict = inspection.configConflict
	status.Running = inspection.running
	status.safeToStop = inspection.safeToStop
	status.PublicURL = inspection.publicURL
	if status.PublicURL == "" && status.Running && node.Self != nil {
		status.PublicURL = "https://" + strings.TrimSuffix(node.Self.DNSName, ".")
	}
	if status.PublicURL != "" {
		status.APIURL = strings.TrimRight(status.PublicURL, "/") + "/v1"
	}

	status.DefaultCredential = defaultCredentials()
	switch {
	case status.Running && !status.ConfigConflict:
		status.Phase = PhaseRunning
	case status.ConfigConflict:
		status.Phase = PhaseConflict
		if status.LastError == "" {
			status.LastError = ErrConflict.Error()
		}
	case status.DefaultCredential:
		status.Phase = PhaseCredentialsNeeded
		status.LastError = ErrDefaultCredential.Error()
	default:
		status.Phase = PhaseStopped
	}
	return status
}

func Start(ctx context.Context) (Status, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	status := GetStatus(ctx)
	switch {
	case !status.Supported:
		return fail(status, ErrUnsupported)
	case !status.Installed:
		return fail(status, ErrNotInstalled)
	case status.Phase == PhaseNeedsLogin:
		return fail(status, ErrNeedsLogin)
	case status.Phase == PhaseError:
		return fail(status, errors.New(status.LastError))
	case status.DefaultCredential && !status.Running:
		return fail(status, ErrDefaultCredential)
	case status.ConfigConflict:
		return fail(status, ErrConflict)
	case status.Running:
		clearRecentError()
		status.LastError = ""
		status.ApprovalURL = ""
		return status, nil
	}
	output, err := runCLI(
		ctx,
		actionTimeout,
		status.BinaryPath,
		"funnel", "--bg", "--yes", "--https="+publicHTTPSPort, status.TargetURL,
	)
	if err != nil {
		return failWithOutput(ctx, output, err)
	}

	status = GetStatus(ctx)
	if !status.Running {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = "Tailscale Funnel did not start"
		}
		return fail(status, errors.New(limitString(message)))
	}

	clearRecentError()
	status.LastError = ""
	status.ApprovalURL = ""
	return status, nil
}

func Stop(ctx context.Context) (Status, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	status := GetStatus(ctx)
	switch {
	case !status.Supported:
		return fail(status, ErrUnsupported)
	case !status.Installed:
		return fail(status, ErrNotInstalled)
	case status.Phase == PhaseError:
		return fail(status, errors.New(status.LastError))
	case status.ConfigConflict:
		return fail(status, ErrConflict)
	case !status.Running:
		clearRecentError()
		status.LastError = ""
		status.ApprovalURL = ""
		return status, nil
	case !status.safeToStop:
		return fail(status, errors.New("refusing to stop Funnel because HTTPS port 443 has additional handlers"))
	}

	output, err := runCLI(
		ctx,
		actionTimeout,
		status.BinaryPath,
		"funnel", "--bg", "--yes", "--https="+publicHTTPSPort, "off",
	)
	if err != nil {
		return failWithOutput(ctx, output, err)
	}

	status = GetStatus(ctx)
	if status.Running {
		return fail(status, errors.New("Tailscale Funnel is still running"))
	}

	clearRecentError()
	status.LastError = ""
	status.ApprovalURL = ""
	return status, nil
}

func failWithOutput(ctx context.Context, output []byte, commandErr error) (Status, error) {
	message := cliError(output, commandErr)
	setRecentError(message)
	status := GetStatus(ctx)
	status.LastError = message
	status.ApprovalURL = extractApprovalURL(message)
	return status, errors.New(message)
}

func fail(status Status, err error) (Status, error) {
	message := err.Error()
	setRecentError(message)
	status.LastError = message
	status.ApprovalURL = extractApprovalURL(message)
	return status, err
}

func inspectFunnel(data []byte, target string) (funnelInspection, error) {
	var config serveConfig
	if err := decodeCLIJSON(data, &config); err != nil {
		return funnelInspection{}, err
	}

	activeEntries := 0
	for hostPort, enabled := range config.AllowFunnel {
		if enabled && hostPortUsesPublicPort(hostPort) {
			activeEntries++
		}
	}

	webEntries := 0
	matchingEntries := 0
	matchingSafe := false
	matchingPublicURL := ""
	for hostPort, web := range config.Web {
		if !hostPortUsesPublicPort(hostPort) || len(web.Handlers) == 0 {
			continue
		}
		webEntries++
		root, ok := web.Handlers["/"]
		if !ok || !config.AllowFunnel[hostPort] || !sameTarget(root.Proxy, target) {
			continue
		}
		matchingEntries++
		matchingSafe = len(web.Handlers) == 1
		matchingPublicURL = publicURL(hostPort)
	}

	exactOwnership := activeEntries == 1 && webEntries == 1 && matchingEntries == 1 && matchingSafe
	occupied := activeEntries > 0 || webEntries > 0
	if !exactOwnership {
		matchingPublicURL = ""
	}
	return funnelInspection{
		active:         activeEntries > 0,
		running:        exactOwnership,
		configConflict: occupied && !exactOwnership,
		safeToStop:     exactOwnership,
		publicURL:      matchingPublicURL,
	}, nil
}

func hostPortUsesPublicPort(hostPort string) bool {
	_, port, err := net.SplitHostPort(hostPort)
	return err == nil && port == publicHTTPSPort
}

func decodeCLIJSON(data []byte, target any) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		data = []byte("{}")
	}
	start := bytes.IndexByte(data, '{')
	end := bytes.LastIndexByte(data, '}')
	if start < 0 || end < start {
		return errors.New("JSON object not found")
	}
	return json.Unmarshal(data[start:end+1], target)
}

func sameTarget(left, right string) bool {
	return normalizeTarget(left) != "" && normalizeTarget(left) == normalizeTarget(right)
}

func normalizeTarget(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if _, err := strconv.ParseUint(value, 10, 16); err == nil {
		value = "http://127.0.0.1:" + value
	} else if !strings.Contains(value, "://") {
		value = "http://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" || parsed.Port() == "" {
		return ""
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" {
		host = "127.0.0.1"
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	return strings.ToLower(parsed.Scheme) + "://" + net.JoinHostPort(host, parsed.Port()) + path
}

func publicURL(hostPort string) string {
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return ""
	}
	host = strings.TrimSuffix(host, ".")
	if port == publicHTTPSPort {
		return "https://" + host
	}
	return "https://" + net.JoinHostPort(host, port)
}

func localTargetURL() string {
	host := strings.TrimSpace(conf.AppConfig.Server.Host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	host = strings.Trim(host, "[]")
	return "http://" + net.JoinHostPort(host, strconv.Itoa(conf.AppConfig.Server.Port))
}

func defaultCredentials() bool {
	return hasDefaultCredentials(op.UserGet())
}

func hasDefaultCredentials(user model.User) bool {
	return user.Username == "admin" && user.ComparePassword("admin") == nil
}

func supportedPlatform() bool {
	return runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows"
}

func resolveBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv(envBinaryPath)); configured != "" {
		info, err := os.Stat(configured)
		if err != nil {
			return "", fmt.Errorf("%s is invalid: %w", envBinaryPath, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s points to a directory", envBinaryPath)
		}
		return configured, nil
	}

	if path, err := exec.LookPath("tailscale"); err == nil {
		return path, nil
	}

	candidates := []string{
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/bin/tailscale",
		"/snap/bin/tailscale",
	}
	if runtime.GOOS == "windows" {
		candidates = []string{`C:\Program Files\Tailscale\tailscale.exe`}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", ErrNotInstalled
}

func runCLI(parent context.Context, timeout time.Duration, binaryPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, binaryPath, args...).CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output, errors.New("tailscale command timed out")
	}
	return output, err
}

func cliError(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" && err != nil {
		message = err.Error()
	}
	return limitString(message)
}

func limitString(value string) string {
	if len(value) <= maxErrorLength {
		return value
	}
	return value[:maxErrorLength] + "…"
}

func isLoginError(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "logged out") || strings.Contains(message, "needs login")
}

func extractApprovalURL(message string) string {
	match := loginURLPattern.FindString(message)
	return strings.TrimRight(match, ".,;)")
}

func setRecentError(message string) {
	recent.Lock()
	recent.message = message
	recent.approvalURL = extractApprovalURL(message)
	recent.Unlock()
}

func clearRecentError() {
	recent.Lock()
	recent.message = ""
	recent.approvalURL = ""
	recent.Unlock()
}

func recentError() (string, string) {
	recent.RLock()
	defer recent.RUnlock()
	return recent.message, recent.approvalURL
}

// Install 自动安装 Tailscale 并启动 tailscaled。sudo 密码只通过
// stdin 传给本次 sudo 调用，不写入日志、不落盘、不保存。
func Install(ctx context.Context, sudoPassword string) (Status, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	status := GetStatus(ctx)
	switch {
	case !status.Supported:
		return fail(status, ErrUnsupported)
	case status.Installed:
		// 已安装也要确保守护进程在跑（上次安装可能启动失败）。
		if err := ensureDaemon(ctx, sudoPassword); err != nil {
			return fail(GetStatus(ctx), err)
		}
		return GetStatus(ctx), nil
	case runtime.GOOS == "windows":
		return fail(status, ErrWindowsInstall)
	case os.Geteuid() != 0 && strings.TrimSpace(sudoPassword) == "":
		return fail(status, ErrSudoRequired)
	}

	var output []byte
	var err error
	if runtime.GOOS == "darwin" {
		output, err = installMac(ctx, sudoPassword)
	} else {
		output, err = installLinux(ctx, sudoPassword)
	}
	if err != nil {
		// 安装脚本在无 init 的环境（容器）里最后一步启动服务会失败，
		// 但软件包往往已装好；以 CLI 是否可用为准。
		if _, lookErr := resolveBinary(); lookErr == nil {
			err = nil
		}
	}

	if err == nil {
		// 清空安装日志，避免后续守护进程的错误被安装输出掩盖。
		output = nil
		err = ensureDaemon(ctx, sudoPassword)
	}

	status = GetStatus(ctx)
	if err != nil {
		message := err.Error()
		if len(output) > 0 {
			message = cliError(output, err)
		}
		setRecentError(message)
		status.LastError = message
		return status, errors.New(message)
	}

	clearRecentError()
	status.LastError = ""
	status.ApprovalURL = ""
	return status, nil
}

// Login 在后台发起 tailscale login，授权链接通过 Status.ApprovalURL 返回。
func Login(ctx context.Context) (Status, error) {
	operationMu.Lock()
	defer operationMu.Unlock()

	status := GetStatus(ctx)
	switch {
	case !status.Supported:
		return fail(status, ErrUnsupported)
	case !status.Installed:
		return fail(status, ErrNotInstalled)
	case status.LoggedIn:
		return status, nil
	case !status.DaemonRunning:
		return fail(status, errors.New("tailscaled is not running"))
	}

	cmd := exec.Command(status.BinaryPath, "login")
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fail(status, err)
	}

	// 授权可能需要几分钟，后台等待，避免阻塞接口；前台只等 AuthURL 出现。
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		status = GetStatus(ctx)
		if status.LoggedIn || status.ApprovalURL != "" {
			break
		}
	}

	if status.ApprovalURL == "" && !status.LoggedIn {
		return fail(status, errors.New("tailscale did not return a login URL; run 'tailscale login' manually"))
	}
	return status, nil
}

func installLinux(ctx context.Context, password string) ([]byte, error) {
	fetch := "curl -fsSL"
	if _, err := exec.LookPath("curl"); err != nil {
		fetch = "wget -qO-"
	}
	return runPrivileged(ctx, installTimeout, password, true, "sh", "-c", fetch+" https://tailscale.com/install.sh | sh")
}

func installMac(ctx context.Context, password string) ([]byte, error) {
	brew, err := findBrew()
	if err != nil {
		return nil, err
	}
	output, err := runPrivileged(ctx, installTimeout, password, false, brew, "install", "tailscale")
	if err != nil {
		return output, err
	}
	// 注册 launchd 系统守护进程。
	binaryPath, err := resolveBinary()
	if err != nil {
		return nil, err
	}
	return runPrivileged(ctx, actionTimeout, password, true, resolveDaemon(binaryPath), "install-system-daemon")
}

func ensureDaemon(ctx context.Context, password string) error {
	binaryPath, err := resolveBinary()
	if err != nil {
		return err
	}
	if daemonResponds(ctx, binaryPath) {
		return nil
	}

	if hasSystemd() {
		output, err := runPrivileged(ctx, actionTimeout, password, true, "systemctl", "enable", "--now", "tailscaled")
		if err != nil {
			return errors.New(cliError(output, err))
		}
	} else {
		daemon := resolveDaemon(binaryPath)
		args := []string{"--statedir=/var/lib/tailscale"}
		if _, statErr := os.Stat("/dev/net/tun"); statErr != nil {
			args = append(args, "--tun=userspace-networking")
		}
		if err := startDaemonDetached(daemon, password, args...); err != nil {
			return err
		}
	}

	for i := 0; i < 20; i++ {
		time.Sleep(500 * time.Millisecond)
		if daemonResponds(ctx, binaryPath) {
			return nil
		}
	}
	return errors.New("tailscaled did not start")
}

func daemonResponds(ctx context.Context, binaryPath string) bool {
	output, err := runCLI(ctx, statusTimeout, binaryPath, "status", "--json")
	return err == nil || isLoginError(cliError(output, err))
}

// resolveDaemon 找 tailscaled：先看 CLI 同目录，再查 PATH 和常见 sbin 位置。
// Alpine 等发行版把 tailscale 放 /usr/bin、tailscaled 放 /usr/sbin。
func resolveDaemon(binaryPath string) string {
	sibling := filepath.Join(filepath.Dir(binaryPath), "tailscaled")
	if info, err := os.Stat(sibling); err == nil && !info.IsDir() {
		return sibling
	}
	if path, err := exec.LookPath("tailscaled"); err == nil {
		return path
	}
	for _, candidate := range []string{"/usr/sbin/tailscaled", "/usr/local/sbin/tailscaled", "/sbin/tailscaled"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return sibling
}

// startDaemonDetached 以后台方式拉起 tailscaled，进程脱离 Octopus 生命周期。
func startDaemonDetached(daemon, password string, args ...string) error {
	name := daemon
	if os.Geteuid() != 0 {
		args = append([]string{"-S", "-p", "", daemon}, args...)
		name = "sudo"
	}
	cmd := exec.Command(name, args...)
	if name == "sudo" {
		cmd.Stdin = strings.NewReader(password + "\n")
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// runPrivileged 在需要 root 且当前非 root 时经 sudo -S 执行，密码只走 stdin。
func runPrivileged(ctx context.Context, timeout time.Duration, password string, needsRoot bool, name string, args ...string) ([]byte, error) {
	if needsRoot && os.Geteuid() != 0 {
		args = append([]string{"-S", "-p", "", name}, args...)
		name = "sudo"
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	if name == "sudo" {
		cmd.Stdin = strings.NewReader(password + "\n")
	}
	output, err := cmd.CombinedOutput()
	if errors.Is(cctx.Err(), context.DeadlineExceeded) {
		return output, errors.New("command timed out")
	}
	return output, err
}

func hasSystemd() bool {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}

func findBrew() (string, error) {
	if path, err := exec.LookPath("brew"); err == nil {
		return path, nil
	}
	for _, candidate := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", ErrBrewMissing
}
