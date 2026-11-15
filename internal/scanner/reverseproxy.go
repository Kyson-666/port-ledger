package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// ReverseProxyScanner 解析群晖 DSM 的反向代理规则文件，抽取对外暴露的端口。
//
// DSM 控制面板 → 登录门户 → 高级 → 反向代理 的每条规则最终落在
// /usr/syno/etc/www/ReverseProxy.json（DSM 6/7 通用，UI 修改会即时重写该文件）。
// 规则结构（键为 UUID）：
//
//	{ "<uuid>": {
//	    "frontend": { "fqdn": "...", "port": 8443, "protocol": 1, "https": {...} },
//	    "backend":  { "fqdn": "localhost", "port": 5000, "protocol": 0 },
//	    "description": "备注" }, ..., "version": 2 }
//
// protocol: 0=HTTP, 1=HTTPS。frontend.port 就是映射到外网的端口。
// 容器内需把宿主 /usr/syno/etc/www 只读挂载到 ReverseProxyRoot（默认 /host/syno-www）。
type ReverseProxyScanner struct {
	// Candidates 是候选文件路径（容器内路径），按序尝试，第一个可读的生效。
	// 典型值：[/host/syno-www/ReverseProxy.json]
	Candidates []string
}

// ReverseProxyRule 是一条反代规则解析后的结果。
type ReverseProxyRule struct {
	Port        int    // 对外端口（frontend.port）
	Protocol    string // tcp（nginx 监听恒为 TCP）
	HTTPS       bool   // 来源协议是否为 HTTPS
	Host        string // 来源主机名（fqdn，可为空 = 通配）
	BackendPort int    // 目的地端口
	BackendHost string // 目的地主机
	Description string // 规则备注
}

// Name 返回数据源名称。
func (r *ReverseProxyScanner) Name() string { return "rproxy" }

// Enabled 目录未配置或全部候选不存在时返回 false（数据源计入 enabled 但标记降级）。
func (r *ReverseProxyScanner) Enabled() bool {
	for _, p := range r.Candidates {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// rpFile 是 ReverseProxy.json 的直接映射。
type rpFile struct {
	Rules   map[string]rpRule `json:"-"`
	Version int               `json:"-"`
}

type rpRule struct {
	Frontend    rpEnd  `json:"frontend"`
	Backend     rpEnd  `json:"backend"`
	Description string `json:"description"`
}

type rpEnd struct {
	FQDN     any `json:"fqdn"`
	Port     int `json:"port"`
	Protocol int `json:"protocol"` // 0=HTTP 1=HTTPS
	HTTPS    *struct {
		HSTS  bool `json:"hsts"`
		HTTP2 bool `json:"http2"`
	} `json:"https"`
}

// Scan 解析反代规则文件。
// 文件缺失/不可读是可降级情形：返回 Health 异常但不视为扫描失败（与 compose 探针一致）。
// DSM 未启用反代时文件内容为 {"version":2} 之类，正常返回 0 条规则。
func (r *ReverseProxyScanner) Scan() ([]ReverseProxyRule, []Health, error) {
	var data []byte
	var used string
	var lastErr string
	for _, p := range r.Candidates {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			lastErr = err.Error()
			continue
		}
		data, used = b, p
		break
	}
	h := Health{Name: r.Name(), Enabled: true, At: time.Now().UTC()}
	if data == nil {
		h.OK = false
		h.Error = fmt.Sprintf("反代规则文件不可读: %v（%s）", r.Candidates, lastErr)
		return nil, []Health{h}, nil
	}

	rules, err := parseReverseProxy(data)
	if err != nil {
		h.OK = false
		h.Error = fmt.Sprintf("反代规则解析失败 %s: %v", used, err)
		return nil, []Health{h}, nil
	}

	h.OK = true
	h.Count = len(rules)
	return rules, []Health{h}, nil
}

// parseReverseProxy 解析 ReverseProxy.json 内容。
// 顶层混有 UUID 键（规则）与 "version" 键（数字），用两次 Unmarshal 拆开。
func parseReverseProxy(data []byte) ([]ReverseProxyRule, error) {
	// 第一遍：宽松解析顶层，挑出对象值（规则）
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	var out []ReverseProxyRule
	for _, raw := range top {
		if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
			continue // version 之类的标量
		}
		var rule rpRule
		if err := json.Unmarshal(raw, &rule); err != nil {
			continue // 单条损坏不影响其余规则
		}
		if rule.Frontend.Port < 1 || rule.Frontend.Port > 65535 {
			continue
		}
		host := fqdnString(rule.Frontend.FQDN)
		r := ReverseProxyRule{
			Port:        rule.Frontend.Port,
			Protocol:    "tcp",
			HTTPS:       rule.Frontend.Protocol == 1,
			Host:        host,
			BackendPort: rule.Backend.Port,
			BackendHost: fqdnString(rule.Backend.FQDN),
			Description: strings.TrimSpace(rule.Description),
		}
		out = append(out, r)
	}
	return out, nil
}

// fqdnString 兼容 fqdn 为字符串或 null 的两种形态。
func fqdnString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}
