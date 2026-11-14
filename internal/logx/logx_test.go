package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newLogger(t *testing.T, level Level) (*Logger, string) {
	t.Helper()
	dir := t.TempDir()
	l, err := New(dir, level, false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l, dir
}

func TestNewCreatesDirs(t *testing.T) {
	_, dir := newLogger(t, Info)
	for _, p := range []string{"app.log", "scan"} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("应创建 %s: %v", p, err)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	l, _ := newLogger(t, Info)
	l.Debugf("调试", nil)
	l.Infof("信息", nil)
	l.Warnf("告警", nil)
	l.Errorf("错误", nil)

	got := l.Tail(Debug, "", 0)
	if len(got) != 3 {
		t.Fatalf("Info 级别应记录 3 条（不含 debug），实际 %d", len(got))
	}
	if got[0].Level != "error" {
		t.Errorf("Tail 应最新在前，首条为 error，实际 %s", got[0].Level)
	}

	// 仅取 warn 以上
	got = l.Tail(Warn, "", 0)
	if len(got) != 2 {
		t.Fatalf("Warn 级别应 2 条，实际 %d", len(got))
	}
	for _, e := range got {
		if e.Level == "info" || e.Level == "debug" {
			t.Errorf("不应包含低级别日志: %+v", e)
		}
	}
}

func TestTailKeywordAndLimit(t *testing.T) {
	l, _ := newLogger(t, Debug)
	for _, m := range []string{"扫描完成", "扫描开始", "命名端口"} {
		l.Infof(m, map[string]any{"scan_id": 1})
	}
	got := l.Tail(Debug, "扫描", 0)
	if len(got) != 2 {
		t.Fatalf("关键字过滤应 2 条，实际 %d", len(got))
	}
	got = l.Tail(Debug, "", 1)
	if len(got) != 1 {
		t.Fatalf("limit 应生效，实际 %d", len(got))
	}
	got = l.Tail(Debug, "不存在的内容", 0)
	if len(got) != 0 {
		t.Fatalf("无匹配应返回空，实际 %d", len(got))
	}
}

func TestJSONLinesWritten(t *testing.T) {
	l, _ := newLogger(t, Debug)
	l.Infof("带字段", map[string]any{"port": 9000, "ok": true})
	l.Warnf("无字段", nil)

	b, err := os.ReadFile(l.AppLogPath())
	if err != nil {
		t.Fatalf("读取 app.log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("应有 2 行，实际 %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "{") || !strings.Contains(lines[0], `"msg":"带字段"`) {
		t.Errorf("首行不是合法 JSON 行: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"port":9000`) {
		t.Errorf("字段未序列化: %s", lines[0])
	}
	if !strings.Contains(lines[1], `"level":"warn"`) {
		t.Errorf("级别不符: %s", lines[1])
	}
}

func TestRingBufferBound(t *testing.T) {
	l, _ := newLogger(t, Debug)
	for i := 0; i < ringSize+50; i++ {
		l.Infof("条目", map[string]any{"i": i})
	}
	got := l.Tail(Debug, "", 0)
	if len(got) != ringSize {
		t.Fatalf("环形缓冲应封顶 %d 条，实际 %d", ringSize, len(got))
	}
	// 最旧的一条应已被挤出
	if got[len(got)-1].Msg != "条目" {
		t.Errorf("尾部消息不符: %s", got[len(got)-1].Msg)
	}
}

func TestRotation(t *testing.T) {
	l, dir := newLogger(t, Debug)
	l.Infof("第一份", nil)
	l.rotate()
	l.Infof("第二份", nil)

	for _, f := range []string{"app.log", "app.log.1"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("轮转后应存在 %s: %v", f, err)
		}
	}
	b1, _ := os.ReadFile(filepath.Join(dir, "app.log.1"))
	if !strings.Contains(string(b1), "第一份") {
		t.Errorf("app.log.1 应含轮转前内容: %s", b1)
	}
	b0, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	if !strings.Contains(string(b0), "第二份") {
		t.Errorf("app.log 应含轮转后内容: %s", b0)
	}
}

func TestScanLogs(t *testing.T) {
	l, dir := newLogger(t, Debug)
	today := time.Now()
	l.Scanf(today, "scan_id=%d status=%s", 1, "ok")
	l.Scanf(today, "scan_id=%d status=%s", 2, "partial")

	dates := l.ScanLogDates()
	if len(dates) != 1 || dates[0] != today.Format("2006-01-02") {
		t.Fatalf("扫描日志日期不符: %v", dates)
	}
	content, err := l.ReadScanLog(dates[0], 0)
	if err != nil {
		t.Fatalf("ReadScanLog: %v", err)
	}
	if strings.Count(content, "\n") != 1 || !strings.Contains(content, "scan_id=1") {
		t.Fatalf("扫描日志内容不符: %q", content)
	}
	// limit 取尾部
	tail, _ := l.ReadScanLog(dates[0], 1)
	if strings.Contains(tail, "scan_id=1") || !strings.Contains(tail, "scan_id=2") {
		t.Errorf("limit 应只保留最后一行: %q", tail)
	}
	// 不存在
	if _, err := l.ReadScanLog("1999-01-01", 0); err == nil {
		t.Error("不存在的日期应报错")
	}
	if p := l.ScanLogPath(today); !strings.HasSuffix(p, today.Format("2006-01-02")+".log") {
		t.Errorf("ScanLogPath 不符: %s", p)
	}

	// 保留清理
	old := time.Now().AddDate(0, 0, -40)
	oldPath := filepath.Join(dir, "scan", old.Format("2006-01-02")+".log")
	if err := os.WriteFile(oldPath, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := l.CleanupScanLogs(30); n != 1 {
		t.Fatalf("应清理 1 个过期文件，实际 %d", n)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("过期文件应被删除")
	}
	if n := l.CleanupScanLogs(30); n != 0 {
		t.Errorf("再次清理应为 0，实际 %d", n)
	}
	// 未过期的应保留
	if n := l.CleanupScanLogs(3650); n != 0 {
		t.Errorf("保留期很长时不应清理，实际 %d", n)
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{"debug": Debug, "INFO": Info, "warn": Warn, "warning": Warn, "error": Error, "trace": Error}
	for in, want := range cases {
		if got := ParseLevel(in); got != want {
			t.Errorf("ParseLevel(%q) = %v，期望 %v", in, got, want)
		}
	}
	if Info.String() != "info" || Debug.String() != "debug" || Warn.String() != "warn" || Error.String() != "error" {
		t.Error("Level.String 不符")
	}
}

func TestDirAndClose(t *testing.T) {
	l, dir := newLogger(t, Info)
	if l.Dir() != dir {
		t.Errorf("Dir = %s，期望 %s", l.Dir(), dir)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// 关闭后再写不应 panic
	l.Infof("关闭后", nil)
}
