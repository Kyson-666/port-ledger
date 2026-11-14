package scanner

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ProcScanner 解析宿主 /proc/net/{tcp,tcp6,udp,udp6}。
// 容器内需只读挂载宿主 /proc 到 ProcRoot（默认 /host/proc）。
type ProcScanner struct {
	ProcRoot string
}

// Name 返回数据源名称。
func (p *ProcScanner) Name() string { return "proc" }

// Scan 返回宿主全部监听端口。单个文件缺失不视为失败（例如内核未启用 IPv6）。
func (p *ProcScanner) Scan() ([]Listener, []Health, error) {
	files := []struct {
		file string
		proto string
	}{
		{"net/tcp", "tcp"},
		{"net/tcp6", "tcp"},
		{"net/udp", "udp"},
		{"net/udp6", "udp"},
	}

	inodes := p.resolveInodes()

	var out []Listener
	seen := map[string]bool{}
	var lastErr error
	okCount := 0

	for _, f := range files {
		path := filepath.Join(p.ProcRoot, filepath.FromSlash(f.file))
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = fmt.Errorf("读取 %s 失败: %w", path, err)
			continue
		}
		okCount++
		for _, line := range strings.Split(string(data), "\n") {
			l, ok := parseProcLine(line, f.proto)
			if !ok {
				continue
			}
			key := fmt.Sprintf("%d/%s", l.Port, l.Protocol)
			if seen[key] {
				// 合并绑定地址（同一端口可能同时 bind 在 v4/v6）
				continue
			}
			seen[key] = true
			if in, ok := inodes[l.Inode]; ok {
				l.PID = in.pid
				l.ProcessName = in.name
				l.Cmdline = in.cmdline
			}
			out = append(out, l)
		}
	}

	health := []Health{{
		Name: "proc", Enabled: true, OK: okCount > 0, Count: len(out),
		At: time.Now().UTC(),
	}}
	if okCount == 0 && lastErr != nil {
		health[0].Error = lastErr.Error()
	}
	return out, health, nil
}

// parseProcLine 解析 /proc/net/tcp* 的一行；非监听行返回 false。
func parseProcLine(line, proto string) (Listener, bool) {
	fields := strings.Fields(line)
	if len(fields) < 10 {
		return Listener{}, false
	}
	if fields[0] == "sl" || strings.HasPrefix(fields[0], "sl") {
		return Listener{}, false
	}
	local := fields[1]
	remote := fields[2]
	state := fields[3]

	switch proto {
	case "tcp":
		if state != "0A" { // TCP_LISTEN
			return Listener{}, false
		}
	case "udp":
		// UDP 只保留未连接（服务端绑定）的套接字
		if _, rp, ok := splitProcAddr(remote); ok && rp != 0 {
			return Listener{}, false
		}
	}

	addr, port, ok := splitProcAddr(local)
	if !ok || port == 0 {
		return Listener{}, false
	}
	// inode 位于第 10 个字段（index 9）
	return Listener{
		Port:     port,
		Protocol: proto,
		BindAddr: addr,
		Inode:    fields[9],
	}, true
}

// splitProcAddr 解析 "HEXADDR:HEXPORT"，返回可读地址与端口号。
func splitProcAddr(s string) (string, int, bool) {
	i := strings.LastIndex(s, ":")
	if i <= 0 {
		return "", 0, false
	}
	hexAddr := s[:i]
	hexPort := s[i+1:]
	p, err := strconv.ParseInt(hexPort, 16, 32)
	if err != nil {
		return "", 0, false
	}
	var addr string
	switch len(hexAddr) {
	case 8:
		addr = decodeIPv4(hexAddr)
	case 32:
		addr = decodeIPv6(hexAddr)
	default:
		addr = hexAddr
	}
	return addr, int(p), true
}

// decodeIPv4 把 /proc 中的小端 32 位十六进制转为点分十进制。
func decodeIPv4(h string) string {
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return h
	}
	return net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
}

// decodeIPv6 把 /proc 中的 4 个小端 32 位字转为标准 IPv6 文本。
func decodeIPv6(h string) string {
	if len(h) != 32 {
		return h
	}
	ip := make(net.IP, 16)
	for w := 0; w < 4; w++ {
		v, err := strconv.ParseUint(h[w*8:(w+1)*8], 16, 32)
		if err != nil {
			return h
		}
		ip[w*4+0] = byte(v)
		ip[w*4+1] = byte(v >> 8)
		ip[w*4+2] = byte(v >> 16)
		ip[w*4+3] = byte(v >> 24)
	}
	return ip.String()
}

type procInfo struct {
	pid     int
	name    string
	cmdline string
}

// resolveInodes 遍历 ProcRoot 下各进程的 fd，建立 inode → 进程信息 映射。
// 只读，且失败静默降级（进程名称为可选增强信息）。
func (p *ProcScanner) resolveInodes() map[string]procInfo {
	result := map[string]procInfo{}
	ents, err := os.ReadDir(p.ProcRoot)
	if err != nil {
		return result
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		base := filepath.Join(p.ProcRoot, e.Name())
		info := procInfo{pid: pid, name: readTrimmed(filepath.Join(base, "comm"))}
		if info.name == "" {
			continue
		}
		info.cmdline = readCmdline(filepath.Join(base, "cmdline"))
		fdDir := filepath.Join(base, "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "socket:[") {
				inode := strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]")
				if _, exists := result[inode]; !exists {
					result[inode] = info
				}
			}
		}
	}
	return result
}

func readTrimmed(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readCmdline(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(string(b), "\x00", " ")
	return strings.TrimSpace(s)
}
