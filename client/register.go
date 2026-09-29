package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const agentVersion = "0.3.0"

type registerRequest struct {
	BindCode     string `json:"bind_code,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	DeviceUID    string `json:"device_uid"`
	Name         string `json:"name"`
	OSVersion    string `json:"os_version"`
	AgentVersion string `json:"agent_version"`
}

func deviceUID() string {
	h, _ := os.Hostname()
	return h + "-" + shortMachineID()
}

func shortMachineID() string {
	h, _ := os.Hostname()
	if len(h) > 8 {
		return h[len(h)-8:]
	}
	return h
}

// normalizeServerURL ensures a usable API base like http://host:8080
func normalizeServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return "", fmt.Errorf("请填写服务器地址，例如 http://192.168.1.10:8080")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("服务器地址无效: %s（示例 http://192.168.1.10:8080）", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("仅支持 http/https，当前: %s", u.Scheme)
	}
	// Common mistake: pointing at Vite client/admin ports
	switch u.Port() {
	case "5173", "5174":
		return "", fmt.Errorf("当前地址是前端端口 %s，请改为 API 端口（默认 8080），例如 http://%s:8080", u.Port(), u.Hostname())
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func decodeAPIResponse(raw []byte, status int, dest interface{}) error {
	trim := bytes.TrimSpace(raw)
	if len(trim) == 0 {
		return fmt.Errorf("服务器返回空响应 (HTTP %d)，请确认 API 已启动且地址正确", status)
	}
	if trim[0] == '<' {
		snippet := string(trim)
		if len(snippet) > 120 {
			snippet = snippet[:120] + "..."
		}
		return fmt.Errorf("服务器返回了网页而不是 API JSON (HTTP %d)。请把「服务器地址」设为 Remote API，例如 http://服务器IP:8080（不要用 5173/5174 前端端口）。预览: %s", status, snippet)
	}
	if err := json.Unmarshal(trim, dest); err != nil {
		snippet := string(trim)
		if len(snippet) > 160 {
			snippet = snippet[:160] + "..."
		}
		return fmt.Errorf("解析 API 响应失败 (HTTP %d): %v；内容: %s", status, err, snippet)
	}
	return nil
}

func registerDevice(serverURL, bindCode, username, password string) (*Config, error) {
	base, err := normalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	uid := deviceUID()
	body := registerRequest{
		BindCode:     bindCode,
		Username:     username,
		Password:     password,
		DeviceUID:    uid,
		Name:         hostname,
		OSVersion:    runtime.GOOS + "/" + runtime.GOARCH,
		AgentVersion: agentVersion,
	}
	b, _ := json.Marshal(body)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/devices/register", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接服务器 %s: %v（请检查 IP/端口、防火墙，并确认本机可访问该 API）", base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}
	prev := getConfig()
	return saveDeviceRegistration(base, resp.StatusCode, raw, prev.AccessToken, prev.RefreshToken)
}

func registerDeviceWithUserToken(serverURL, accessToken string) (*Config, error) {
	base, err := normalizeServerURL(serverURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("未登录")
	}
	hostname, _ := os.Hostname()
	body := registerRequest{
		DeviceUID:    deviceUID(),
		Name:         hostname,
		OSVersion:    runtime.GOOS + "/" + runtime.GOARCH,
		AgentVersion: agentVersion,
	}
	b, _ := json.Marshal(body)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/devices/register-self", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法连接服务器 %s: %v", base, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}
	prev := getConfig()
	return saveDeviceRegistration(base, resp.StatusCode, raw, accessToken, prev.RefreshToken)
}

func saveDeviceRegistration(base string, status int, raw []byte, accessToken, refreshToken string) (*Config, error) {
	var out struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			DeviceToken string `json:"device_token"`
			Device      struct {
				DeviceUID string `json:"device_uid"`
				Name      string `json:"name"`
			} `json:"device"`
		} `json:"data"`
	}
	if err := decodeAPIResponse(raw, status, &out); err != nil {
		return nil, err
	}
	if status >= 400 && out.Message == "" {
		return nil, fmt.Errorf("注册失败 HTTP %d", status)
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("%s", out.Message)
	}
	if out.Data.DeviceToken == "" {
		return nil, fmt.Errorf("服务器未返回 device_token，请确认对接的是 Remote API")
	}
	prev := getConfig()
	cfg := &Config{
		ServerURL:    base,
		DeviceToken:  out.Data.DeviceToken,
		DeviceUID:    out.Data.Device.DeviceUID,
		DeviceName:   out.Data.Device.Name,
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AutoStart:    prev.AutoStart,
		AutoConnect:  true,
		UIPort:       prev.UIPort,
	}
	if err := saveConfig(cfgPath, cfg); err != nil {
		return nil, err
	}
	setConfig(cfg)
	return cfg, nil
}
