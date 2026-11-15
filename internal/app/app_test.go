package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/store"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		AdminPort:        443,
		DataDir:          dir,
		LogDir:           filepath.Join(dir, "logs"),
		BackupDir:        filepath.Join(dir, "backups"),
		DBPath:           filepath.Join(dir, "port-ledger.db"),
		LogLevel:         "info",
		ScanHostProc:     false,
		ScanDockerSocket: false,
		ScanComposeDir:   "",
		ScanRanges:       "1-65535",
		ScanOnStart:      false,
		ScanInterval:     0,
		ProcRoot:         filepath.Join(dir, "proc"),
		DockerHost:       "unix:///var/run/docker.sock",
		SelfContainer:    "port-ledger",
		Version:          "test",
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	es, err := dict.Builtin()
	if err != nil {
		t.Fatalf("dict.Builtin: %v", err)
	}
	if err := st.SeedDict(es); err != nil {
		t.Fatalf("SeedDict: %v", err)
	}
	lg, err := logx.New(cfg.LogDir, logx.Info, false)
	if err != nil {
		t.Fatalf("logx.New: %v", err)
	}
	t.Cleanup(func() { lg.Close() })
	a, err := New(cfg, st, lg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	return a
}

// seedScan 直接写入一次扫描结果，用于视图层测试。
func seedScan(t *testing.T, a *App, obs []store.Observation, status string) {
	t.Helper()
	id, err := a.Store.BeginScan("test")
	if err != nil {
		t.Fatalf("BeginScan: %v", err)
	}
	if err := a.Store.ReplaceObservations(id, obs); err != nil {
		t.Fatalf("ReplaceObservations: %v", err)
	}
	if err := a.Store.FinishScan(id, status, "{}", "[]"); err != nil {
		t.Fatalf("FinishScan: %v", err)
	}
}

func TestScanWithAllSourcesDisabled(t *testing.T) {
	a := newTestApp(t)
	res, err := a.Scan(ScanOptions{})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("无启用数据源时状态应为 ok，实际 %s", res.Status)
	}
	if res.Counts["system"] == 0 {
		t.Fatalf("内置字典应产生 system 观测: %+v", res.Counts)
	}
	// 观测应已落库
	obs, err := a.Store.ObservationsByScan(res.ScanID)
	if err != nil || len(obs) != res.Counts["system"] {
		t.Fatalf("落库观测数 %d，期望 %d（%v）", len(obs), res.Counts["system"], err)
	}
	for _, o := range obs {
		if o.State != "system" || o.Source != "dict" {
			t.Fatalf("未启用其他数据源时应全部为 system/dict: %+v", o)
		}
	}
	// 再次扫描应产生新的 scan_id
	res2, err := a.Scan(ScanOptions{})
	if err != nil {
		t.Fatalf("Scan 二次: %v", err)
	}
	if res2.ScanID <= res.ScanID {
		t.Errorf("scan_id 应递增: %d -> %d", res.ScanID, res2.ScanID)
	}
	if n, _ := a.Store.CountScans(); n != 2 {
		t.Errorf("CountScans = %d，期望 2", n)
	}
}

func TestListFiltersAndPaging(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
		{Port: 9000, Protocol: "tcp", State: "in-use(docker)", Source: "docker", ContainerName: "portainer", Image: "portainer/portainer-ce"},
		{Port: 8081, Protocol: "tcp", State: "in-use(host)", Source: "proc", PID: 12, ProcessName: "nginx"},
		{Port: 6690, Protocol: "tcp", State: "system", Source: "dict", Conflict: true, ContainerName: "drive"},
		{Port: 7000, Protocol: "tcp", State: "configured", Source: "compose", ComposeFile: "/compose/a.yml#web"},
		{Port: 21000, Protocol: "tcp", State: "reserved", Source: "reserve"},
		{Port: 123, Protocol: "udp", State: "system", Source: "dict"},
	}, "ok")

	// 默认：不含 free
	res, err := a.List(ListQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if res.Total != 7 {
		t.Fatalf("默认列表应返回 7 条（不含空闲），实际 %d", res.Total)
	}
	for _, it := range res.Items {
		if it.State == "free" {
			t.Fatalf("默认不应包含空闲端口: %+v", it)
		}
	}

	// 状态筛选
	res, _ = a.List(ListQuery{States: []string{"in-use(docker)"}})
	if res.Total != 1 || res.Items[0].Port != 9000 {
		t.Fatalf("状态筛选失败: %+v", res.Items)
	}
	// 协议筛选
	res, _ = a.List(ListQuery{Protocol: "udp"})
	if res.Total != 1 || res.Items[0].Port != 123 {
		t.Fatalf("协议筛选失败: %+v", res.Items)
	}
	// 来源筛选
	res, _ = a.List(ListQuery{SourceKind: "proc"})
	if res.Total != 1 || res.Items[0].ProcessName != "nginx" {
		t.Fatalf("来源筛选失败: %+v", res.Items)
	}
	// 关键字：容器名
	res, _ = a.List(ListQuery{Q: "portainer"})
	if res.Total != 1 || res.Items[0].Port != 9000 {
		t.Fatalf("关键字筛选失败: %+v", res.Items)
	}
	// 关键字：字典名（自动命名）
	res, _ = a.List(ListQuery{Q: "DSM"})
	if res.Total != 1 || res.Items[0].Port != 5000 {
		t.Fatalf("按系统字典名搜索失败: %+v", res.Items)
	}
	// 范围筛选
	res, _ = a.List(ListQuery{From: 8000, To: 9000})
	if res.Total != 2 {
		t.Fatalf("范围筛选应为 2 条，实际 %d", res.Total)
	}
	// 分页
	res, _ = a.List(ListQuery{Page: 1, PageSize: 3})
	if len(res.Items) != 3 || res.Total != 7 {
		t.Fatalf("分页失败: len=%d total=%d", len(res.Items), res.Total)
	}
	res, _ = a.List(ListQuery{Page: 3, PageSize: 3})
	if len(res.Items) != 1 {
		t.Fatalf("末页应为 1 条，实际 %d", len(res.Items))
	}
	// 计数与冲突
	res, _ = a.List(ListQuery{})
	if res.Counts["conflict"] != 1 || res.Counts["system"] != 3 {
		t.Fatalf("计数不符: %+v", res.Counts)
	}
	if res.LastScan == nil || res.LastScan.Status != "ok" {
		t.Fatalf("last_scan 不符: %+v", res.LastScan)
	}
}

func TestListFreeEnumeration(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
		{Port: 20000, Protocol: "tcp", State: "in-use(host)", Source: "proc"},
	}, "ok")

	res, err := a.List(ListQuery{States: []string{"free"}, From: 19998, To: 20002})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// 19998,19999,20001,20002 空闲（tcp+udp 各一条）；20000 仅 tcp 被占用
	if res.Total != 9 {
		t.Fatalf("空闲端口应为 9 条（tcp+udp），实际 %d（%+v）", res.Total, res.Items)
	}
	for _, it := range res.Items {
		if it.State != "free" {
			t.Fatalf("状态应为 free: %+v", it)
		}
		if it.Port == 20000 && it.Protocol == "tcp" {
			t.Fatal("20000/tcp 已被占用，不应列为空闲")
		}
	}
	// 20000/udp 未被占用，应仍为空闲
	res, _ = a.List(ListQuery{States: []string{"free"}, Protocol: "udp", From: 19998, To: 20002})
	if res.Total != 5 {
		t.Fatalf("udp 空闲应为 5 个，实际 %d", res.Total)
	}
	var saw20000UDP bool
	for _, it := range res.Items {
		if it.Port == 20000 && it.Protocol == "udp" {
			saw20000UDP = true
		}
	}
	if !saw20000UDP {
		t.Fatal("20000/udp 应列为空闲（占用仅发生在 tcp）")
	}
}

func TestFreeQueryAndRanges(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 20000, Protocol: "tcp", State: "in-use(host)", Source: "proc"},
		{Port: 20001, Protocol: "tcp", State: "in-use(host)", Source: "proc"},
		{Port: 20005, Protocol: "tcp", State: "in-use(host)", Source: "proc"},
	}, "ok")

	fq, err := a.Free(20000, 20006, "tcp", 100)
	if err != nil {
		t.Fatalf("Free: %v", err)
	}
	if fq.Count != 4 {
		t.Fatalf("空闲数应为 4，实际 %d（%v）", fq.Count, fq.Ports)
	}
	// 连续区间应折叠为 [20002-20004] 与 [20006-20006]
	if len(fq.Ranges) != 2 {
		t.Fatalf("区间数应为 2，实际 %d（%+v）", len(fq.Ranges), fq.Ranges)
	}
	if fq.Ranges[0].From != 20002 || fq.Ranges[0].To != 20004 || fq.Ranges[0].Count != 3 {
		t.Fatalf("首段区间不符: %+v", fq.Ranges[0])
	}
	if fq.BasedOn == "" {
		t.Error("应带上次扫描时间")
	}

	// 数量上限
	fq, _ = a.Free(20000, 60000, "tcp", 3)
	if fq.Count != 3 || !fq.Truncated {
		t.Fatalf("数量上限未生效: count=%d truncated=%v", fq.Count, fq.Truncated)
	}
}

func TestSummary(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
		{Port: 9000, Protocol: "tcp", State: "in-use(docker)", Source: "docker"},
		{Port: 6690, Protocol: "tcp", State: "system", Source: "dict", Conflict: true},
	}, "ok")

	s, err := a.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if s.Version != "test" {
		t.Errorf("Version = %s", s.Version)
	}
	if s.Counts["system"] != 2 || s.Counts["in-use(docker)"] != 1 {
		t.Errorf("计数不符: %+v", s.Counts)
	}
	if s.Counts["conflict"] != 1 {
		t.Errorf("冲突计数不符: %+v", s.Counts)
	}
	if s.FreeCount <= 0 {
		t.Errorf("空闲计数应大于 0，实际 %d", s.FreeCount)
	}
	if s.FreeFrom != 1024 || s.FreeTo != 65535 {
		t.Errorf("空闲范围不符: %d-%d", s.FreeFrom, s.FreeTo)
	}
	if s.DictSize == 0 || s.DictBuiltin == 0 {
		t.Errorf("字典统计不符: %+v", s)
	}
	if s.Uptime < 0 {
		t.Errorf("运行时长异常: %d", s.Uptime)
	}
}

func TestGetPortAndLabels(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 9000, Protocol: "tcp", State: "in-use(docker)", Source: "docker", ContainerName: "portainer", Image: "portainer/portainer-ce"},
	}, "ok")
	_ = a.Store.PutPortMeta(store.PortMeta{Port: 9000, Protocol: "tcp", Name: "Portainer 面板", Project: "portainer"})

	it, err := a.GetPort(9000, "tcp")
	if err != nil || it == nil {
		t.Fatalf("GetPort: %+v %v", it, err)
	}
	if it.Name != "Portainer 面板" || it.NameSource != "user" {
		t.Errorf("用户命名应优先生效: %+v", it)
	}
	if it.StateLabel != "容器" {
		t.Errorf("状态中文名不符: %s", it.StateLabel)
	}
	if it.Source != "portainer · portainer/portainer-ce" {
		t.Errorf("来源描述不符: %s", it.Source)
	}
	if it.Project != "portainer" {
		t.Errorf("归属项目不符: %s", it.Project)
	}

	// 自动命名回退
	seedScan(t, a, []store.Observation{{Port: 6690, Protocol: "tcp", State: "system", Source: "dict"}}, "ok")
	it, _ = a.GetPort(6690, "tcp")
	if it == nil || it.NameSource != "auto" || it.Name == "" {
		t.Fatalf("系统端口应自动命名: %+v", it)
	}

	if got := StateLabel("nope"); got != "nope" {
		t.Errorf("未知状态应原样返回，实际 %s", got)
	}
	if _, err := a.GetPort(65000, "tcp"); err != nil {
		t.Errorf("不存在的端口不应报错: %v", err)
	}
}

func TestApplySettingsAndDictReload(t *testing.T) {
	a := newTestApp(t)
	if err := a.Store.PutSetting("scan_ranges", "1000-2000"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutSetting("scan_interval", "60"); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.PutSetting("scan_host_proc", "true"); err != nil {
		t.Fatal(err)
	}
	if err := a.ApplySettings(); err != nil {
		t.Fatalf("ApplySettings: %v", err)
	}
	if a.Cfg.ScanRanges != "1000-2000" || a.Cfg.ScanInterval != 60*time.Second || !a.Cfg.ScanHostProc {
		t.Fatalf("设置未生效: %+v", a.Cfg)
	}
	// 非法范围
	_ = a.Store.PutSetting("scan_ranges", "9000-8000")
	if err := a.ApplySettings(); err == nil {
		t.Error("非法范围应报错")
	}

	// 字典重载
	before := a.Dict().Size()
	if _, err := a.Store.AddDictUser(store.DictUserEntry{Port: 25000, Protocol: "tcp", Category: "自定义", DisplayName: "X"}); err != nil {
		t.Fatal(err)
	}
	if err := a.ReloadDict(); err != nil {
		t.Fatalf("ReloadDict: %v", err)
	}
	if a.Dict().Size() != before+1 {
		t.Errorf("字典重载未生效: %d -> %d", before, a.Dict().Size())
	}
}

func TestValidateSettings(t *testing.T) {
	good := []map[string]any{
		{"scan_ranges": "1-65535"},
		{"scan_interval": float64(0)},
		{"scan_compose_dir": "/compose"},
	}
	for _, m := range good {
		if err := ValidateSettings(m); err != nil {
			t.Errorf("ValidateSettings(%v) 不应报错: %v", m, err)
		}
	}
	bad := []map[string]any{
		{"scan_ranges": "9000-8000"},
		{"scan_interval": float64(-1)},
		{"scan_interval": float64(99999)},
	}
	for _, m := range bad {
		if err := ValidateSettings(m); err == nil {
			t.Errorf("ValidateSettings(%v) 应报错", m)
		}
	}
}

// 回归：仅有用户台账（无观测）的端口在空闲筛选中只应出现一次。
// 此前 buildItems 未把该类端口计入 occupied，free 枚举会再列一次，列表出现重复行。
func TestMetaOnlyPortNotDuplicatedInFreeFilter(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
	}, "ok")
	if err := a.Store.PutPortMeta(store.PortMeta{Port: 25000, Protocol: "tcp", Name: "我的服务", UpdatedBy: "test"}); err != nil {
		t.Fatal(err)
	}

	res, err := a.List(ListQuery{States: []string{"free"}, From: 25000, To: 25000, PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, it := range res.Items {
		if it.Port == 25000 && it.Protocol == "tcp" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("端口 25000/tcp 在空闲筛选中出现 %d 次，期望 1 次", n)
	}
}

// 回归：metaOnly 端口仍应参与列表展示（不可因为去重而消失）。
func TestMetaOnlyPortVisibleInList(t *testing.T) {
	a := newTestApp(t)
	seedScan(t, a, []store.Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
	}, "ok")
	if err := a.Store.PutPortMeta(store.PortMeta{Port: 26000, Protocol: "tcp", Name: "台账端口", UpdatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	res, err := a.List(ListQuery{From: 26000, To: 26000})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Name != "台账端口" {
		t.Fatalf("仅有台账的端口应展示且带用户命名: %+v", res.Items)
	}
}
