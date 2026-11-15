package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kyson-666/port-ledger/internal/reconcile"
	"github.com/kyson-666/port-ledger/internal/store"
)

// PortItem 是端口列表的一行（API 与前端的一等数据）。
type PortItem struct {
	Port       int    `json:"port"`
	Protocol   string `json:"protocol"`
	State      string `json:"state"`
	StateLabel string `json:"state_label"`
	Conflict   bool   `json:"conflict"`

	Name       string `json:"name"`
	NameSource string `json:"name_source"` // user / auto / ""
	Note       string `json:"note,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Project    string `json:"project,omitempty"`

	Source     string `json:"source"`
	SourceKind string `json:"source_kind"` // self/dict/docker/proc/compose/reserve/active

	ContainerName string `json:"container_name,omitempty"`
	Image         string `json:"image,omitempty"`
	ProcessName   string `json:"process_name,omitempty"`
	Cmdline       string `json:"cmdline,omitempty"`
	PID           int    `json:"pid,omitempty"`
	BindAddr      string `json:"bind_addr,omitempty"`
	ComposeFile   string `json:"compose_file,omitempty"`

	DictName string `json:"dict_name,omitempty"`
	DictRef  string `json:"dict_ref,omitempty"`
	DictCat  string `json:"dict_category,omitempty"`

	ReservedUntil string `json:"reserved_until,omitempty"`
	ObservedAt    string `json:"observed_at,omitempty"`

	// MetaOnly 表示该行仅来自用户台账（有命名但当前无任何观测），
	// 用于列表展示；不参与"空闲端口"的占用计算。
	MetaOnly bool `json:"-"`
}

// ListQuery 是端口列表查询参数。
type ListQuery struct {
	Q          string
	States     []string
	Protocol   string
	SourceKind string
	Project    string
	From, To   int
	Page       int
	PageSize   int
}

// ListResult 是端口列表响应。
type ListResult struct {
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []PortItem     `json:"items"`
	Counts   map[string]int `json:"counts"`
	LastScan *store.ScanRun `json:"last_scan"`
}

var stateLabels = map[string]string{
	reconcile.StateSelf:       "本工具",
	reconcile.StateSystem:     "系统",
	reconcile.StateRProxy:     "反代",
	reconcile.StateInUseDock:  "容器",
	reconcile.StateInUseHost:  "进程",
	reconcile.StateConfigured: "已声明",
	reconcile.StateReserved:   "已保留",
	reconcile.StateFree:       "空闲",
}

// StateLabel 返回状态的中文名。
func StateLabel(state string) string {
	if s, ok := stateLabels[state]; ok {
		return s
	}
	return state
}

func dictKey(port int, proto string) string { return fmt.Sprintf("%d/%s", port, proto) }

// parseKey 解析 "port/protocol" 形式的键。
func parseKey(k string) (int, string, bool) {
	i := strings.Index(k, "/")
	if i <= 0 {
		return 0, "", false
	}
	p, err := strconv.Atoi(k[:i])
	if err != nil || p < 1 || p > 65535 {
		return 0, "", false
	}
	proto := k[i+1:]
	if proto != "tcp" && proto != "udp" {
		return 0, "", false
	}
	return p, proto, true
}

// buildItems 依据一次扫描的观测构建全部端口条目（含可选的 free 枚举）。
func (a *App) buildItems(includeFree bool, from, to int) ([]PortItem, *store.ScanRun, map[string]int, error) {
	last, err := a.Store.LatestScan()
	if err != nil {
		return nil, nil, nil, err
	}
	counts := map[string]int{}
	if last == nil {
		return nil, nil, counts, nil
	}

	obs, err := a.Store.ObservationsByScan(last.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	metas, err := a.Store.AllPortMeta()
	if err != nil {
		return nil, nil, nil, err
	}
	metaByKey := map[string]store.PortMeta{}
	for _, m := range metas {
		metaByKey[dictKey(m.Port, m.Protocol)] = m
	}
	resList, err := a.Store.ListReservations()
	if err != nil {
		return nil, nil, nil, err
	}
	resByKey := map[string]store.Reservation{}
	for _, r := range resList {
		if !r.Expired {
			resByKey[dictKey(r.Port, r.Protocol)] = r
		}
	}

	d := a.Dict()
	items := make([]PortItem, 0, len(obs)+64)
	occupied := map[string]bool{}

	for _, o := range obs {
		k := dictKey(o.Port, o.Protocol)
		occupied[k] = true
		counts[o.State]++
		if o.Conflict {
			counts["conflict"]++
		}
		it := PortItem{
			Port:          o.Port,
			Protocol:      o.Protocol,
			State:         o.State,
			StateLabel:    StateLabel(o.State),
			Conflict:      o.Conflict,
			ContainerName: o.ContainerName,
			Image:         o.Image,
			ProcessName:   o.ProcessName,
			Cmdline:       o.Cmdline,
			PID:           o.PID,
			BindAddr:      o.BindAddr,
			ComposeFile:   o.ComposeFile,
			ObservedAt:    o.ObservedAt,
		}
		it.SourceKind = o.Source
		it.Source = describeSource(o)

		if m, ok := metaByKey[k]; ok {
			it.Note, it.Tags, it.Project = m.Note, m.Tags, m.Project
			if strings.TrimSpace(m.Name) != "" {
				it.Name = m.Name
				it.NameSource = "user"
			}
		}
		if e, ok := d.Lookup(o.Port, o.Protocol); ok {
			it.DictName, it.DictRef, it.DictCat = e.DisplayName, e.SourceRef, e.Category
			if it.Name == "" {
				it.Name = e.DisplayName
				it.NameSource = "auto"
			}
		}
		if r, ok := resByKey[k]; ok {
			it.ReservedUntil = r.ExpiresAt
		}
		items = append(items, it)
	}

	// 叠加：上次扫描之后新建的保留，保证列表即时反映保留结果（否则需等下一次扫描）
	for k, r := range resByKey {
		if occupied[k] {
			continue
		}
		port, proto, ok := parseKey(k)
		if !ok {
			continue
		}
		occupied[k] = true
		counts[reconcile.StateReserved]++
		it := PortItem{
			Port: port, Protocol: proto, State: reconcile.StateReserved,
			StateLabel: StateLabel(reconcile.StateReserved),
			Source:     "用户保留", SourceKind: reconcile.SrcReserve,
			ReservedUntil: r.ExpiresAt,
		}
		if m, ok := metaByKey[k]; ok {
			it.Note, it.Tags, it.Project = m.Note, m.Tags, m.Project
			if strings.TrimSpace(m.Name) != "" {
				it.Name, it.NameSource = m.Name, "user"
			}
		}
		if e, ok := d.Lookup(port, proto); ok {
			it.DictName, it.DictRef, it.DictCat = e.DisplayName, e.SourceRef, e.Category
			if it.Name == "" {
				it.Name, it.NameSource = e.DisplayName, "auto"
			}
		}
		items = append(items, it)
	}

	// 叠加：仅有用户台账（命名/备注/项目）但当前无任何观测的端口，按空闲行展示
	for k, m := range metaByKey {
		if occupied[k] {
			continue
		}
		port, proto, ok := parseKey(k)
		if !ok {
			continue
		}
		it := PortItem{
			Port: port, Protocol: proto, State: reconcile.StateFree,
			StateLabel: StateLabel(reconcile.StateFree),
			Source:     "—", SourceKind: "-",
			Note: m.Note, Tags: m.Tags, Project: m.Project,
			MetaOnly: true,
		}
		if strings.TrimSpace(m.Name) != "" {
			it.Name, it.NameSource = m.Name, "user"
		}
		items = append(items, it)
	}

	if includeFree {
		mk := func(lo, hi int) {
			if lo < 1 {
				lo = 1
			}
			if hi > 65535 {
				hi = 65535
			}
			for p := lo; p <= hi; p++ {
				for _, proto := range []string{"tcp", "udp"} {
					k := dictKey(p, proto)
					if occupied[k] {
						continue
					}
					if _, ok := d.Lookup(p, proto); ok {
						continue // 字典端口不属于空闲
					}
					if _, ok := resByKey[k]; ok {
						continue // 已保留不计入空闲
					}
					it := PortItem{Port: p, Protocol: proto, State: reconcile.StateFree, StateLabel: StateLabel(reconcile.StateFree), Source: "—", SourceKind: "-"}
					if m, ok := metaByKey[k]; ok {
						it.Note, it.Tags, it.Project = m.Note, m.Tags, m.Project
						if strings.TrimSpace(m.Name) != "" {
							it.Name, it.NameSource = m.Name, "user"
						}
					}
					items = append(items, it)
				}
			}
		}
		mk(from, to)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Port != items[j].Port {
			return items[i].Port < items[j].Port
		}
		return items[i].Protocol < items[j].Protocol
	})
	return items, last, counts, nil
}

// List 执行筛选、分页并返回结果。
func (a *App) List(q ListQuery) (*ListResult, error) {
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 100
	}
	if q.PageSize > 1000 {
		q.PageSize = 1000
	}
	wantFree := containsStr(q.States, reconcile.StateFree)
	from, to := q.From, q.To
	if wantFree {
		if from == 0 {
			from = 1024
		}
		if to == 0 {
			to = 65535
		}
	}

	items, last, counts, err := a.buildItems(wantFree, from, to)
	if err != nil {
		return nil, err
	}

	filtered := make([]PortItem, 0, len(items))
	for _, it := range items {
		if q.From > 0 && (it.Port < q.From || (q.To > 0 && it.Port > q.To)) {
			continue
		}
		if len(q.States) > 0 && !containsStr(q.States, it.State) {
			continue
		}
		if q.Protocol != "" && it.Protocol != q.Protocol {
			continue
		}
		if q.SourceKind != "" && it.SourceKind != q.SourceKind {
			continue
		}
		if q.Project != "" && it.Project != q.Project {
			continue
		}
		if q.Q != "" && !matchesQuery(it, q.Q) {
			continue
		}
		filtered = append(filtered, it)
	}

	total := len(filtered)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	pageItems := make([]PortItem, 0, end-start)
	pageItems = append(pageItems, filtered[start:end]...)
	return &ListResult{
		Total:    total,
		Page:     q.Page,
		PageSize: q.PageSize,
		Items:    pageItems,
		Counts:   counts,
		LastScan: last,
	}, nil
}

// GetPort 返回单个端口的条目视图。
func (a *App) GetPort(port int, proto string) (*PortItem, error) {
	res, err := a.List(ListQuery{From: port, To: port, PageSize: 10})
	if err != nil {
		return nil, err
	}
	for _, it := range res.Items {
		if it.Port == port && (proto == "" || it.Protocol == proto) {
			return &it, nil
		}
	}
	return nil, nil
}

func describeSource(o store.Observation) string {
	switch o.Source {
	case reconcile.SrcSelf:
		return "本工具"
	case reconcile.SrcDict:
		return "系统字典"
	case reconcile.SrcDocker:
		s := o.ContainerName
		if s == "" {
			s = "容器"
		}
		if o.Image != "" {
			s += " · " + o.Image
		}
		return s
	case reconcile.SrcProc:
		if o.ProcessName == "" {
			return "宿主进程"
		}
		if o.PID > 0 {
			return fmt.Sprintf("%s (PID %d)", o.ProcessName, o.PID)
		}
		return o.ProcessName
	case reconcile.SrcCompose:
		return "compose: " + filepath.Base(strings.SplitN(o.ComposeFile, "#", 2)[0])
	case reconcile.SrcReserve:
		return "用户保留"
	case reconcile.SrcActive:
		return "主动扫描"
	}
	return o.Source
}

func matchesQuery(it PortItem, q string) bool {
	q = strings.ToLower(q)
	hay := []string{
		strconv.Itoa(it.Port),
		strings.ToLower(it.Name),
		strings.ToLower(it.Note),
		strings.ToLower(it.Tags),
		strings.ToLower(it.Project),
		strings.ToLower(it.ContainerName),
		strings.ToLower(it.Image),
		strings.ToLower(it.ProcessName),
		strings.ToLower(it.DictName),
		strings.ToLower(it.ComposeFile),
	}
	for _, h := range hay {
		if h != "" && strings.Contains(h, q) {
			return true
		}
	}
	return false
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// FreeQuery 结果：区间折叠 + 端口列表。
type FreeQuery struct {
	From      int         `json:"from"`
	To        int         `json:"to"`
	Count     int         `json:"count"`
	Ports     []int       `json:"ports"`
	Ranges    []FreeRange `json:"ranges"`
	Protocol  string      `json:"protocol"`
	BasedOn   string      `json:"based_on"`
	Truncated bool        `json:"truncated"`
}

// FreeRange 是一段连续空闲区间。
type FreeRange struct {
	From  int `json:"from"`
	To    int `json:"to"`
	Count int `json:"count"`
}

const maxFreePorts = 5000

// Free 查询空闲端口（默认 TCP），返回端口列表与连续区间。
func (a *App) Free(from, to int, proto string, max int) (*FreeQuery, error) {
	if proto == "" {
		proto = "tcp"
	}
	if proto != "tcp" && proto != "udp" {
		return nil, fmt.Errorf("protocol 仅支持 tcp/udp")
	}
	if from <= 0 {
		from = 1024
	}
	if to <= 0 || to > 65535 {
		to = 65535
	}
	if from > to {
		from, to = to, from
	}
	if max <= 0 || max > maxFreePorts {
		max = maxFreePorts
	}

	items, last, _, err := a.buildItems(false, 0, 0)
	if err != nil {
		return nil, err
	}
	occupied := map[int]bool{}
	for _, it := range items {
		if it.MetaOnly {
			continue // 仅有台账、无观测的端口仍视为可分配
		}
		if it.Protocol == proto {
			occupied[it.Port] = true
		}
	}
	// 字典与保留已在 buildItems 中体现为观测；此处仅需排除它们。

	res := &FreeQuery{From: from, To: to, Protocol: proto}
	if last != nil {
		res.BasedOn = last.FinishedAt
	}

	var ports []int
	var ranges []FreeRange
	var cur *FreeRange
	for p := from; p <= to; p++ {
		if occupied[p] {
			if cur != nil {
				ranges = append(ranges, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			cur = &FreeRange{From: p, To: p, Count: 1}
		} else {
			cur.To = p
			cur.Count++
		}
		if len(ports) < max {
			ports = append(ports, p)
		} else {
			res.Truncated = true
		}
	}
	if cur != nil {
		ranges = append(ranges, *cur)
	}
	res.Ports = ports
	res.Count = len(ports)
	res.Ranges = ranges
	return res, nil
}

// Summary 是总览页所需的聚合数据。
type Summary struct {
	Version     string         `json:"version"`
	Uptime      int64          `json:"uptime_seconds"`
	LastScan    *store.ScanRun `json:"last_scan"`
	Counts      map[string]int `json:"counts"`
	FreeCount   int            `json:"free_count"`
	FreeFrom    int            `json:"free_from"`
	FreeTo      int            `json:"free_to"`
	DictSize    int            `json:"dict_size"`
	DictBuiltin int            `json:"dict_builtin"`
	DictCustom  int            `json:"dict_custom"`
	Health      any            `json:"source_health"`
	Scanning    bool           `json:"scanning"`
}

// Summary 构建总览聚合。
func (a *App) Summary() (*Summary, error) {
	items, last, counts, err := a.buildItems(false, 0, 0)
	if err != nil {
		return nil, err
	}
	from, to := 1024, 65535
	occupied := map[int]bool{}
	for _, it := range items {
		if it.MetaOnly {
			continue
		}
		if it.Protocol == "tcp" {
			occupied[it.Port] = true
		}
	}
	free := 0
	for p := from; p <= to; p++ {
		if !occupied[p] {
			free++
		}
	}
	d := a.Dict()
	builtin, _ := a.Store.BuiltinDict()
	custom, _ := a.Store.ListDictUser()

	s := &Summary{
		Version:     a.Cfg.Version,
		Uptime:      int64(time.Since(a.startedAt).Seconds()),
		LastScan:    last,
		Counts:      counts,
		FreeCount:   free,
		FreeFrom:    from,
		FreeTo:      to,
		DictSize:    d.Size(),
		DictBuiltin: len(builtin),
		DictCustom:  len(custom),
		Health:      a.SourceHealth(),
		Scanning:    a.Scanning(),
	}
	return s, nil
}
