package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the Wails API exposed to the frontend.
type App struct {
	ctx       context.Context
	allowQuit atomic.Bool
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if isSessionWindow() {
		startSessionHotkeyHook(ctx)
		return
	}
	startTray(a)
	cfg := getConfig()
	if cfg.AccessToken != "" {
		if _, err := applyUserSession(cfg.AccessToken, cfg.RefreshToken); err != nil {
			LogWarn("restore user session: %v", err)
		}
	}
}

func (a *App) shutdown(ctx context.Context) {
	stopTray()
	stopSessionHotkeyHook()
	if !isSessionWindow() {
		runtimeAgent.Stop()
	}
}

func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if isSessionWindow() || a.allowQuit.Load() {
		return false
	}
	runtime.WindowHide(ctx)
	return true
}

func (a *App) GetWindowMode() map[string]interface{} {
	if isSessionWindow() {
		return map[string]interface{}{
			"kind":       "session",
			"session_id": sessionWindow.SessionID,
			"token":      sessionWindow.Token,
			"server_url": sessionWindow.ServerURL,
		}
	}
	return map[string]interface{}{
		"kind":       "main",
		"server_url": getConfig().ServerURL,
	}
}

func (a *App) GetStatus() map[string]interface{} {
	st := runtimeAgent.Status()
	st["auto_start"] = IsAutoStartEnabled()
	st["log_path"] = GetLogFilePath()
	st["service_status"] = serviceStatusText()
	st["service_ready"] = isInputServiceReachable()
	st["locked"] = isWorkstationLocked()
	st["window_kind"] = "main"
	if isSessionWindow() {
		st["window_kind"] = "session"
	}
	return st
}

func (a *App) SetServerURL(server string) (string, error) {
	normalized, err := normalizeServerURL(server)
	if err != nil {
		return "", err
	}
	cfg := getConfig()
	cfg.ServerURL = normalized
	setConfig(&cfg)
	if err := saveConfig(cfgPath, &cfg); err != nil {
		return "", err
	}
	return normalized, nil
}

func (a *App) OpenSessionWindow(sessionID string, token string) error {
	id, err := strconv.ParseUint(sessionID, 10, 64)
	if err != nil || id == 0 {
		return fmt.Errorf("无效会话")
	}
	if token == "" {
		return fmt.Errorf("未登录")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	cfg := getConfig()
	cmd := exec.Command(exe,
		"-window", "session",
		"-session", strconv.FormatUint(id, 10),
		"-token", token,
		"-server", cfg.ServerURL,
		"-config", cfgPath,
	)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		LogErrorErr("open session window", err)
		return err
	}
	LogInfo("opened session window id=%d pid=%d", id, cmd.Process.Pid)
	return nil
}

func (a *App) CloseWindow() {
	a.allowQuit.Store(true)
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) ApplyUserSession(accessToken, refreshToken string) (map[string]interface{}, error) {
	st, err := applyUserSession(accessToken, refreshToken)
	if err != nil {
		return nil, err
	}
	return st, nil
}

func (a *App) ClearUserSession() {
	clearUserSession()
}

func applyUserSession(accessToken, refreshToken string) (map[string]interface{}, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("未登录")
	}
	cfg := getConfig()
	if refreshToken == "" {
		refreshToken = cfg.RefreshToken
	}
	cfg.AccessToken = accessToken
	cfg.RefreshToken = refreshToken
	setConfig(&cfg)
	_ = saveConfig(cfgPath, &cfg)

	wasRunning := runtimeAgent.running.Load()
	runtimeAgent.Stop()
	if _, err := registerDeviceWithUserToken(cfg.ServerURL, accessToken); err != nil {
		LogErrorErr("register with user session", err)
		return nil, err
	}
	LogInfo("this PC bound to logged-in user")
	if wasRunning || getConfig().AutoConnect {
		if err := runtimeAgent.Start(); err != nil {
			LogErrorErr("start agent after login", err)
			return nil, err
		}
	}
	return runtimeAgent.Status(), nil
}

func clearUserSession() {
	runtimeAgent.Stop()
	cfg := getConfig()
	cfg.AccessToken = ""
	cfg.RefreshToken = ""
	cfg.DeviceToken = ""
	setConfig(&cfg)
	_ = saveConfig(cfgPath, &cfg)
	LogInfo("user session cleared, agent stopped")
}

func (a *App) Start() error {
	if err := runtimeAgent.Start(); err != nil {
		LogErrorErr("start agent", err)
		return err
	}
	LogInfo("agent start requested")
	return nil
}

func (a *App) Stop() {
	runtimeAgent.Stop()
	LogInfo("agent stopped by user")
}

type BindRequest struct {
	ServerURL string `json:"server_url"`
	BindCode  string `json:"bind_code"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

func (a *App) Bind(req BindRequest) (map[string]interface{}, error) {
	server := req.ServerURL
	if server == "" {
		server = getConfig().ServerURL
	}
	wasRunning := runtimeAgent.running.Load()
	runtimeAgent.Stop()
	if _, err := registerDevice(server, req.BindCode, req.Username, req.Password); err != nil {
		LogErrorErr("bind failed", err)
		return nil, err
	}
	LogInfo("device bound successfully server=%s", server)
	if wasRunning || getConfig().AutoConnect {
		if err := runtimeAgent.Start(); err != nil {
			LogErrorErr("auto start after bind", err)
		}
	}
	return a.GetStatus(), nil
}

func (a *App) SetAutoStart(enable bool) (map[string]interface{}, error) {
	if err := SetAutoStart(enable); err != nil {
		LogErrorErr("set autostart", err)
		return nil, err
	}
	cfg := getConfig()
	cfg.AutoStart = enable
	_ = saveConfig(cfgPath, &cfg)
	setConfig(&cfg)
	LogInfo("autostart=%v", enable)
	return map[string]interface{}{"auto_start": IsAutoStartEnabled()}, nil
}

func (a *App) GetRecentLogs(limit int) []string {
	return GetRecentLogs(limit)
}

func (a *App) GetLogPath() string {
	return GetLogFilePath()
}

func (a *App) OpenLogFolder() error {
	return OpenLogFolder()
}

func (a *App) ClearRecentLogs() {
	ClearRecentLogs()
	LogInfo("recent error buffer cleared")
}

func (a *App) InstallInputService() error {
	LogInfo("request install input service (UAC)")
	return InstallInputServiceElevated()
}

func (a *App) UninstallInputService() error {
	LogInfo("request uninstall input service (UAC)")
	return UninstallInputServiceElevated()
}
