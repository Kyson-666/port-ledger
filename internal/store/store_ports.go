package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrDuplicateReservation 表示同端口同协议已存在有效保留。
var ErrDuplicateReservation = errors.New("该端口已存在有效保留")

// ErrReservationNotFound 表示保留记录不存在。
var ErrReservationNotFound = errors.New("保留记录不存在")

// ---- ports 用户台账 ----

// PortMeta 是用户在 ports 表中维护的端口元数据。
type PortMeta struct {
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Name      string `json:"name"`
	Note      string `json:"note"`
	Tags      string `json:"tags"`
	Project   string `json:"project"`
	UpdatedBy string `json:"updated_by"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// GetPortMeta 读取端口元数据；不存在返回 (nil, nil)。
func (s *Store) GetPortMeta(port int, protocol string) (*PortMeta, error) {
	var m PortMeta
	err := s.db.QueryRow(`SELECT port, protocol, name, note, tags, project, updated_by, created_at, updated_at
		FROM ports WHERE port=? AND protocol=?`, port, protocol).
		Scan(&m.Port, &m.Protocol, &m.Name, &m.Note, &m.Tags, &m.Project, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// PutPortMeta 写入或更新端口元数据（整体覆盖）。
func (s *Store) PutPortMeta(m PortMeta) error {
	ts := now()
	_, err := s.db.Exec(`INSERT INTO ports(port, protocol, name, note, tags, project, updated_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(port, protocol) DO UPDATE SET
		  name=excluded.name, note=excluded.note, tags=excluded.tags,
		  project=excluded.project, updated_by=excluded.updated_by, updated_at=excluded.updated_at`,
		m.Port, m.Protocol, m.Name, m.Note, m.Tags, m.Project, m.UpdatedBy, ts, ts)
	return err
}

// DeletePortMeta 删除端口元数据（用于回退系统名后清理空记录）。
func (s *Store) DeletePortMeta(port int, protocol string) error {
	_, err := s.db.Exec(`DELETE FROM ports WHERE port=? AND protocol=?`, port, protocol)
	return err
}

// AllPortMeta 返回全部端口元数据，便于列表页一次性装载。
func (s *Store) AllPortMeta() ([]PortMeta, error) {
	rows, err := s.db.Query(`SELECT port, protocol, name, note, tags, project, updated_by, created_at, updated_at FROM ports`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortMeta
	for rows.Next() {
		var m PortMeta
		if err := rows.Scan(&m.Port, &m.Protocol, &m.Name, &m.Note, &m.Tags, &m.Project, &m.UpdatedBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ---- 保留 ----

// Reservation 是一条端口保留记录。
type Reservation struct {
	ID        int64  `json:"id"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Owner     string `json:"owner"`
	Project   string `json:"project"`
	Reason    string `json:"reason"`
	ExpiresAt string `json:"expires_at"`
	CreatedAt string `json:"created_at"`
	Expired   bool   `json:"expired"`
}

func parseExpiry(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ListReservations 返回全部保留记录（新在前），并标注是否过期。
func (s *Store) ListReservations() ([]Reservation, error) {
	rows, err := s.db.Query(`SELECT id, port, protocol, owner, project, reason, expires_at, created_at
		FROM reservations ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Reservation
	nowT := time.Now()
	for rows.Next() {
		var r Reservation
		if err := rows.Scan(&r.ID, &r.Port, &r.Protocol, &r.Owner, &r.Project, &r.Reason, &r.ExpiresAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		if t, ok := parseExpiry(r.ExpiresAt); ok {
			r.Expired = !t.After(nowT)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateReservation 新建保留，返回自增 ID。
// 同一 port/protocol 只允许一条有效（未过期）保留，重复创建返回 ErrDuplicateReservation。
func (s *Store) CreateReservation(r Reservation) (int64, error) {
	list, err := s.ListReservations()
	if err != nil {
		return 0, err
	}
	for _, it := range list {
		if it.Port == r.Port && it.Protocol == r.Protocol && !it.Expired {
			return 0, ErrDuplicateReservation
		}
	}
	res, err := s.db.Exec(`INSERT INTO reservations(port, protocol, owner, project, reason, expires_at, created_at)
		VALUES(?,?,?,?,?,?,?)`, r.Port, r.Protocol, r.Owner, r.Project, r.Reason, r.ExpiresAt, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateReservation 续期 / 编辑保留。目标不存在时返回 ErrReservationNotFound。
func (s *Store) UpdateReservation(id int64, owner, project, reason, expiresAt string) error {
	res, err := s.db.Exec(`UPDATE reservations SET owner=?, project=?, reason=?, expires_at=? WHERE id=?`,
		owner, project, reason, expiresAt, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReservationNotFound
	}
	return nil
}

// DeleteReservation 释放保留。目标不存在时返回 ErrReservationNotFound。
func (s *Store) DeleteReservation(id int64) error {
	res, err := s.db.Exec(`DELETE FROM reservations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrReservationNotFound
	}
	return nil
}

// ActiveReservations 返回未过期的保留，键为 "port/protocol"。
func (s *Store) ActiveReservations() (map[string]Reservation, error) {
	list, err := s.ListReservations()
	if err != nil {
		return nil, err
	}
	m := map[string]Reservation{}
	for _, r := range list {
		if r.Expired {
			continue
		}
		m[fmt.Sprintf("%d/%s", r.Port, r.Protocol)] = r
	}
	return m, nil
}

// ---- 观测与扫描运行 ----

// Observation 是一次扫描得到的单端口观测。
type Observation struct {
	Port          int    `json:"port"`
	Protocol      string `json:"protocol"`
	State         string `json:"state"`
	Source        string `json:"source"`
	BindAddr      string `json:"bind_addr,omitempty"`
	PID           int    `json:"pid,omitempty"`
	ProcessName   string `json:"process_name,omitempty"`
	Cmdline       string `json:"cmdline,omitempty"`
	ContainerID   string `json:"container_id,omitempty"`
	ContainerName string `json:"container_name,omitempty"`
	Image         string `json:"image,omitempty"`
	ComposeFile   string `json:"compose_file,omitempty"`
	ObservedAt    string `json:"observed_at,omitempty"`
	// Conflict 为交互稿 §3.3 描述的「端口冲突」叠加态：
	// 该端口命中群晖系统字典，同时又被容器/进程/compose 实际占用。
	Conflict bool `json:"conflict,omitempty"`
}

// ReplaceObservations 以一次扫描的观测整体替换（先删同 scan_id 再插入）。
func (s *Store) ReplaceObservations(scanID int64, obs []Observation) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM port_observations WHERE scan_id=?`, scanID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO port_observations
		(scan_id, port, protocol, state, source, bind_addr, pid, process_name, cmdline,
		 container_id, container_name, image, compose_file, observed_at, conflict)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	ts := now()
	for _, o := range obs {
		if _, err := stmt.Exec(scanID, o.Port, o.Protocol, o.State, o.Source, o.BindAddr, o.PID,
			o.ProcessName, o.Cmdline, o.ContainerID, o.ContainerName, o.Image, o.ComposeFile, ts, boolToInt(o.Conflict)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ObservationsByScan 读取某次扫描的全部观测。
func (s *Store) ObservationsByScan(scanID int64) ([]Observation, error) {
	rows, err := s.db.Query(`SELECT port, protocol, state, source, bind_addr, pid, process_name, cmdline,
		container_id, container_name, image, compose_file, observed_at, conflict
		FROM port_observations WHERE scan_id=?`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Observation
	for rows.Next() {
		var o Observation
		var conflict int
		if err := rows.Scan(&o.Port, &o.Protocol, &o.State, &o.Source, &o.BindAddr, &o.PID, &o.ProcessName,
			&o.Cmdline, &o.ContainerID, &o.ContainerName, &o.Image, &o.ComposeFile, &o.ObservedAt, &conflict); err != nil {
			return nil, err
		}
		o.Conflict = conflict != 0
		out = append(out, o)
	}
	return out, rows.Err()
}

// ScanRun 是一次扫描的执行记录。
type ScanRun struct {
	ID           int64  `json:"id"`
	StartedAt    string `json:"started_at"`
	FinishedAt   string `json:"finished_at"`
	Scope        string `json:"scope"`
	Status       string `json:"status"`
	Counts       string `json:"counts"`
	SourceHealth string `json:"source_health"`
}

// BeginScan 创建一条 running 状态的扫描记录。
func (s *Store) BeginScan(scope string) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO scan_runs(started_at, scope, status) VALUES(?,?, 'running')`, now(), scope)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// FinishScan 结束扫描并记录结果。
func (s *Store) FinishScan(id int64, status, counts, health string) error {
	_, err := s.db.Exec(`UPDATE scan_runs SET finished_at=?, status=?, counts=?, source_health=? WHERE id=?`,
		now(), status, counts, health, id)
	return err
}

// LatestScan 返回最近一次已结束的扫描；不存在返回 (nil, nil)。
func (s *Store) LatestScan() (*ScanRun, error) {
	var r ScanRun
	err := s.db.QueryRow(`SELECT id, started_at, finished_at, scope, status, counts, source_health
		FROM scan_runs WHERE status!='running' ORDER BY id DESC LIMIT 1`).
		Scan(&r.ID, &r.StartedAt, &r.FinishedAt, &r.Scope, &r.Status, &r.Counts, &r.SourceHealth)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CountScans 返回扫描次数，用于判断是否首次启动。
func (s *Store) CountScans() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM scan_runs`).Scan(&n)
	return n, err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
