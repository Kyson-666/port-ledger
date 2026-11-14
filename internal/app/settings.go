package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/kyson-666/port-ledger/internal/config"
)

// RestartRequiredKeys 列出需要重启容器才生效的设置键。
var RestartRequiredKeys = []string{"admin_port", "tls_self_signed"}

// SettingKeys 是设置键的白名单。
// 白名单外的键一律拒绝保存：既防止前端误包层级（如 {"values":{...}}）产生的
// 垃圾键落库并在备份中传播，也保证导入的设置不引入未知行为。
var SettingKeys = map[string]bool{
	"scan_ranges":        true,
	"scan_interval":      true,
	"scan_on_start":      true,
	"scan_host_proc":     true,
	"scan_docker_socket": true,
	"scan_compose_dir":   true,
	"admin_port":         true,
	"tls_self_signed":    true,
}

// SettingDefaults 返回启动时环境变量基准值（只读展示用）。
// 取 envCfg 快照而非 Cfg：Cfg 会被 DB 设置覆盖，不能代表「默认」。
func (a *App) SettingDefaults() map[string]any {
	return map[string]any{
		"scan_ranges":        a.envCfg.ScanRanges,
		"scan_interval":      int(a.envCfg.ScanInterval.Seconds()),
		"scan_on_start":      a.envCfg.ScanOnStart,
		"scan_host_proc":     a.envCfg.ScanHostProc,
		"scan_docker_socket": a.envCfg.ScanDockerSocket,
		"scan_compose_dir":   a.envCfg.ScanComposeDir,
		"admin_port":         a.envCfg.AdminPort,
		"tls_self_signed":    a.envCfg.TLSSelfSigned,
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
		if !SettingKeys[k] {
			continue // 历史脏数据不出现在对外视图里
		}
		switch k {
		case "scan_interval":
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
				continue
			}
		case "admin_port":
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

// ApplySettings 把 DB 中的设置叠加到运行时配置。
// admin_port 与 tls_self_signed 不会改变本进程的监听行为（它们由环境变量决定，
// 修改需重建容器），因此这里刻意跳过；保存时仍然校验它们的合法性。
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
		if err != nil || n < 0 || n > 86400 {
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
	if v, ok := db["scan_compose_dir"]; ok && v != "" {
		a.Cfg.ScanComposeDir = v
	}
	return nil
}

// ResetSettingsToEnv 把运行配置恢复为启动时环境变量基准。
// 由「恢复默认」调用：DB 设置已清空，Cfg 里残留的内存覆盖值也一并还原，
// 否则 PUT 过的 scan_interval 会在重置后继续生效。
func (a *App) ResetSettingsToEnv() {
	a.Cfg.ScanRanges = a.envCfg.ScanRanges
	a.Cfg.ScanInterval = a.envCfg.ScanInterval
	a.Cfg.ScanOnStart = a.envCfg.ScanOnStart
	a.Cfg.ScanHostProc = a.envCfg.ScanHostProc
	a.Cfg.ScanDockerSocket = a.envCfg.ScanDockerSocket
	a.Cfg.ScanComposeDir = a.envCfg.ScanComposeDir
}

// ValidateLedgerSettingsValues 校验备份文件携带的设置值（键白名单由 API 层过滤）。
// 与 ValidateSettings 不同：这里的值已是字符串形态（来自导出文件）。
func ValidateLedgerSettingsValues(m map[string]string) error {
	for k, v := range m {
		switch k {
		case "scan_interval":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 86400 {
				return fmt.Errorf("备份中的 scan_interval 非法: %q", v)
			}
		case "admin_port":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 65535 {
				return fmt.Errorf("备份中的 admin_port 非法: %q", v)
			}
		case "scan_ranges":
			if _, err := config.ParseRanges(v); err != nil {
				return fmt.Errorf("备份中的 scan_ranges 非法: %q", v)
			}
		}
	}
	return nil
}

// ValidateSettings 校验待保存的设置：键必须在白名单内，值需合法。
func ValidateSettings(m map[string]any) error {
	for k := range m {
		if k == "actor" {
			continue
		}
		if !SettingKeys[k] {
			return fmt.Errorf("未知设置项 %q；可修改的项：scan_ranges / scan_interval / scan_on_start / scan_host_proc / scan_docker_socket / scan_compose_dir / admin_port / tls_self_signed", k)
		}
	}
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
		default:
			return fmt.Errorf("scan_interval 需为数字")
		}
	}
	if v, ok := m["admin_port"]; ok {
		switch t := v.(type) {
		case float64:
			if t != float64(int(t)) || t < 1 || t > 65535 {
				return fmt.Errorf("admin_port 需在 1-65535 之间")
			}
		case string:
			n, err := strconv.Atoi(t)
			if err != nil || n < 1 || n > 65535 {
				return fmt.Errorf("admin_port 需在 1-65535 之间")
			}
		default:
			return fmt.Errorf("admin_port 需为数字")
		}
	}
	if v, ok := m["scan_compose_dir"]; ok {
		if s, _ := v.(string); len(s) > 512 {
			return fmt.Errorf("scan_compose_dir 过长")
		}
	}
	return nil
}
