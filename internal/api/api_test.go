package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/kyson-666/port-ledger/internal/app"
	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *app.App) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		AdminPort: 443, DataDir: dir,
		LogDir: filepath.Join(dir, "logs"), BackupDir: filepath.Join(dir, "backups"),
		DBPath: filepath.Join(dir, "port-ledger.db"), LogLevel: "info",
		ScanHostProc: false, ScanDockerSocket: false, ScanComposeDir: "",
		ScanRanges: "1-65535", ScanInterval: 0,
		ProcRoot: filepath.Join(dir, "proc"), DockerHost: "unix:///var/run/docker.sock",
		SelfContainer: "port-ledger", Version: "test",
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	es, _ := dict.Builtin()
	if err := st.SeedDict(es); err != nil {
		t.Fatalf("SeedDict: %v", err)
	}
	lg, err := logx.New(cfg.LogDir, logx.Info, false)
	if err != nil {
		t.Fatalf("logx.New: %v", err)
	}
	a, err := app.New(cfg, st, lg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>PortLedger</title>")},
	}
	srv, err := New(a, lg, assets)
	if err != nil {
		t.Fatalf("api.New: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(func() { ts.Close(); lg.Close(); st.Close() })
	return ts, a
}

func seed(t *testing.T, a *app.App, obs []store.Observation, path string) {
	t.Helper()
	id, err := a.Store.BeginScan(path)
	if err != nil {
		t.Fatalf("BeginScan: %v", err)
	}
	if err := a.Store.ReplaceObservations(id, obs); err != nil {
		t.Fatalf("ReplaceObservations: %v", err)
	}
	if err := a.Store.FinishScan(id, "ok", "{}", "[]"); err != nil {
		t.Fatalf("FinishScan: %v", err)
	}
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any) (int, map[string]any, string) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rdr = strings.NewReader(b)
		default:
			buf, _ := json.Marshal(b)
			rdr = bytes.NewReader(buf)
		}
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m, string(raw)
}

func num(m map[string]any, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}

const seedObs = `[{"port":5000,"protocol":"tcp","state":"system","source":"dict"},
{"port":9000,"protocol":"tcp","state":"in-use(docker)","source":"docker","container_name":"portainer","image":"portainer/portainer-ce"},
{"port":8081,"protocol":"tcp","state":"in-use(host)","source":"proc","pid":12,"process_name":"nginx","cmdline":"nginx -g daemon off;"},
{"port":6690,"protocol":"tcp","state":"system","source":"dict","conflict":true,"container_name":"drive","image":"synology/drive"},
{"port":7000,"protocol":"tcp","state":"configured","source":"compose","compose_file":"/compose/a.yml#web"},
{"port":21000,"protocol":"tcp","state":"reserved","source":"reserve"}]`

func seedStd(t *testing.T, a *app.App) {
	t.Helper()
	var obs []store.Observation
	if err := json.Unmarshal([]byte(seedObs), &obs); err != nil {
		t.Fatalf("fixture 解析: %v", err)
	}
	seed(t, a, obs, "ranges=1-65535")
}

/* ---------- 健康与扫描 ---------- */

func TestAPIHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	code, m, raw := do(t, ts, "GET", "/api/health", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if m["status"] != "ok" || m["version"] != "test" {
		t.Fatalf("健康检查字段不符: %v", m)
	}
	if _, ok := m["source_health"]; !ok {
		t.Error("缺少 source_health")
	}
	if _, ok := m["scanning"]; !ok {
		t.Error("缺少 scanning")
	}
	if num(m, "dict_size") == 0 {
		t.Error("dict_size 应大于 0")
	}
	cfg, _ := m["config"].(map[string]any)
	if cfg == nil || num(cfg, "admin_port") != 443 {
		t.Errorf("config.admin_port 不符: %v", cfg)
	}
}

func TestAPIScan(t *testing.T) {
	ts, _ := newTestServer(t)
	code, m, raw := do(t, ts, "POST", "/api/scan", map[string]any{})
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if m["status"] != "ok" {
		t.Errorf("status=%v", m["status"])
	}
	counts, _ := m["counts"].(map[string]any)
	if counts == nil || num(counts, "system") == 0 {
		t.Fatalf("counts 不符: %v", m)
	}
	// 扫描后列表应可读
	code, m, _ = do(t, ts, "GET", "/api/ports?page_size=1", nil)
	if code != 200 || num(m, "total") == 0 {
		t.Fatalf("扫描后列表为空: %v", m)
	}
}

func TestAPIScanBadBody(t *testing.T) {
	ts, _ := newTestServer(t)
	code, _, raw := do(t, ts, "POST", "/api/scan", "{not json")
	if code != 400 {
		t.Fatalf("非法 JSON 应返回 400，实际 %d raw=%s", code, raw)
	}
}

/* ---------- 端口列表 ---------- */

func TestAPIPortsList(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)

	code, m, raw := do(t, ts, "GET", "/api/ports", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if num(m, "total") != 6 {
		t.Fatalf("默认应为 6 条（不含空闲），实际 %v", m["total"])
	}
	items, _ := m["items"].([]any)
	if len(items) != 6 {
		t.Fatalf("items 长度 = %d", len(items))
	}
	first, _ := items[0].(map[string]any)
	if num(first, "port") != 123 && num(first, "port") != 5000 {
		t.Logf("首条: %v", first)
	}

	// 过滤
	code, m, _ = do(t, ts, "GET", "/api/ports?state=in-use(docker)", nil)
	if code != 200 || num(m, "total") != 1 {
		t.Fatalf("状态过滤失败: %v", m)
	}
	code, m, _ = do(t, ts, "GET", "/api/ports?protocol=udp", nil)
	if code != 200 || num(m, "total") != 0 {
		t.Fatalf("协议过滤失败: %v", m)
	}
	code, m, _ = do(t, ts, "GET", "/api/ports?q=portainer", nil)
	if code != 200 || num(m, "total") != 1 {
		t.Fatalf("关键字过滤失败: %v", m)
	}
	code, m, _ = do(t, ts, "GET", "/api/ports?source=proc", nil)
	if code != 200 || num(m, "total") != 1 {
		t.Fatalf("来源过滤失败: %v", m)
	}
	// 空闲枚举
	code, m, _ = do(t, ts, "GET", "/api/ports?state=free&from=19998&to=20002", nil)
	if code != 200 || num(m, "total") != 10 {
		t.Fatalf("空闲枚举应为 10 条（tcp+udp），实际 %v", m["total"])
	}
	// 冲突标记应透出
	code, m, _ = do(t, ts, "GET", "/api/ports?q=6690", nil)
	items, _ = m["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("6690 应命中 1 条: %v", m)
	}
	i0, _ := items[0].(map[string]any)
	if i0["conflict"] != true {
		t.Errorf("conflict 未透出: %v", i0)
	}
	if i0["name_source"] != "auto" || i0["name"] == "" {
		t.Errorf("系统端口应自动命名: %v", i0)
	}
	// 计数
	counts, _ := m["counts"].(map[string]any)
	if counts == nil {
		t.Fatal("缺少 counts")
	}
}

func TestAPIPortGetAndPut(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)

	code, m, raw := do(t, ts, "GET", "/api/ports/9000?protocol=tcp", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if m["state"] != "in-use(docker)" || m["state_label"] != "容器" {
		t.Fatalf("端口详情不符: %v", m)
	}
	if m["container_name"] != "portainer" {
		t.Errorf("应带容器名: %v", m)
	}

	// 400：非法端口
	if code, _, _ := do(t, ts, "GET", "/api/ports/99999", nil); code != 400 {
		t.Errorf("非法端口应 400，实际 %d", code)
	}
	// 404：无观测
	if code, _, _ := do(t, ts, "GET", "/api/ports/65000", nil); code != 404 {
		t.Errorf("不存在端口应 404，实际 %d", code)
	}

	// 命名
	code, m, raw = do(t, ts, "PUT", "/api/ports/9000", map[string]any{
		"protocol": "tcp", "name": "Portainer 面板", "note": "容器管理", "tags": "容器,运维", "project": "portainer",
	})
	if code != 200 {
		t.Fatalf("PUT code=%d raw=%s", code, raw)
	}
	if m["name"] != "Portainer 面板" || m["name_source"] != "user" {
		t.Fatalf("命名未生效: %v", m)
	}
	if m["project"] != "portainer" || m["note"] != "容器管理" {
		t.Fatalf("元数据未生效: %v", m)
	}

	// 校验：名称过长
	long := strings.Repeat("字", 61)
	if code, _, _ := do(t, ts, "PUT", "/api/ports/9000", map[string]any{"name": long}); code != 400 {
		t.Errorf("名称过长应 400，实际 %d", code)
	}
	// 校验：备注过长
	if code, _, _ := do(t, ts, "PUT", "/api/ports/9000", map[string]any{"note": strings.Repeat("a", 201)}); code != 400 {
		t.Errorf("备注过长应 400，实际 %d", code)
	}
	// 校验：标签过多
	if code, _, _ := do(t, ts, "PUT", "/api/ports/9000", map[string]any{"tags": "1,2,3,4,5,6,7,8,9"}); code != 400 {
		t.Errorf("标签过多应 400，实际 %d", code)
	}
	// 校验：协议非法
	if code, _, _ := do(t, ts, "PUT", "/api/ports/9000", map[string]any{"protocol": "sctp"}); code != 400 {
		t.Errorf("非法协议应 400，实际 %d", code)
	}

	// 回退系统名（全空 → 删除台账记录）
	code, m, _ = do(t, ts, "PUT", "/api/ports/6690", map[string]any{"protocol": "tcp", "name": "自定义名"})
	if code != 200 || m["name_source"] != "user" {
		t.Fatalf("自定义命名失败: %v", m)
	}
	code, m, _ = do(t, ts, "PUT", "/api/ports/6690", map[string]any{"protocol": "tcp"})
	if code != 200 {
		t.Fatalf("回退失败: %d", code)
	}
	if m["name_source"] != "auto" || m["name"] == "" {
		t.Fatalf("回退后应显示系统名: %v", m)
	}
}

/* ---------- 空闲端口 ---------- */

func TestAPIFree(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)
	code, m, raw := do(t, ts, "GET", "/api/free?from=20000&to=20010&count=5&protocol=tcp", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if num(m, "count") != 5 {
		t.Fatalf("count=%v", m["count"])
	}
	ports, _ := m["ports"].([]any)
	if len(ports) != 5 || num2(ports[0]) != 20000 {
		t.Fatalf("ports 不符: %v", ports)
	}
	ranges, _ := m["ranges"].([]any)
	if len(ranges) != 1 {
		t.Fatalf("区间应为 1 段: %v", ranges)
	}
	r0, _ := ranges[0].(map[string]any)
	if num(r0, "from") != 20000 || num(r0, "to") != 20010 || num(r0, "count") != 11 {
		t.Fatalf("区间不符: %v", r0)
	}
}

func num2(v any) float64 {
	f, _ := v.(float64)
	return f
}

/* ---------- 保留 ---------- */

func TestAPIReservations(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)

	// 占用端口不可保留
	code, m, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 9000, "protocol": "tcp", "owner": "k"})
	if code != 409 {
		t.Fatalf("已占用端口应 409，实际 %d（%v）", code, m)
	}
	if s, _ := m["error"].(string); !strings.Contains(s, "已被占用") {
		t.Errorf("错误文案不符: %v", m["error"])
	}
	// 非法端口
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 0}); code != 400 {
		t.Error("端口 0 应 400")
	}
	// 非法时间
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "expires_at": "not-a-date"}); code != 400 {
		t.Error("非法时间应 400")
	}

	// 正常创建
	code, m, raw := do(t, ts, "POST", "/api/reservations", map[string]any{
		"port": 21001, "protocol": "tcp", "owner": "kyson", "project": "nginx", "reason": "待部署", "expires_at": "2030-01-01",
	})
	if code != 201 {
		t.Fatalf("创建保留 code=%d raw=%s", code, raw)
	}
	id := num(m, "id")
	if id == 0 {
		t.Fatalf("应返回 id: %v", m)
	}

	// 列表
	code, m, _ = do(t, ts, "GET", "/api/reservations", nil)
	if code != 200 || num(m, "total") != 1 {
		t.Fatalf("列表不符: %v", m)
	}
	items, _ := m["items"].([]any)
	r0, _ := items[0].(map[string]any)
	if r0["expired"] != false || r0["owner"] != "kyson" || r0["project"] != "nginx" {
		t.Fatalf("保留字段不符: %v", r0)
	}

	// 过滤
	if _, m, _ := do(t, ts, "GET", "/api/reservations?state=expired", nil); num(m, "total") != 0 {
		t.Error("过期过滤失败")
	}
	if _, m, _ := do(t, ts, "GET", "/api/reservations?state=active", nil); num(m, "total") != 1 {
		t.Error("有效过滤失败")
	}
	if _, m, _ := do(t, ts, "GET", "/api/reservations?q=nginx", nil); num(m, "total") != 1 {
		t.Error("关键字过滤失败")
	}

	// 更新（续期）
	code, _, raw = do(t, ts, "PUT", "/api/reservations", map[string]any{"id": id, "owner": "kyson2", "project": "nginx2", "expires_at": "2031-01-01"})
	if code != 200 {
		t.Fatalf("更新 code=%d raw=%s", code, raw)
	}
	_, m, _ = do(t, ts, "GET", "/api/reservations", nil)
	items, _ = m["items"].([]any)
	r0, _ = items[0].(map[string]any)
	if r0["owner"] != "kyson2" || r0["project"] != "nginx2" {
		t.Fatalf("更新未生效: %v", r0)
	}

	// 释放
	if code, _, _ := do(t, ts, "DELETE", "/api/reservations?id="+intStr(int(id)), nil); code != 200 {
		t.Fatalf("释放失败: %d", code)
	}
	if _, m, _ := do(t, ts, "GET", "/api/reservations", nil); num(m, "total") != 0 {
		t.Error("释放后应为空")
	}
	// 缺 id
	if code, _, _ := do(t, ts, "DELETE", "/api/reservations", nil); code != 400 {
		t.Error("缺 id 应 400")
	}
}

func intStr(n int) string { return strings.TrimSpace(strings.ReplaceAll(jsonNum(n), "\"", "")) }
func jsonNum(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

/* ---------- 字典 ---------- */

func TestAPIDict(t *testing.T) {
	ts, _ := newTestServer(t)

	code, m, raw := do(t, ts, "GET", "/api/system-dict", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if num(m, "builtin_count") < 100 {
		t.Fatalf("builtin_count=%v", m["builtin_count"])
	}
	cats, _ := m["categories"].([]any)
	if len(cats) < 8 {
		t.Fatalf("categories 过少: %v", cats)
	}
	// 覆盖
	code, _, raw = do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "override", "port": 6690, "protocol": "tcp", "display_name": "Drive 同步（改名）"})
	if code != 200 {
		t.Fatalf("覆盖 code=%d raw=%s", code, raw)
	}
	_, m, _ = do(t, ts, "GET", "/api/system-dict?q=Drive", nil)
	items, _ := m["entries"].([]any)
	if len(items) == 0 {
		t.Fatal("覆盖后应可查到")
	}
	found := false
	for _, it := range items {
		e, _ := it.(map[string]any)
		if num(e, "port") == 6690 {
			if e["type"] != "overridden" || e["display_name"] != "Drive 同步（改名）" {
				t.Fatalf("覆盖未生效: %v", e)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("未找到 6690 覆盖条目")
	}

	// 新增自定义
	code, m, _ = do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "create", "port": 25000, "port_end": 25010, "protocol": "tcp", "category": "我的服务", "display_name": "内部工具", "description": "d"})
	if code != 201 {
		t.Fatalf("新增 code=%d（%v）", code, m)
	}
	cid := num(m, "id")
	if cid == 0 {
		t.Fatal("应返回 id")
	}
	// 空名称
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "create", "port": 25020, "display_name": ""}); code != 400 {
		t.Error("空名称应 400")
	}
	// 端口越界
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "create", "port": 70000, "display_name": "x"}); code != 400 {
		t.Error("端口越界应 400")
	}
	// 未知 action
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "boom"}); code != 400 {
		t.Error("未知 action 应 400")
	}

	// 编辑
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "update", "id": cid, "port": 25000, "protocol": "tcp", "category": "我的服务", "display_name": "内部工具2"}); code != 200 {
		t.Error("编辑失败")
	}
	// 删除
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "delete", "id": cid}); code != 200 {
		t.Error("删除失败")
	}
	// 恢复内置
	if code, _, _ := do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "restore", "port": 6690, "protocol": "tcp"}); code != 200 {
		t.Error("恢复内置失败")
	}
	_, m, _ = do(t, ts, "GET", "/api/system-dict?q=Synology%20Drive", nil)
	items, _ = m["entries"].([]any)
	for _, it := range items {
		e, _ := it.(map[string]any)
		if num(e, "port") == 6690 && e["type"] != "builtin" {
			t.Fatalf("恢复内置后类型应为 builtin: %v", e)
		}
	}

	// 导出
	code, m, raw = do(t, ts, "GET", "/api/dict/export", nil)
	if code != 200 {
		t.Fatalf("导出 code=%d", code)
	}
	if _, ok := m["builtin"]; !ok {
		t.Fatalf("导出缺少 builtin: %s", raw[:minIntVal(120, len(raw))])
	}

	// 导入（预览 + 应用）
	payload := map[string]any{"user": []any{map[string]any{"port": 26000, "protocol": "tcp", "category": "导入", "display_name": "导入条目"}}, "overrides": []any{}}
	code, m, _ = do(t, ts, "POST", "/api/dict/import?preview=1", payload)
	if code != 200 || m["preview"] != true || num(m, "new") != 1 {
		t.Fatalf("导入预览不符: %v", m)
	}
	code, m, _ = do(t, ts, "POST", "/api/dict/import", payload)
	if code != 200 {
		t.Fatalf("导入 code=%d（%v）", code, m)
	}
	_, m, _ = do(t, ts, "GET", "/api/system-dict?type=custom", nil)
	if num(m, "total") < 1 {
		t.Fatal("导入后应有自定义条目")
	}
	// 空导入
	if code, _, _ := do(t, ts, "POST", "/api/dict/import", map[string]any{"user": []any{}}); code != 400 {
		t.Error("空导入应 400")
	}
	// 非法 JSON
	if code, _, _ := do(t, ts, "POST", "/api/dict/import", "{oops"); code != 400 {
		t.Error("非法 JSON 应 400")
	}
}

func minIntVal(a, b int) int {
	if a < b {
		return a
	}
	return b
}

/* ---------- 审计与日志 ---------- */

func TestAPIAuditAndLogs(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)
	// 产生审计
	_, _, _ = do(t, ts, "PUT", "/api/ports/9000", map[string]any{"protocol": "tcp", "name": "X"})
	_, _, _ = do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "protocol": "tcp", "owner": "o"})

	code, m, raw := do(t, ts, "GET", "/api/audit", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if num(m, "total") < 2 {
		t.Fatalf("审计条数不足: %v", m)
	}
	_, m, _ = do(t, ts, "GET", "/api/audit?action=rename", nil)
	if num(m, "total") != 1 {
		t.Fatalf("按动作过滤失败: %v", m)
	}

	code, m, raw = do(t, ts, "GET", "/api/logs", nil)
	if code != 200 {
		t.Fatalf("日志 code=%d raw=%s", code, raw)
	}
	if _, ok := m["items"]; !ok {
		t.Fatalf("日志缺少 items: %v", m)
	}
	// 未知日期
	if code, _, _ := do(t, ts, "GET", "/api/logs?date=1999-01-01", nil); code != 404 {
		t.Errorf("未知日期应 404，实际 %d", code)
	}
	// 日志级别与关键字过滤
	_, m, _ = do(t, ts, "GET", "/api/logs?level=info&q=zzz-no-match", nil)
	items, _ := m["items"].([]any)
	if len(items) != 0 {
		t.Errorf("关键字无匹配应返回 0 条，实际 %d", len(items))
	}
}

/* ---------- 备份 ---------- */

func TestAPIBackup(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)
	_, _, _ = do(t, ts, "PUT", "/api/ports/9000", map[string]any{"protocol": "tcp", "name": "Portainer"})
	_, _, _ = do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "protocol": "tcp", "owner": "o", "project": "p"})
	_, _, _ = do(t, ts, "POST", "/api/system-dict", map[string]any{"action": "override", "port": 6690, "protocol": "tcp", "display_name": "改名"})
	_, _, _ = do(t, ts, "PUT", "/api/settings", map[string]any{"scan_interval": 120})

	code, m, raw := do(t, ts, "GET", "/api/backup/export", nil)
	if code != 200 {
		t.Fatalf("导出 code=%d raw=%s", code, raw)
	}
	if len(m["ports"].([]any)) != 1 || len(m["reservations"].([]any)) != 1 || len(m["dict_overrides"].([]any)) != 1 {
		t.Fatalf("导出内容不符: %v", m)
	}
	if settings, _ := m["settings"].(map[string]any); settings == nil || settings["scan_interval"] != "120" {
		t.Fatalf("导出设置不符: %v", m["settings"])
	}

	// 预览
	code, m, _ = do(t, ts, "POST", "/api/backup/import?preview=1", map[string]any{"ports": []any{}, "reservations": []any{}})
	if code != 400 {
		t.Fatalf("空预览应 400，实际 %d", code)
	}

	// 导入到同一实例（merge）
	code, m, _ = do(t, ts, "POST", "/api/backup/import", map[string]any{
		"mode": "merge",
		"ports": []any{map[string]any{"port": 8080, "protocol": "tcp", "name": "额外"}},
	})
	if code != 200 {
		t.Fatalf("导入 code=%d（%v）", code, m)
	}
	_, m, _ = do(t, ts, "GET", "/api/ports?q=8080", nil)
	if num(m, "total") < 1 {
		t.Fatal("导入的命名应作为台账行可见（即使该端口当前无观测）")
	}

	// replace 导入
	if code, _, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"mode": "replace", "ports": []any{}}); code != 400 {
		t.Error("空 replace 应 400")
	}
}

/* ---------- 设置 ---------- */

func TestAPISettings(t *testing.T) {
	ts, _ := newTestServer(t)
	code, m, raw := do(t, ts, "GET", "/api/settings", nil)
	if code != 200 {
		t.Fatalf("code=%d raw=%s", code, raw)
	}
	if _, ok := m["values"]; !ok {
		t.Fatal("缺少 values")
	}
	if _, ok := m["env_defaults"]; !ok {
		t.Fatal("缺少 env_defaults")
	}
	rk, _ := m["restart_required"].([]any)
	if len(rk) == 0 {
		t.Fatal("缺少 restart_required")
	}

	code, m, raw = do(t, ts, "PUT", "/api/settings", map[string]any{"scan_interval": 600, "scan_ranges": "1024-65535"})
	if code != 200 {
		t.Fatalf("PUT code=%d raw=%s", code, raw)
	}
	vals, _ := m["values"].(map[string]any)
	if num(vals, "scan_interval") != 600 || vals["scan_ranges"] != "1024-65535" {
		t.Fatalf("设置未生效: %v", vals)
	}
	ch, _ := m["changed"].([]any)
	if len(ch) != 2 {
		t.Fatalf("changed 不符: %v", m["changed"])
	}

	// 需重启项
	code, m, _ = do(t, ts, "PUT", "/api/settings", map[string]any{"tls_self_signed": true})
	if code != 200 {
		t.Fatalf("PUT tls code=%d", code)
	}
	rr, _ := m["restart_required"].([]any)
	if len(rr) != 1 || rr[0] != "tls_self_signed" {
		t.Fatalf("restart_required 不符: %v", m["restart_required"])
	}

	// 校验失败
	if code, _, _ := do(t, ts, "PUT", "/api/settings", map[string]any{"scan_ranges": "9000-8000"}); code != 400 {
		t.Error("非法范围应 400")
	}
	if code, _, _ := do(t, ts, "PUT", "/api/settings", map[string]any{"scan_interval": 999999}); code != 400 {
		t.Error("越界周期应 400")
	}
	// 审计应记录设置变更
	_, m, _ = do(t, ts, "GET", "/api/audit?action=setting_update", nil)
	if num(m, "total") < 1 {
		t.Error("设置变更应写审计")
	}
}

/* ---------- 静态资源 ---------- */

func TestAPIStaticAndFallback(t *testing.T) {
	ts, _ := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(body), "PortLedger") {
		t.Fatalf("首页不符: %d %s", res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("Content-Type 不符: %s", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("HTML 应 no-store，实际 %s", cc)
	}
	// SPA 回退
	res2, _ := http.Get(ts.URL + "/ports")
	b2, _ := io.ReadAll(res2.Body)
	res2.Body.Close()
	if res2.StatusCode != 200 || !strings.Contains(string(b2), "PortLedger") {
		t.Fatalf("SPA 回退失败: %d %s", res2.StatusCode, b2)
	}
	// API 不缓存
	res3, _ := http.Get(ts.URL + "/api/health")
	res3.Body.Close()
	if cc := res3.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("API 应 no-store，实际 %s", cc)
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/health", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("方法不允许应 405，实际 %d", res.StatusCode)
	}
}
