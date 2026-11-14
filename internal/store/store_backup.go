package store

import "fmt"

// Ledger 是台账导出结构（不含扫描快照，便于跨设备复用）。
type Ledger struct {
	Version       string            `json:"version"`
	ExportedAt    string            `json:"exported_at"`
	Ports         []PortMeta        `json:"ports"`
	Reservations  []Reservation     `json:"reservations"`
	DictUser      []DictUserEntry   `json:"dict_user"`
	DictOverrides []DictOverride    `json:"dict_overrides"`
	Settings      map[string]string `json:"settings"`
}

// ExportLedger 导出完整台账。
func (s *Store) ExportLedger(version string) (*Ledger, error) {
	ports, err := s.AllPortMeta()
	if err != nil {
		return nil, err
	}
	res, err := s.ListReservations()
	if err != nil {
		return nil, err
	}
	du, err := s.ListDictUser()
	if err != nil {
		return nil, err
	}
	dov, err := s.ListDictOverrides()
	if err != nil {
		return nil, err
	}
	st, err := s.AllSettings()
	if err != nil {
		return nil, err
	}
	return &Ledger{
		Version: version, ExportedAt: now(),
		Ports: ports, Reservations: res, DictUser: du, DictOverrides: dov, Settings: st,
	}, nil
}

// ImportLedger 导入台账。mode = replace 时先清空既有台账类数据。
func (s *Store) ImportLedger(l *Ledger, mode string) (map[string]int, error) {
	counts := map[string]int{}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if mode == "replace" {
		for _, q := range []string{`DELETE FROM ports`, `DELETE FROM reservations`, `DELETE FROM dict_user`, `DELETE FROM dict_override`} {
			if _, err := tx.Exec(q); err != nil {
				return nil, err
			}
		}
	}

	ts := now()
	for _, p := range l.Ports {
		if _, err := tx.Exec(`INSERT INTO ports(port, protocol, name, note, tags, project, updated_by, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(port, protocol) DO UPDATE SET
			  name=excluded.name, note=excluded.note, tags=excluded.tags,
			  project=excluded.project, updated_at=excluded.updated_at`,
			p.Port, p.Protocol, p.Name, p.Note, p.Tags, p.Project, p.UpdatedBy, ts, ts); err != nil {
			return nil, fmt.Errorf("导入端口 %d/%s 失败: %w", p.Port, p.Protocol, err)
		}
		counts["ports"]++
	}

	for _, r := range l.Reservations {
		if _, err := tx.Exec(`INSERT INTO reservations(port, protocol, owner, project, reason, expires_at, created_at)
			VALUES(?,?,?,?,?,?,?)`, r.Port, r.Protocol, r.Owner, r.Project, r.Reason, r.ExpiresAt, orNow(r.CreatedAt)); err != nil {
			return nil, fmt.Errorf("导入保留 %d/%s 失败: %w", r.Port, r.Protocol, err)
		}
		counts["reservations"]++
	}

	for _, d := range l.DictUser {
		if _, err := tx.Exec(`INSERT INTO dict_user(port, port_end, protocol, category, display_name, description, created_at)
			VALUES(?,?,?,?,?,?,?)`, d.Port, d.PortEnd, d.Protocol, d.Category, d.DisplayName, d.Description, ts); err != nil {
			return nil, fmt.Errorf("导入自定义字典 %d 失败: %w", d.Port, err)
		}
		counts["dict_user"]++
	}

	for _, o := range l.DictOverrides {
		if _, err := tx.Exec(`INSERT INTO dict_override(port, protocol, display_name, description, updated_at)
			VALUES(?,?,?,?,?)
			ON CONFLICT(port, protocol) DO UPDATE SET
			  display_name=excluded.display_name, description=excluded.description, updated_at=excluded.updated_at`,
			o.Port, o.Protocol, o.DisplayName, o.Description, ts); err != nil {
			return nil, fmt.Errorf("导入字典覆盖 %d/%s 失败: %w", o.Port, o.Protocol, err)
		}
		counts["dict_overrides"]++
	}

	for k, v := range l.Settings {
		if _, err := tx.Exec(`INSERT INTO settings(key,value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			return nil, fmt.Errorf("导入设置 %s 失败: %w", k, err)
		}
		counts["settings"]++
	}

	return counts, tx.Commit()
}

func orNow(s string) string {
	if s == "" {
		return now()
	}
	return s
}
