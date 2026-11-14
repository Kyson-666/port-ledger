// Package logx 提供结构化 JSON 行日志、按大小轮转、扫描明细按天切分与保留策略。
// 对应设计稿 §8.2 的日志策略：app.log 10MB × 5 轮转；scan/YYYY-MM-DD.log 保留 30 天。
package logx

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Level int

const (
	Debug Level = iota
	Info
	Warn
	Error
)

func (l Level) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warn:
		return "warn"
	default:
		return "error"
	}
}

// ParseLevel 解析日志级别名。
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return Debug
	case "info":
		return Info
	case "warn", "warning":
		return Warn
	default:
		return Error
	}
}

const (
	maxFileSize  = 10 << 20 // 10MB
	maxRotations = 4        // 保留 app.log.1..4，加当前共 5 份
	ringSize     = 2000     // 内存环形缓冲，供 /api/logs 使用
)

// Entry 是一条日志记录。
type Entry struct {
	Time   string         `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Fields map[string]any `json:"fields,omitempty"`
}

// Logger 是并发安全的文件日志器。
type Logger struct {
	mu    sync.Mutex
	dir   string
	level Level

	file *os.File
	size int64

	ring  []Entry
	start int
	count int

	console bool
}

// New 创建日志器；dir 为日志根目录（通常是 <DataDir>/logs）。
func New(dir string, level Level, console bool) (*Logger, error) {
	if err := os.MkdirAll(filepath.Join(dir, "scan"), 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	l := &Logger{dir: dir, level: level, ring: make([]Entry, ringSize), console: console}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) open() error {
	f, err := os.OpenFile(filepath.Join(l.dir, "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("打开 app.log 失败: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.file = f
	l.size = st.Size()
	return nil
}

func (l *Logger) rotate() {
	if l.file == nil {
		return
	}
	l.file.Close()
	l.file = nil
	// app.log.3 -> app.log.4 ... app.log -> app.log.1
	for i := maxRotations - 1; i >= 1; i-- {
		old := filepath.Join(l.dir, fmt.Sprintf("app.log.%d", i))
		newer := filepath.Join(l.dir, fmt.Sprintf("app.log.%d", i+1))
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, newer)
		}
	}
	_ = os.Rename(filepath.Join(l.dir, "app.log"), filepath.Join(l.dir, "app.log.1"))
	_ = l.open()
}

// Dir 返回日志根目录。
func (l *Logger) Dir() string { return l.dir }

// AppLogPath 返回 app.log 的绝对路径，供下载。
func (l *Logger) AppLogPath() string { return filepath.Join(l.dir, "app.log") }

func (l *Logger) log(lv Level, msg string, fields map[string]any) {
	if lv < l.level {
		return
	}
	e := Entry{
		Time:   time.Now().Format(time.RFC3339),
		Level:  lv.String(),
		Msg:    msg,
		Fields: fields,
	}
	line, _ := json.Marshal(e)

	l.mu.Lock()
	defer l.mu.Unlock()

	// 环形缓冲
	l.ring[(l.start+l.count)%ringSize] = e
	if l.count < ringSize {
		l.count++
	} else {
		l.start = (l.start + 1) % ringSize
	}

	if l.console {
		fmt.Fprintln(os.Stdout, string(line))
	}
	if l.file == nil {
		return
	}
	n, _ := l.file.Write(append(line, '\n'))
	l.size += int64(n)
	if l.size >= maxFileSize {
		l.rotate()
	}
}

func (l *Logger) Debugf(msg string, fields map[string]any) { l.log(Debug, msg, fields) }
func (l *Logger) Infof(msg string, fields map[string]any)  { l.log(Info, msg, fields) }
func (l *Logger) Warnf(msg string, fields map[string]any)  { l.log(Warn, msg, fields) }
func (l *Logger) Errorf(msg string, fields map[string]any) { l.log(Error, msg, fields) }

// Tail 返回最近日志（按级别与关键字过滤），level 为最低级别。
func (l *Logger) Tail(level Level, keyword string, limit int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0)
	for i := 0; i < l.count; i++ {
		e := l.ring[(l.start+i)%ringSize]
		if ParseLevel(e.Level) < level {
			continue
		}
		if keyword != "" && !strings.Contains(e.Msg, keyword) {
			continue
		}
		out = append(out, e)
	}
	// 倒序（最新在前）
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Scanf 追加一条扫描明细到 <LogDir>/scan/<date>.log。
func (l *Logger) Scanf(date time.Time, format string, args ...any) {
	dir := filepath.Join(l.dir, "scan")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, date.Format("2006-01-02")+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// ScanLogPath 返回指定日期的扫描日志路径。
func (l *Logger) ScanLogPath(date time.Time) string {
	return filepath.Join(l.dir, "scan", date.Format("2006-01-02")+".log")
}

// ScanLogDates 列出已有扫描日志的日期（倒序）。
func (l *Logger) ScanLogDates() []string {
	ents, err := os.ReadDir(filepath.Join(l.dir, "scan"))
	if err != nil {
		return []string{}
	}
	out := make([]string, 0)
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			out = append(out, strings.TrimSuffix(e.Name(), ".log"))
		}
	}
	return out
}

// ReadScanLog 读取指定日期扫描日志的尾部内容。
func (l *Logger) ReadScanLog(date string, limit int) (string, error) {
	f, err := os.Open(filepath.Join(l.dir, "scan", date+".log"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return strings.Join(lines, "\n"), sc.Err()
}

// CleanupScanLogs 删除超过 retainDays 天的扫描明细。
func (l *Logger) CleanupScanLogs(retainDays int) int {
	dir := filepath.Join(l.dir, "scan")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	removed := 0
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		d, err := time.Parse("2006-01-02", strings.TrimSuffix(e.Name(), ".log"))
		if err != nil {
			continue
		}
		if d.Before(cutoff) {
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				removed++
			}
		}
	}
	return removed
}

// Close 关闭文件句柄。
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

var _ io.Closer = (*Logger)(nil)
