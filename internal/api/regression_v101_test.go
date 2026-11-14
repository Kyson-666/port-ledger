package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

/* ---------- v1.0.1 修复项回归 ---------- */

// PUT 未知设置键应被白名单拒绝（P1-2）。
func TestAPISettingsUnknownKeyRejected(t *testing.T) {
	ts, _ := newTestServer(t)
	code, m, _ := do(t, ts, "PUT", "/api/settings", map[string]any{"webhook_url": "http://x", "scan_interval": 300})
	if code != 400 {
		t.Fatalf("未知设置键应 400，实际 %d（%v）", code, m)
	}
	if s, _ := m["error"].(string); !strings.Contains(s, "未知设置项") {
		t.Errorf("错误文案不符: %v", m["error"])
	}
	// 被拒绝的整批键都不应落库
	_, m, _ = do(t, ts, "GET", "/api/settings", nil)
	vals, _ := m["values"].(map[string]any)
	if num(vals, "scan_interval") != 0 {
		t.Errorf("整批拒绝后 scan_interval 不应生效: %v", vals)
	}
}

// 设置重置应清库并回默认（P1-1 配套）。
func TestAPISettingsReset(t *testing.T) {
	ts, _ := newTestServer(t)
	if code, _, _ := do(t, ts, "PUT", "/api/settings", map[string]any{"scan_interval": 777}); code != 200 {
		t.Fatal("PUT 失败")
	}
	code, m, _ := do(t, ts, "DELETE", "/api/settings", nil)
	if code != 200 {
		t.Fatalf("reset code=%d（%v）", code, m)
	}
	_, m, _ = do(t, ts, "GET", "/api/settings", nil)
	vals, _ := m["values"].(map[string]any)
	if num(vals, "scan_interval") != 0 {
		t.Errorf("重置后 scan_interval 应回默认: %v", vals)
	}
	// 审计应记录 setting_reset
	_, m, _ = do(t, ts, "GET", "/api/audit?action=setting_reset", nil)
	if num(m, "total") < 1 {
		t.Error("重置应写审计")
	}
}

// 重复保留 409；更新/释放不存在的保留 404（P2）。
func TestAPIReservationDuplicateAndNotFound(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)
	code, m, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "protocol": "tcp", "owner": "a"})
	if code != 201 {
		t.Fatalf("首次创建应 201，实际 %d（%v）", code, m)
	}
	code, m, _ = do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "protocol": "tcp", "owner": "b"})
	if code != 409 {
		t.Fatalf("重复保留应 409，实际 %d（%v）", code, m)
	}
	if s, _ := m["error"].(string); !strings.Contains(s, "已有一条有效保留") {
		t.Errorf("错误文案不符: %v", m["error"])
	}

	// 更新/释放不存在的 id
	if code, m, _ := do(t, ts, "PUT", "/api/reservations", map[string]any{"id": 424242, "owner": "x"}); code != 404 {
		t.Errorf("更新不存在保留应 404，实际 %d（%v）", code, m)
	}
	if code, m, _ := do(t, ts, "DELETE", "/api/reservations?id=424242", nil); code != 404 {
		t.Errorf("释放不存在保留应 404，实际 %d（%v）", code, m)
	}

	// 释放后可重新保留
	_, m, _ = do(t, ts, "GET", "/api/reservations", nil)
	items, _ := m["items"].([]any)
	r0, _ := items[0].(map[string]any)
	if err := a.Store.DeleteReservation(int64(num(r0, "id"))); err != nil {
		t.Fatalf("DeleteReservation: %v", err)
	}
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 21001, "protocol": "tcp", "owner": "c"}); code != 201 {
		t.Errorf("释放后应可重新保留，实际 %d", code)
	}
}

// 备份导入：mode 非法 400、未知设置键 400、非法设置值 400、settings-only 放行（P2）。
func TestAPIBackupImportValidation(t *testing.T) {
	ts, _ := newTestServer(t)
	port := map[string]any{"port": 8080, "protocol": "tcp", "name": "x"}
	// mode 缺失/非法
	if code, _, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"ports": []any{port}}); code != 400 {
		t.Error("缺 mode 应 400")
	}
	if code, m, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"mode": "upsert", "ports": []any{port}}); code != 400 {
		t.Errorf("非法 mode 应 400，实际 %d（%v）", code, m)
	}
	// settings 未知键 / 非法值
	if code, _, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"mode": "merge", "settings": map[string]any{"evil_key": "1"}, "ports": []any{port}}); code != 400 {
		t.Error("未知设置键应 400")
	}
	if code, _, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"mode": "merge", "settings": map[string]any{"admin_port": "99999"}, "ports": []any{port}}); code != 400 {
		t.Error("非法 admin_port 应 400")
	}
	// settings-only 导入应放行并生效
	if code, m, _ := do(t, ts, "POST", "/api/backup/import", map[string]any{"mode": "merge", "settings": map[string]any{"scan_interval": "900"}}); code != 200 {
		t.Fatalf("settings-only 导入应 200，实际 %d（%v）", code, m)
	}
	_, m, _ := do(t, ts, "GET", "/api/settings", nil)
	vals, _ := m["values"].(map[string]any)
	if num(vals, "scan_interval") != 900 {
		t.Errorf("settings-only 导入未生效: %v", vals)
	}
}

// /api/free 参数强校验（P3）。
func TestAPIFreeParamValidation(t *testing.T) {
	ts, _ := newTestServer(t)
	if code, _, _ := do(t, ts, "GET", "/api/free?count=abc", nil); code != 400 {
		t.Error("count=abc 应 400")
	}
	if code, _, _ := do(t, ts, "GET", "/api/free?from=x", nil); code != 400 {
		t.Error("from=x 应 400")
	}
	if code, _, _ := do(t, ts, "GET", "/api/free?protocol=sctp", nil); code != 400 {
		t.Error("protocol=sctp 应 400")
	}
	if code, m, _ := do(t, ts, "GET", "/api/free?count=0", nil); code != 400 {
		t.Errorf("count=0 应 400，实际 %d（%v）", code, m)
	}
	if code, _, _ := do(t, ts, "GET", "/api/free?count=10", nil); code != 200 {
		t.Error("count=10 应 200")
	}
}

// 端口列表 state 过滤白名单（P3）。
func TestAPIPortsStateValidation(t *testing.T) {
	ts, a := newTestServer(t)
	seedStd(t, a)
	if code, _, _ := do(t, ts, "GET", "/api/ports?state=hacker", nil); code != 400 {
		t.Error("state=hacker 应 400")
	}
	if code, _, _ := do(t, ts, "GET", "/api/ports?state=in-use(docker)", nil); code != 200 {
		t.Error("合法 state 应 200")
	}
}

// 未知 API 路径 405 应返回 JSON 错误体（P2）。
func TestAPIMethodNotAllowedJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/api/health", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("应 405，实际 %d", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("405 应 JSON Content-Type，实际 %s body=%s", ct, raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("405 响应体应为 JSON: %s", raw)
	}
	if s, _ := m["error"].(string); s == "" {
		t.Errorf("405 JSON 缺少 error 字段: %s", raw)
	}
}

// 静态资源缺失应 404 而非回退 SPA；页面路径才回退（P3）。
func TestAPIStaticMissingAsset404(t *testing.T) {
	ts, _ := newTestServer(t)
	res, err := http.Get(ts.URL + "/no-such.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("缺失资源应 404，实际 %d", res.StatusCode)
	}
	res2, err := http.Get(ts.URL + "/some-page")
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != 200 {
		t.Errorf("页面路径应回退 200，实际 %d", res2.StatusCode)
	}
}

// 安全响应头（P3）。
func TestAPISecurityHeaders(t *testing.T) {
	ts, _ := newTestServer(t)
	res, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if v := res.Header.Get("X-Content-Type-Options"); v != "nosniff" {
		t.Errorf("X-Content-Type-Options 不符: %s", v)
	}
	if v := res.Header.Get("X-Frame-Options"); v != "DENY" {
		t.Errorf("X-Frame-Options 不符: %s", v)
	}
	if v := res.Header.Get("Content-Security-Policy"); !strings.Contains(v, "default-src 'self'") {
		t.Errorf("CSP 不符: %s", v)
	}
}

// 空集合应序列化为 [] 而非 null（P3）。
func TestAPIEmptyCollectionsJSON(t *testing.T) {
	ts, _ := newTestServer(t)
	_, _, raw := do(t, ts, "GET", "/api/ports", nil)
	if strings.Contains(raw, `"items":null`) {
		t.Errorf("空列表不应为 null: %s", raw)
	}
	_, _, raw = do(t, ts, "GET", "/api/audit", nil)
	if strings.Contains(raw, `"items":null`) {
		t.Errorf("审计空列表不应为 null: %s", raw)
	}
}

// 重复保留判定须按端口+协议（tcp/udp 互不冲突）。
func TestAPIReservationPerProtocol(t *testing.T) {
	ts, _ := newTestServer(t)
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 31001, "protocol": "tcp", "owner": "a"}); code != 201 {
		t.Fatal("tcp 创建失败")
	}
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 31001, "protocol": "udp", "owner": "b"}); code != 201 {
		t.Error("同端口 udp 应允许")
	}
	if code, _, _ := do(t, ts, "POST", "/api/reservations", map[string]any{"port": 31001, "protocol": "tcp", "owner": "c"}); code != 409 {
		t.Error("同端口同协议应 409")
	}
}
