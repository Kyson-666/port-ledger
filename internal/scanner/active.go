package scanner

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// ActiveScanner 对指定目标发起 TCP connect 探测。
//
// 重要限制：本工具以 bridge 网络运行，容器内的 127.0.0.1 指向容器自身而非宿主，
// 因此主动扫描无法发现宿主端口，仅作为"容器可达范围"的补充手段。
// 默认关闭（SCAN_TARGET_HOST 为空时扫描器整体禁用）。
type ActiveScanner struct {
	Target    string // 目标主机；为空则禁用
	TimeoutMS int
	MaxPorts  int // 单次扫描端口上限，防止误用
}

// Name 返回数据源名称。
func (a *ActiveScanner) Name() string { return "active" }

// Enabled 返回是否启用。
func (a *ActiveScanner) Enabled() bool { return a.Target != "" }

// Scan 并发探测 [lo,hi] 区间，返回处于监听状态的端口。
func (a *ActiveScanner) Scan(lo, hi int) ([]ActiveResult, []Health, error) {
	if !a.Enabled() {
		return nil, []Health{{Name: "active", Enabled: false, OK: true, At: time.Now().UTC()}}, nil
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	if a.MaxPorts > 0 && hi-lo+1 > a.MaxPorts {
		err := fmt.Errorf("主动扫描范围过大（%d 个端口，上限 %d）", hi-lo+1, a.MaxPorts)
		return nil, []Health{{Name: "active", Enabled: true, OK: false, Error: err.Error(), At: time.Now().UTC()}}, err
	}
	timeout := time.Duration(a.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}

	sem := make(chan struct{}, 128)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var out []ActiveResult

	for p := lo; p <= hi; p++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(port int) {
			defer wg.Done()
			defer func() { <-sem }()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(a.Target, strconv.Itoa(port)), timeout)
			open := err == nil
			if conn != nil {
				conn.Close()
			}
			if open {
				mu.Lock()
				out = append(out, ActiveResult{Port: port, Proto: "tcp", Open: true})
				mu.Unlock()
			}
		}(p)
	}
	wg.Wait()

	return out, []Health{{Name: "active", Enabled: true, OK: true, Count: len(out), At: time.Now().UTC()}}, nil
}
