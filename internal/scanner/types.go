// Package scanner 提供四类只读探针：宿主监听表、Docker API、Compose 声明、主动扫描。
package scanner

import "time"

// Listener 是宿主监听表（/proc/net/*）中的一条记录。
type Listener struct {
	Port        int
	Protocol    string // tcp / udp
	BindAddr    string
	PID         int
	ProcessName string
	Cmdline     string
	Inode       string
}

// ContainerPort 是 Docker 发布的一条端口映射。
type ContainerPort struct {
	Port          int
	Protocol      string
	BindAddr      string
	ContainerID   string
	ContainerName string
	Image         string
	IsSelf        bool
}

// ComposePort 是 compose 文件中声明的一条端口。
type ComposePort struct {
	Port     int
	Protocol string
	File     string
	Service  string
}

// ActiveResult 是主动扫描的结论。
type ActiveResult struct {
	Port  int
	Proto string
	Open  bool
}

// Health 记录单个数据源的执行状态，用于 /api/health 与降级提示。
type Health struct {
	Name    string    `json:"name"`
	Enabled bool      `json:"enabled"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Count   int       `json:"count"`
	At      time.Time `json:"at"`
}
