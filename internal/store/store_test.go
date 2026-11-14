package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/kyson-666/port-ledger/internal/dict"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestOpenAndSeed(t *testing.T) {
	st := newStore(t)
	es, err := dict.Builtin()
	if err != nil {
		t.Fatalf("dict.Builtin: %v", err)
	}
	if err := st.SeedDict(es); err != nil {
		t.Fatalf("SeedDict: %v", err)
	}
	// 幂等
	if err := st.SeedDict(es); err != nil {
		t.Fatalf("SeedDict 二次调用应幂等: %v", err)
	}
	got, err := st.BuiltinDict()
	if err != nil {
		t.Fatalf("BuiltinDict: %v", err)
	}
	if len(got) != len(es) {
		t.Fatalf("内置字典条目数 = %d，期望 %d（重复插入？）", len(got), len(es))
	}
}

func TestPortMetaCRUD(t *testing.T) {
	st := newStore(t)
	if m, err := st.GetPortMeta(9000, "tcp"); err != nil || m != nil {
		t.Fatalf("未写入时应返回 nil: %+v %v", m, err)
	}
	if err := st.PutPortMeta(PortMeta{Port: 9000, Protocol: "tcp", Name: "Portainer", Note: "容器管理", Tags: "容器,运维", Project: "portainer", UpdatedBy: "kyson"}); err != nil {
		t.Fatalf("PutPortMeta: %v", err)
	}
	m, err := st.GetPortMeta(9000, "tcp")
	if err != nil || m == nil {
		t.Fatalf("GetPortMeta: %+v %v", m, err)
	}
	if m.Name != "Portainer" || m.Tags != "容器,运维" || m.Project != "portainer" {
		t.Fatalf("字段不符: %+v", m)
	}
	if m.CreatedAt == "" || m.UpdatedAt == "" {
		t.Fatalf("时间戳未填充: %+v", m)
	}
	// 覆盖更新
	if err := st.PutPortMeta(PortMeta{Port: 9000, Protocol: "tcp", Name: "新名字"}); err != nil {
		t.Fatalf("PutPortMeta 更新: %v", err)
	}
	m, _ = st.GetPortMeta(9000, "tcp")
	if m.Name != "新名字" || m.Note != "" {
		t.Fatalf("应整体覆盖: %+v", m)
	}
	all, err := st.AllPortMeta()
	if err != nil || len(all) != 1 {
		t.Fatalf("AllPortMeta: %d %v", len(all), err)
	}
	if err := st.DeletePortMeta(9000, "tcp"); err != nil {
		t.Fatalf("DeletePortMeta: %v", err)
	}
	if m, _ := st.GetPortMeta(9000, "tcp"); m != nil {
		t.Fatal("删除后应查不到")
	}
}

func TestReservations(t *testing.T) {
	st := newStore(t)
	future := time.Now().Add(48 * time.Hour).Format(time.RFC3339)
	past := time.Now().Add(-2 * time.Hour).Format(time.RFC3339)

	id1, err := st.CreateReservation(Reservation{Port: 21000, Protocol: "tcp", Owner: "kyson", Project: "nginx", Reason: "待部署", ExpiresAt: future})
	if err != nil {
		t.Fatalf("CreateReservation: %v", err)
	}
	if _, err := st.CreateReservation(Reservation{Port: 21001, Protocol: "tcp", Owner: "x", ExpiresAt: past}); err != nil {
		t.Fatalf("CreateReservation 2: %v", err)
	}
	if _, err := st.CreateReservation(Reservation{Port: 21002, Protocol: "udp", Owner: "y"}); err != nil {
		t.Fatalf("CreateReservation 3: %v", err)
	}

	list, err := st.ListReservations()
	if err != nil || len(list) != 3 {
		t.Fatalf("ListReservations: %d %v", len(list), err)
	}
	byID := map[int64]Reservation{}
	for _, r := range list {
		byID[r.ID] = r
	}
	if byID[id1].Expired {
		t.Error("未来过期时间不应标记为过期")
	}
	// 排序：最新在前
	if list[0].Port != 21002 {
		t.Errorf("应按 id 倒序，实际首条 %d", list[0].Port)
	}

	active, err := st.ActiveReservations()
	if err != nil {
		t.Fatalf("ActiveReservations: %v", err)
	}
	if len(active) != 2 {
		t.Fatalf("未过期保留应为 2 条，实际 %d（%+v）", len(active), active)
	}
	if _, ok := active["21000/tcp"]; !ok {
		t.Error("缺少 21000/tcp")
	}
	if _, ok := active["21001/tcp"]; ok {
		t.Error("已过期保留不应出现在 active 中")
	}
	if _, ok := active["21002/udp"]; !ok {
		t.Error("长期保留应出现在 active 中")
	}

	if err := st.UpdateReservation(id1, "kyson2", "nginx2", "续期", ""); err != nil {
		t.Fatalf("UpdateReservation: %v", err)
	}
	m, _ := st.GetPortMeta(1, "tcp") // 无关调用，确认不 panic
	_ = m
	list, _ = st.ListReservations()
	for _, r := range list {
		if r.ID == id1 && (r.Owner != "kyson2" || r.ExpiresAt != "") {
			t.Errorf("更新未生效: %+v", r)
		}
	}

	if err := st.DeleteReservation(id1); err != nil {
		t.Fatalf("DeleteReservation: %v", err)
	}
	list, _ = st.ListReservations()
	if len(list) != 2 {
		t.Fatalf("删除后应剩 2 条，实际 %d", len(list))
	}
}

func TestObservationsAndScans(t *testing.T) {
	st := newStore(t)
	if n, err := st.CountScans(); err != nil || n != 0 {
		t.Fatalf("CountScans 初值: %d %v", n, err)
	}
	if last, err := st.LatestScan(); err != nil || last != nil {
		t.Fatalf("无扫描时应返回 nil: %+v %v", last, err)
	}

	id, err := st.BeginScan("ranges=1-65535")
	if err != nil {
		t.Fatalf("BeginScan: %v", err)
	}
	// running 状态不应被 LatestScan 返回
	if last, _ := st.LatestScan(); last != nil {
		t.Fatal("running 状态的扫描不应被 LatestScan 返回")
	}

	obs := []Observation{
		{Port: 5000, Protocol: "tcp", State: "system", Source: "dict"},
		{Port: 9000, Protocol: "tcp", State: "in-use(docker)", Source: "docker", ContainerName: "portainer", Image: "portainer/portainer-ce"},
		{Port: 8081, Protocol: "tcp", State: "in-use(host)", Source: "proc", PID: 123, ProcessName: "nginx", Cmdline: "nginx -g"},
		{Port: 6690, Protocol: "tcp", State: "system", Source: "dict", Conflict: true, ContainerName: "drive"},
	}
	if err := st.ReplaceObservations(id, obs); err != nil {
		t.Fatalf("ReplaceObservations: %v", err)
	}
	// 幂等替换
	if err := st.ReplaceObservations(id, obs); err != nil {
		t.Fatalf("ReplaceObservations 二次: %v", err)
	}
	if err := st.FinishScan(id, "ok", `{"system":2}`, `[{"name":"proc"}]`); err != nil {
		t.Fatalf("FinishScan: %v", err)
	}

	got, err := st.ObservationsByScan(id)
	if err != nil {
		t.Fatalf("ObservationsByScan: %v", err)
	}
	if len(got) != len(obs) {
		t.Fatalf("观测条数 = %d，期望 %d", len(got), len(obs))
	}
	var conflict int
	for _, o := range got {
		if o.Conflict {
			conflict++
			if o.ContainerName != "drive" {
				t.Errorf("冲突观测应保留容器名: %+v", o)
			}
		}
		if o.ObservedAt == "" {
			t.Errorf("观测时间未填充: %+v", o)
		}
	}
	if conflict != 1 {
		t.Errorf("冲突观测数 = %d，期望 1", conflict)
	}

	last, err := st.LatestScan()
	if err != nil || last == nil {
		t.Fatalf("LatestScan: %+v %v", last, err)
	}
	if last.Status != "ok" || last.FinishedAt == "" || last.Scope != "ranges=1-65535" {
		t.Fatalf("扫描记录不符: %+v", last)
	}
	if n, _ := st.CountScans(); n != 1 {
		t.Fatalf("CountScans = %d", n)
	}
}

func TestAudit(t *testing.T) {
	st := newStore(t)
	_ = st.AddAudit(Audit{Action: "rename", Target: "端口 9000/TCP", Detail: "名称 — → Portainer"})
	_ = st.AddAudit(Audit{Action: "reserve", Target: "端口 21000/TCP", Detail: "保留"})
	_ = st.AddAudit(Audit{Actor: "alice", Action: "rename", Target: "端口 5000/TCP", Detail: "x"})

	all, err := st.ListAudit("", "", 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("ListAudit: %d %v", len(all), err)
	}
	if all[0].TS == "" {
		t.Error("审计时间未填充")
	}
	// 默认操作人
	for _, a := range all {
		if a.Action != "rename" || a.Target == "端口 9000/TCP" {
			continue
		}
	}
	renames, _ := st.ListAudit("rename", "", 0)
	if len(renames) != 2 {
		t.Fatalf("按动作过滤应为 2 条，实际 %d", len(renames))
	}
	one, _ := st.ListAudit("", "21000", 0)
	if len(one) != 1 || one[0].Action != "reserve" {
		t.Fatalf("按对象过滤不符: %+v", one)
	}
	limited, _ := st.ListAudit("", "", 1)
	if len(limited) != 1 {
		t.Fatalf("limit 未生效: %d", len(limited))
	}
	// 默认操作人
	_, _ = st.ListAudit("reserve", "", 0)
	all2, _ := st.ListAudit("", "", 0)
	found := false
	for _, a := range all2 {
		if a.Action == "reserve" && a.Actor == "本地" {
			found = true
		}
	}
	if !found {
		t.Error("未指定操作人时应落为「本地」")
	}
}

func TestSettingsStore(t *testing.T) {
	st := newStore(t)
	if v, ok, err := st.GetSetting("scan_interval"); err != nil || ok {
		t.Fatalf("未设置时应返回 ok=false: %q %v %v", v, ok, err)
	}
	if err := st.PutSetting("scan_interval", "600"); err != nil {
		t.Fatalf("PutSetting: %v", err)
	}
	if err := st.PutSetting("scan_interval", "900"); err != nil {
		t.Fatalf("PutSetting 覆盖: %v", err)
	}
	v, ok, err := st.GetSetting("scan_interval")
	if err != nil || !ok || v != "900" {
		t.Fatalf("覆盖未生效: %q %v %v", v, ok, err)
	}
	all, err := st.AllSettings()
	if err != nil || all["scan_interval"] != "900" {
		t.Fatalf("AllSettings: %+v %v", all, err)
	}
}

func TestDictUserAndOverride(t *testing.T) {
	st := newStore(t)
	es, _ := dict.Builtin()
	if err := st.SeedDict(es); err != nil {
		t.Fatalf("SeedDict: %v", err)
	}
	base, _ := st.EffectiveDict()

	// 新增自定义
	id, err := st.AddDictUser(DictUserEntry{Port: 25000, PortEnd: 25010, Protocol: "tcp", Category: "自定义", DisplayName: "我的服务", Description: "内部"})
	if err != nil {
		t.Fatalf("AddDictUser: %v", err)
	}
	eff, _ := st.EffectiveDict()
	if len(eff) != len(base)+1 {
		t.Fatalf("自定义条目未生效: %d vs %d", len(eff), len(base))
	}
	found := false
	for _, e := range eff {
		if e.Port == 25000 && e.Type == "custom" && e.DisplayName == "我的服务" {
			found = true
		}
	}
	if !found {
		t.Fatal("未找到自定义条目")
	}

	// 覆盖内置（6690）
	if err := st.PutDictOverride(6690, "tcp", "Drive 同步（已改名）", ""); err != nil {
		t.Fatalf("PutDictOverride: %v", err)
	}
	has, _ := st.HasOverride(6690, "tcp")
	if !has {
		t.Fatal("HasOverride 应为 true")
	}
	eff, _ = st.EffectiveDict()
	var overlapped bool
	for _, e := range eff {
		if e.Port == 6690 && e.Protocol == "tcp" {
			if e.Type != "overridden" || e.DisplayName != "Drive 同步（已改名）" {
				t.Fatalf("覆盖未生效: %+v", e)
			}
			overlapped = true
		}
	}
	if !overlapped {
		t.Fatal("未找到 6690 条目")
	}

	// 恢复内置
	if err := st.DeleteDictOverride(6690, "tcp"); err != nil {
		t.Fatalf("DeleteDictOverride: %v", err)
	}
	eff, _ = st.EffectiveDict()
	for _, e := range eff {
		if e.Port == 6690 && e.Protocol == "tcp" && e.Type != "builtin" {
			t.Fatalf("恢复内置后不应为 overridden: %+v", e)
		}
	}

	// 编辑与删除自定义
	if err := st.UpdateDictUser(id, 25000, 0, "tcp", "自定义", "改后名称", ""); err != nil {
		t.Fatalf("UpdateDictUser: %v", err)
	}
	users, _ := st.ListDictUser()
	if len(users) != 1 || users[0].DisplayName != "改后名称" {
		t.Fatalf("编辑未生效: %+v", users)
	}
	if err := st.DeleteDictUser(id); err != nil {
		t.Fatalf("DeleteDictUser: %v", err)
	}
	if users, _ = st.ListDictUser(); len(users) != 0 {
		t.Fatalf("删除后应为空: %+v", users)
	}
}

func TestBackupExportImport(t *testing.T) {
	src := newStore(t)
	_ = src.PutPortMeta(PortMeta{Port: 9000, Protocol: "tcp", Name: "Portainer", Tags: "a,b", Project: "p"})
	_, _ = src.CreateReservation(Reservation{Port: 21000, Protocol: "tcp", Owner: "o", Project: "p"})
	_, _ = src.AddDictUser(DictUserEntry{Port: 25000, Protocol: "tcp", Category: "自定义", DisplayName: "X"})
	_ = src.PutDictOverride(6690, "tcp", "覆盖名", "")
	_ = src.PutSetting("scan_interval", "120")

	led, err := src.ExportLedger("test")
	if err != nil {
		t.Fatalf("ExportLedger: %v", err)
	}
	if len(led.Ports) != 1 || len(led.Reservations) != 1 || len(led.DictUser) != 1 || len(led.DictOverrides) != 1 || led.Settings["scan_interval"] != "120" {
		t.Fatalf("导出内容不符: %+v", led)
	}

	// merge 导入到新库
	dst := newStore(t)
	counts, err := dst.ImportLedger(led, "merge")
	if err != nil {
		t.Fatalf("ImportLedger merge: %v", err)
	}
	if counts["ports"] != 1 || counts["reservations"] != 1 || counts["dict_user"] != 1 || counts["dict_overrides"] != 1 {
		t.Fatalf("导入计数不符: %+v", counts)
	}
	m, _ := dst.GetPortMeta(9000, "tcp")
	if m == nil || m.Name != "Portainer" {
		t.Fatalf("导入后端口元数据不符: %+v", m)
	}
	if v, ok, _ := dst.GetSetting("scan_interval"); !ok || v != "120" {
		t.Fatalf("导入后设置不符: %q %v", v, ok)
	}

	// replace 导入应清空既有台账
	_ = dst.PutPortMeta(PortMeta{Port: 8080, Protocol: "tcp", Name: "额外"})
	if _, err := dst.ImportLedger(led, "replace"); err != nil {
		t.Fatalf("ImportLedger replace: %v", err)
	}
	all, _ := dst.AllPortMeta()
	if len(all) != 1 || all[0].Port != 9000 {
		t.Fatalf("replace 导入应清空既有台账: %+v", all)
	}
}
