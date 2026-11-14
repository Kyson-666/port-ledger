package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/kyson-666/port-ledger/internal/config"
)

// RestartRequiredKeys 列出需要重启容器才生效的设置键。
var RestartRequiredKeys = []string{"admin_port", "tls_self_signed"}

// SettingDefaults 返回环境变量提供的初始值（只读展示用）。
func (a *App) SettingDefaults() map[string]any {
	return map[string]any{
		"scan_ranges":        a.Cfg.ScanRanges,
		"scan_interval":      int(a.Cfg.ScanInterval.Seconds()),
		"scan_on_start":      a.Cfg.ScanOnStart,
		"scan_host_proc":     a.Cfg.ScanHostProc,
		"scan_docker_socket": a.Cfg.ScanDockerSocket,
		"scan_compose_dir":   a.Cfg.ScanComposeDir,
		"admin_port":         a.Cfg.AdminPort,
		"tls_self_signed":    a.Cfg.TLSSelfSigned,
		"webhook":            "",
	}
}

// EffectiveSettings 返回当前生效设置（DB 覆盖优先，未设置则回退环境变量）。
func (a *App) EffectiveSettings() (map[string]any, error) {
	db, err := a.Store.AllSettings()
	if err != nil {
		return nil, err
	}
	out := a.SettingDefaults()
	for k, v := range db {
		switch k {
		case "scan_interval":
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
				continue
			}
		case "scan_on_start", "scan_host_proc", "scan_docker_socket", "tls_self_signed":
			out[k] = v == "true" || v == "1"
			continue
		}
		out[k] = v
	}
	return out, nil
}

// ApplySettings 把 DB 中的设置叠加到运行时配置（重启项除外）。
func (a *App) ApplySettings() error {
	db, err := a.Store.AllSettings()
	if err != nil {
		return err
	}
	if v, ok := db["scan_ranges"]; ok && v != "" {
		if _, err := config.ParseRanges(v); err != nil {
			return err
		}
		a.Cfg.ScanRanges = v
	}
	if v, ok := db["scan_interval"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("scan_interval 非法: %q", v)
		}
		a.Cfg.ScanInterval = time.Duration(n) * time.Second
	}
	if v, ok := db["scan_on_start"]; ok {
		a.Cfg.ScanOnStart = v == "true" || v == "1"
	}
	if v, ok := db["scan_host_proc"]; ok {
		a.Cfg.ScanHostProc = v == "true" || v == "1"
	}
	if v, ok := db["scan_docker_socket"]; ok {
		a.Cfg.ScanDockerSocket = v == "true" || v == "1"
	}
	if v, ok := db["scan_compose_dir"]; ok {
		a.Cfg.ScanComposeDir = v
	}
	return nil
}

// ValidateSettings 校验待保存的设置。
func ValidateSettings(m map[string]any) error {
	if v, ok := m["scan_ranges"]; ok {
		s, _ := v.(string)
		if s != "" {
			if _, err := config.ParseRanges(s); err != nil {
				return err
			}
		}
	}
	if v, ok := m["scan_interval"]; ok {
		switch t := v.(type) {
		case float64:
			if t < 0 || t > 86400 {
				return fmt.Errorf("scan_interval 需在 0-86400 秒之间")
			}
		case string:
			n, err := strconv.Atoi(t)
			if err != nil || n < 0 || n > 86400 {
				return fmt.Errorf("scan_interval 需在 0-86400 秒之间")
			}
		}
	}
	if v, ok := m["scan_compose_dir"]; ok {
		if s, _ := v.(string); len(s) > 512 {
			return fmt.Errorf("scan_compose_dir 过长")
		}
	}
	return nil
}
