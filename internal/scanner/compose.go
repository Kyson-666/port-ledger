package scanner

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ComposeScanner 静态解析 compose 文件中的 ports 声明（覆盖"已声明未启动"的端口）。
type ComposeScanner struct {
	Dir      string // 只读挂载的 compose 目录
	MaxFiles int
}

// Name 返回数据源名称。
func (c *ComposeScanner) Name() string { return "compose" }

// Scan 遍历目录下的 *.yml/*.yaml 并抽取宿主端口。
func (c *ComposeScanner) Scan() ([]ComposePort, []Health, error) {
	info, err := os.Stat(c.Dir)
	if err != nil || !info.IsDir() {
		// 目录缺失属可降级情形：不返回错误，仅标记数据源异常（与 proc 扫描器一致）
		msg := fmt.Sprintf("compose 目录不可用: %s", c.Dir)
		return nil, []Health{{Name: "compose", Enabled: true, OK: false, Error: msg, At: time.Now().UTC()}}, nil
	}

	maxFiles := c.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 500
	}

	seen := map[string]bool{}
	var out []ComposePort
	files := 0

	walkErr := filepath.WalkDir(c.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略无权限的子目录
		}
		if d.IsDir() {
			base := d.Name()
			if base == "node_modules" || base == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			return nil
		}
		if files >= maxFiles {
			return fs.SkipAll
		}
		files++

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, cp := range parseComposeDoc(data, path) {
			key := fmt.Sprintf("%d/%s", cp.Port, cp.Protocol)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, cp)
		}
		return nil
	})
	_ = walkErr

	h := Health{Name: "compose", Enabled: true, OK: true, Count: len(out), At: time.Now().UTC()}
	return out, []Health{h}, nil
}

// parseComposeDoc 解析单个 compose 文档的 services.*.ports。
func parseComposeDoc(data []byte, path string) []ComposePort {
	var doc struct {
		Services map[string]struct {
			Ports []any `yaml:"ports"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []ComposePort
	for svc, s := range doc.Services {
		for _, p := range s.Ports {
			out = append(out, parsePortEntry(p, path, svc)...)
		}
	}
	return out
}

const maxRangeExpand = 200

// parsePortEntry 解析 compose 端口条目的三种写法。
func parsePortEntry(v any, file, service string) []ComposePort {
	mk := func(lo, hi int, proto string) []ComposePort {
		if hi < lo {
			lo, hi = hi, lo
		}
		if hi-lo+1 > maxRangeExpand {
			hi = lo
		}
		var res []ComposePort
		for p := lo; p <= hi; p++ {
			if p < 1 || p > 65535 {
				continue
			}
			res = append(res, ComposePort{Port: p, Protocol: proto, File: file, Service: service})
		}
		return res
	}

	switch t := v.(type) {
	case int:
		// 仅容器端口，宿主端口由 Docker 动态分配：不占用固定宿主端口
		return nil
	case string:
		return parsePortString(t, file, service, mk)
	case map[string]any:
		return parsePortMap(t, file, service, mk)
	case map[any]any:
		m := map[string]any{}
		for k, val := range t {
			m[fmt.Sprint(k)] = val
		}
		return parsePortMap(m, file, service, mk)
	}
	return nil
}

func parsePortString(s, file, service string, mk func(int, int, string) []ComposePort) []ComposePort {
	raw := strings.TrimSpace(s)
	if raw == "" || strings.Contains(raw, "${") {
		return nil // 变量插值无法静态求值
	}
	proto := "tcp"
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		proto = strings.ToLower(strings.TrimSpace(raw[i+1:]))
		raw = raw[:i]
	}
	parts := strings.Split(raw, ":")
	var hostPart string
	switch len(parts) {
	case 1:
		return nil // 只有容器端口
	case 2:
		hostPart = parts[0]
	default:
		hostPart = parts[len(parts)-2] // ip:host:container
	}
	lo, hi, ok := parsePortRange(hostPart)
	if !ok {
		return nil
	}
	return mk(lo, hi, proto)
}

func parsePortMap(m map[string]any, file, service string, mk func(int, int, string) []ComposePort) []ComposePort {
	proto := "tcp"
	if p, ok := m["protocol"].(string); ok && p != "" {
		proto = strings.ToLower(p)
	}
	mode, _ := m["mode"].(string)
	published := toInt(m["published"])
	target := toInt(m["target"])
	if published == 0 {
		if mode == "host" && target > 0 {
			return mk(target, target, proto) // host 模式直接占用容器端口
		}
		return nil
	}
	return mk(published, published, proto)
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func parsePortRange(s string) (int, int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	if i := strings.Index(s, "-"); i > 0 {
		lo, err1 := strconv.Atoi(strings.TrimSpace(s[:i]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(s[i+1:]))
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return lo, hi, true
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, 0, false
	}
	return n, n, true
}
