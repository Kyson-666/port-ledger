// Package config 读取并校验全部环境变量配置。
// 所有取值与设计稿 §8.4 的 compose 环境变量一一对应。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 是进程的完整运行配置。
type Config struct {
	AdminPort     int // 容器内监听端口，默认 443
	TLSSelfSigned bool
	TLSCert       string
	TLSKey        string

	DataDir   string // 数据目录，默认 /data
	LogLevel  string // debug/info/warn/error
	LogDir    string // 派生：<DataDir>/logs
	BackupDir string // 派生：<DataDir>/backups
	DBPath    string // 派生：<DataDir>/port-ledger.db

	ScanHostProc     bool
	ScanDockerSocket bool
	ScanComposeDir   string
	ScanRanges       string
	ScanOnStart      bool
	ScanInterval     time.Duration

	ProcRoot        string // 只读挂载的宿主 /proc，默认 /host/proc
	DockerHost      string // 默认 unix:///var/run/docker.sock
	SelfContainer   string // 本工具容器名，用于判定 self 状态
	ScanTargetHost  string // 主动扫描目标（bridge 模式下 127.0.0.1 只代表容器自身，默认禁用）
	ActiveTimeoutMS int

	Version string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func getbool(key string, def bool) bool {
	v := strings.ToLower(getenv(key, ""))
	switch v {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func getint(key string, def int) (int, error) {
	s := getenv(key, "")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("环境变量 %s=%q 不是合法整数: %w", key, s, err)
	}
	return n, nil
}

// Load 读取配置；任一非法取值直接返回错误，避免带病启动。
func Load() (*Config, error) {
	c := &Config{Version: Version}

	var err error
	if c.AdminPort, err = getint("ADMIN_PORT", 443); err != nil {
		return nil, err
	}
	if c.AdminPort < 1 || c.AdminPort > 65535 {
		return nil, fmt.Errorf("ADMIN_PORT 超出 1-65535: %d", c.AdminPort)
	}

	c.TLSSelfSigned = getbool("TLS_SELF_SIGNED", false)
	c.TLSCert = getenv("TLS_CERT", "")
	c.TLSKey = getenv("TLS_KEY", "")

	c.DataDir = getenv("DATA_DIR", "/data")
	c.LogLevel = strings.ToLower(getenv("LOG_LEVEL", "info"))
	c.LogDir = filepath.Join(c.DataDir, "logs")
	c.BackupDir = filepath.Join(c.DataDir, "backups")
	c.DBPath = filepath.Join(c.DataDir, "port-ledger.db")

	c.ScanHostProc = getbool("SCAN_HOST_PROC", true)
	c.ScanDockerSocket = getbool("SCAN_DOCKER_SOCKET", true)
	c.ScanComposeDir = getenv("SCAN_COMPOSE_DIR", "/compose")
	c.ScanRanges = getenv("SCAN_RANGES", "1-65535")
	c.ScanOnStart = getbool("SCAN_ON_START", true)

	sec, err := getint("SCAN_INTERVAL", 300)
	if err != nil {
		return nil, err
	}
	c.ScanInterval = time.Duration(sec) * time.Second

	c.ProcRoot = getenv("PROC_ROOT", "/host/proc")
	c.DockerHost = getenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	c.SelfContainer = getenv("SELF_CONTAINER_NAME", "port-ledger")
	c.ScanTargetHost = getenv("SCAN_TARGET_HOST", "")

	if c.ActiveTimeoutMS, err = getint("ACTIVE_SCAN_TIMEOUT_MS", 300); err != nil {
		return nil, err
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("LOG_LEVEL 仅支持 debug/info/warn/error，收到 %q", c.LogLevel)
	}
	if _, err := ParseRanges(c.ScanRanges); err != nil {
		return nil, err
	}
	return c, nil
}

// ParseRanges 解析形如 "1-65535" 或 "1-1024,8000-8100" 的端口范围。
func ParseRanges(s string) ([]Range, error) {
	var out []Range
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, err := parseRange(part)
		if err != nil {
			return nil, err
		}
		out = append(out, Range{Lo: lo, Hi: hi})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("SCAN_RANGES 为空")
	}
	return out, nil
}

func parseRange(p string) (int, int, error) {
	parts := strings.SplitN(p, "-", 2)
	lo, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("非法端口范围 %q", p)
	}
	hi := lo
	if len(parts) == 2 {
		hi, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return 0, 0, fmt.Errorf("非法端口范围 %q", p)
		}
	}
	if lo < 1 || hi > 65535 || lo > hi {
		return 0, 0, fmt.Errorf("端口范围越界或倒置: %q", p)
	}
	return lo, hi, nil
}

// Range 是闭区间端口范围。
type Range struct{ Lo, Hi int }
