// Package dict 内置群晖系统端口字典（官方 KB 全量），并提供端口匹配。
package dict

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed system_ports.json
var builtinJSON []byte

// Entry 是字典中的一条端口条目；PortEnd 为 0 表示单端口。
type Entry struct {
	Port        int    `json:"port"`
	PortEnd     int    `json:"port_end"`
	Protocol    string `json:"protocol"` // tcp / udp / both
	Category    string `json:"category"`
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	SourceRef   string `json:"source_ref,omitempty"`

	// 运行期附加字段（不参与内置 JSON 解析）
	Type string `json:"type,omitempty"` // builtin / overridden / custom
}

// End 返回条目覆盖的末端口（单端口时等于 Port）。
func (e Entry) End() int {
	if e.PortEnd > e.Port {
		return e.PortEnd
	}
	return e.Port
}

// MatchesProtocol 判断条目是否覆盖给定协议。
func (e Entry) MatchesProtocol(protocol string) bool {
	return e.Protocol == "both" || e.Protocol == protocol
}

// Builtin 返回内置字典条目（已排序）。
func Builtin() ([]Entry, error) {
	var es []Entry
	if err := json.Unmarshal(builtinJSON, &es); err != nil {
		return nil, fmt.Errorf("内置系统端口字典解析失败: %w", err)
	}
	for i := range es {
		es[i].Type = "builtin"
	}
	Sort(es)
	return es, nil
}

// Sort 使单端口条目先于区间条目，保证首匹配语义稳定。
func Sort(es []Entry) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Port != es[j].Port {
			return es[i].Port < es[j].Port
		}
		si, sj := es[i].PortEnd == 0, es[j].PortEnd == 0
		if si != sj {
			return si // 单端口优先
		}
		return es[i].PortEnd < es[j].PortEnd
	})
}

// Dict 是可查询的字典。
type Dict struct {
	entries []Entry
}

// New 用给定条目构造字典（会自动排序）。
func New(es []Entry) *Dict {
	cp := make([]Entry, len(es))
	copy(cp, es)
	Sort(cp)
	return &Dict{entries: cp}
}

// Entries 返回全部条目副本。
func (d *Dict) Entries() []Entry {
	out := make([]Entry, len(d.entries))
	copy(out, d.entries)
	return out
}

// Lookup 返回首个匹配 (port, protocol) 的条目。单端口条目优先于区间条目。
func (d *Dict) Lookup(port int, protocol string) (Entry, bool) {
	for _, e := range d.entries {
		if port < e.Port || port > e.End() {
			continue
		}
		if !e.MatchesProtocol(protocol) {
			continue
		}
		return e, true
	}
	return Entry{}, false
}

// Categories 返回按首次出现顺序排列的类别列表。
func (d *Dict) Categories() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range d.entries {
		if !seen[e.Category] {
			seen[e.Category] = true
			out = append(out, e.Category)
		}
	}
	return out
}

// Size 返回条目数。
func (d *Dict) Size() int { return len(d.entries) }
