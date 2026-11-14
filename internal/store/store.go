// Package store 封装 SQLite 持久化（modernc.org/sqlite，纯 Go 无 CGO）。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kyson-666/port-ledger/internal/dict"
)

// Store 是持久化门面。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并建表。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	dsn := filepath.ToSlash(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	// SQLite 写串行化；本工具并发量极低，单连接最简单可靠。
	db.SetMaxOpenConns(1)
	for _, p := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("设置 %s 失败: %w", p, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表失败: %w", err)
	}
	return &Store{db: db}, nil
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

func now() string { return time.Now().Format(time.RFC3339) }

// SeedDict 幂等地把内置字典写入 system_port_dict。
func (s *Store) SeedDict(entries []dict.Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO system_port_dict
		(port, port_end, protocol, category, display_name, description, source_ref, builtin)
		VALUES (?,?,?,?,?,?,?,1)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.Exec(e.Port, e.PortEnd, e.Protocol, e.Category, e.DisplayName, e.Description, e.SourceRef); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BuiltinDict 读取内置字典条目。
func (s *Store) BuiltinDict() ([]dict.Entry, error) {
	rows, err := s.db.Query(`SELECT port, port_end, protocol, category, display_name, description, source_ref
		FROM system_port_dict WHERE builtin=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dict.Entry
	for rows.Next() {
		var e dict.Entry
		if err := rows.Scan(&e.Port, &e.PortEnd, &e.Protocol, &e.Category, &e.DisplayName, &e.Description, &e.SourceRef); err != nil {
			return nil, err
		}
		e.Type = "builtin"
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- settings ----

// GetSetting 读取一项设置。
func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// PutSetting 写入一项设置。
func (s *Store) PutSetting(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// AllSettings 返回全部设置。
func (s *Store) AllSettings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}

// ---- audit ----

// Audit 是一条审计记录。
type Audit struct {
	ID     int64  `json:"id"`
	TS     string `json:"ts"`
	Actor  string `json:"actor"`
	Action string `json:"action"`
	Target string `json:"target"`
	Detail string `json:"detail"`
}

// AddAudit 写入审计记录。
func (s *Store) AddAudit(a Audit) error {
	if a.Actor == "" {
		a.Actor = "本地"
	}
	a.TS = now()
	_, err := s.db.Exec(`INSERT INTO audit_log(ts,actor,action,target,detail) VALUES(?,?,?,?,?)`,
		a.TS, a.Actor, a.Action, a.Target, a.Detail)
	return err
}

// ListAudit 按动作与端口过滤返回审计记录（时间倒序）。
func (s *Store) ListAudit(action, target string, limit int) ([]Audit, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT id, ts, actor, action, target, detail FROM audit_log WHERE 1=1`)
	var args []any
	if action != "" {
		q.WriteString(` AND action=?`)
		args = append(args, action)
	}
	if target != "" {
		q.WriteString(` AND target LIKE ?`)
		args = append(args, "%"+target+"%")
	}
	q.WriteString(` ORDER BY id DESC`)
	if limit > 0 {
		q.WriteString(` LIMIT ?`)
		args = append(args, limit)
	}
	rows, err := s.db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Audit
	for rows.Next() {
		var a Audit
		if err := rows.Scan(&a.ID, &a.TS, &a.Actor, &a.Action, &a.Target, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
