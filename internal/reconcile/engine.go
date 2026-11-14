// Package reconcile 实现设计稿 §3.3 的端口状态归并。
//
// 优先级（高 → 低）：self > system > in-use(docker) > in-use(host) > configured > reserved；其余为 free。
// 额外叠加态：冲突（conflict）——命中共晖系统字典，同时又被容器/进程/compose 实际占用。
package reconcile

import (
	"fmt"
	"sort"

	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/scanner"
	"github.com/kyson-666/port-ledger/internal/store"
)

// 端口状态常量。
const (
	StateSelf       = "self"
	StateSystem     = "system"
	StateInUseDock  = "in-use(docker)"
	StateInUseHost  = "in-use(host)"
	StateConfigured = "configured"
	StateReserved   = "reserved"
	StateFree       = "free"
)

// 数据来源常量。
const (
	SrcSelf    = "self"
	SrcDict    = "dict"
	SrcDocker  = "docker"
	SrcProc    = "proc"
	SrcCompose = "compose"
	SrcReserve = "reserve"
	SrcActive  = "active"
)

// Priority 返回状态的归并优先级，数字越小优先级越高。
func Priority(state string) int {
	switch state {
	case StateSelf:
		return 1
	case StateSystem:
		return 2
	case StateInUseDock:
		return 3
	case StateInUseHost:
		return 4
	case StateConfigured:
		return 5
	case StateReserved:
		return 6
	default:
		return 9 // free
	}
}

// Input 是一次归并的全部输入。
type Input struct {
	Listeners    []scanner.Listener
	Containers   []scanner.ContainerPort
	Compose      []scanner.ComposePort
	Active       map[string]bool
	Dict         *dict.Dict
	Reservations map[string]store.Reservation
	Ranges       []config.Range
	SelfKeys     map[string]bool
}

func key(port int, proto string) string { return fmt.Sprintf("%d/%s", port, proto) }

func inRanges(ranges []config.Range, port int) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, r := range ranges {
		if port >= r.Lo && port <= r.Hi {
			return true
		}
	}
	return false
}

func protosOf(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}

// Run 执行归并，返回有序的观测列表。
func Run(in Input) []store.Observation {
	idx := map[string]*store.Observation{}
	get := func(port int, proto string) *store.Observation {
		k := key(port, proto)
		if o, ok := idx[k]; ok {
			return o
		}
		o := &store.Observation{Port: port, Protocol: proto}
		idx[k] = o
		return o
	}
	// put 仅在该键尚无更高优先级状态时写入。
	put := func(port int, proto, state, source string) *store.Observation {
		o := get(port, proto)
		if o.State == "" || Priority(state) < Priority(o.State) {
			o.State = state
			o.Source = source
		}
		return o
	}

	// 1) self：本工具自身占用，最高优先级
	for k := range in.SelfKeys {
		var port int
		var proto string
		fmt.Sscanf(k, "%d/%s", &port, &proto)
		if port == 0 || proto == "" {
			continue
		}
		o := get(port, proto)
		o.State = StateSelf
		o.Source = SrcSelf
		o.ProcessName = "port-ledger"
	}

	// 2) system：命中群晖系统端口字典
	if in.Dict != nil {
		for _, e := range in.Dict.Entries() {
			for p := e.Port; p <= e.End(); p++ {
				if !inRanges(in.Ranges, p) {
					continue
				}
				for _, proto := range protosOf(e.Protocol) {
					put(p, proto, StateSystem, SrcDict)
				}
			}
		}
	}

	// 3) in-use(docker)：容器发布映射
	for _, c := range in.Containers {
		o := get(c.Port, c.Protocol)
		if o.State == "" {
			o.State = StateInUseDock
			o.Source = SrcDocker
		} else if o.State == StateSystem {
			// 系统端口被容器占用 → 冲突叠加
			o.Conflict = true
		}
		o.ContainerID = c.ContainerID
		o.ContainerName = c.ContainerName
		o.Image = c.Image
		if o.BindAddr == "" {
			o.BindAddr = c.BindAddr
		}
	}

	// 4) in-use(host)：宿主进程监听
	for _, l := range in.Listeners {
		o := get(l.Port, l.Protocol)
		if o.State == "" {
			o.State = StateInUseHost
			o.Source = SrcProc
		} else if o.State == StateSystem {
			o.Conflict = true
		}
		if o.PID == 0 {
			o.PID = l.PID
			o.ProcessName = l.ProcessName
			o.Cmdline = l.Cmdline
		}
		if o.BindAddr == "" {
			o.BindAddr = l.BindAddr
		}
	}

	// 5) active：容器可达范围内的 TCP 探测结论（补充证据）
	for k := range in.Active {
		var port int
		var proto string
		fmt.Sscanf(k, "%d/%s", &port, &proto)
		if port == 0 || proto == "" {
			continue
		}
		o := get(port, proto)
		if o.State == "" {
			o.State = StateInUseHost
			o.Source = SrcActive
		} else if o.State == StateSystem {
			o.Conflict = true
		}
	}

	// 6) configured：compose 已声明但未监听
	for _, c := range in.Compose {
		o := get(c.Port, c.Protocol)
		if o.State == "" {
			o.State = StateConfigured
			o.Source = SrcCompose
		} else if o.State == StateSystem {
			o.Conflict = true
		}
		if o.ComposeFile == "" {
			o.ComposeFile = c.File
			if c.Service != "" {
				o.ComposeFile = fmt.Sprintf("%s#%s", c.File, c.Service)
			}
		}
	}

	// 7) reserved：用户保留（不覆盖已占用状态）
	for k, r := range in.Reservations {
		var port int
		var proto string
		fmt.Sscanf(k, "%d/%s", &port, &proto)
		if port == 0 || proto == "" {
			continue
		}
		o := get(port, proto)
		if o.State == "" {
			o.State = StateReserved
			o.Source = SrcReserve
		}
		if o.Source == SrcReserve {
			o.ProcessName = r.Owner
		}
	}

	out := make([]store.Observation, 0, len(idx))
	for _, o := range idx {
		if o.State == "" {
			continue
		}
		out = append(out, *o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		return out[i].Protocol < out[j].Protocol
	})
	return out
}

// Counts 统计各状态数量，供 scan_runs.counts 与总览页使用。
func Counts(obs []store.Observation) map[string]int {
	m := map[string]int{}
	for _, o := range obs {
		m[o.State]++
		if o.Conflict {
			m["conflict"]++
		}
	}
	return m
}
