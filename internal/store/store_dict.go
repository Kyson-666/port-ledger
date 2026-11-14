package store

import (
	"database/sql"
	"fmt"

	"github.com/kyson-666/port-ledger/internal/dict"
)

// DictUserEntry 是用户自定义的字典条目。
type DictUserEntry struct {
	ID          int64  `json:"id"`
	Port        int    `json:"port"`
	PortEnd     int    `json:"port_end"`
	Protocol    string `json:"protocol"`
	Category    string `json:"category"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

// DictOverride 是用户对内置条目名称的覆盖。
type DictOverride struct {
	Port        int    `json:"port"`
	Protocol    string `json:"protocol"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
}

// ListDictUser 返回全部自定义字典条目。
func (s *Store) ListDictUser() ([]DictUserEntry, error) {
	rows, err := s.db.Query(`SELECT id, port, port_end, protocol, category, display_name, description, created_at
		FROM dict_user ORDER BY port`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DictUserEntry
	for rows.Next() {
		var d DictUserEntry
		if err := rows.Scan(&d.ID, &d.Port, &d.PortEnd, &d.Protocol, &d.Category, &d.DisplayName, &d.Description, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AddDictUser 新增自定义字典条目。
func (s *Store) AddDictUser(d DictUserEntry) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO dict_user(port, port_end, protocol, category, display_name, description, created_at)
		VALUES(?,?,?,?,?,?,?)`, d.Port, d.PortEnd, d.Protocol, d.Category, d.DisplayName, d.Description, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteDictUser 删除自定义条目。
func (s *Store) DeleteDictUser(id int64) error {
	_, err := s.db.Exec(`DELETE FROM dict_user WHERE id=?`, id)
	return err
}

// UpdateDictUser 编辑自定义条目。
func (s *Store) UpdateDictUser(id int64, port, portEnd int, protocol, category, displayName, description string) error {
	_, err := s.db.Exec(`UPDATE dict_user SET port=?, port_end=?, protocol=?, category=?, display_name=?, description=? WHERE id=?`,
		port, portEnd, protocol, category, displayName, description, id)
	return err
}

// ListDictOverrides 返回全部覆盖记录。
func (s *Store) ListDictOverrides() ([]DictOverride, error) {
	rows, err := s.db.Query(`SELECT port, protocol, display_name, description, updated_at FROM dict_override`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DictOverride
	for rows.Next() {
		var o DictOverride
		if err := rows.Scan(&o.Port, &o.Protocol, &o.DisplayName, &o.Description, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PutDictOverride 写入或更新覆盖。
func (s *Store) PutDictOverride(port int, protocol, displayName, description string) error {
	_, err := s.db.Exec(`INSERT INTO dict_override(port, protocol, display_name, description, updated_at)
		VALUES(?,?,?,?,?)
		ON CONFLICT(port, protocol) DO UPDATE SET
		  display_name=excluded.display_name, description=excluded.description, updated_at=excluded.updated_at`,
		port, protocol, displayName, description, now())
	return err
}

// DeleteDictOverride 恢复内置（删除覆盖）。
func (s *Store) DeleteDictOverride(port int, protocol string) error {
	_, err := s.db.Exec(`DELETE FROM dict_override WHERE port=? AND protocol=?`, port, protocol)
	return err
}

// EffectiveDict 计算生效字典：内置条目套用覆盖后 + 自定义条目。
// 覆盖按 (port, protocol) 精确匹配；协议 both 的条目对 tcp/udp 双写覆盖。
func (s *Store) EffectiveDict() ([]dict.Entry, error) {
	builtin, err := s.BuiltinDict()
	if err != nil {
		return nil, err
	}
	overrides, err := s.ListDictOverrides()
	if err != nil {
		return nil, err
	}
	ovMap := map[string]DictOverride{}
	for _, o := range overrides {
		ovMap[fmt.Sprintf("%d/%s", o.Port, o.Protocol)] = o
	}

	out := make([]dict.Entry, 0, len(builtin))
	for _, e := range builtin {
		e.Type = "builtin"
		// 单端口条目才支持覆盖（覆盖语义按精确端口）
		if e.Port == e.End() {
			for _, proto := range protosOf(e.Protocol) {
				if o, ok := ovMap[fmt.Sprintf("%d/%s", e.Port, proto)]; ok {
					e.DisplayName = o.DisplayName
					if o.Description != "" {
						e.Description = o.Description
					}
					e.Type = "overridden"
					break
				}
			}
		}
		out = append(out, e)
	}

	user, err := s.ListDictUser()
	if err != nil {
		return nil, err
	}
	for _, u := range user {
		out = append(out, dict.Entry{
			Port:        u.Port,
			PortEnd:     u.PortEnd,
			Protocol:    u.Protocol,
			Category:    u.Category,
			DisplayName: u.DisplayName,
			Description: u.Description,
			SourceRef:   "用户自定义",
			Type:        "custom",
		})
	}
	dict.Sort(out)
	return out, nil
}

func protosOf(p string) []string {
	if p == "both" {
		return []string{"tcp", "udp"}
	}
	return []string{p}
}

// HasOverride 判断某端口协议是否存在覆盖。
func (s *Store) HasOverride(port int, protocol string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM dict_override WHERE port=? AND protocol=?`, port, protocol).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	return n > 0, nil
}
