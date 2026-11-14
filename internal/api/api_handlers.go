package api

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kyson-666/port-ledger/internal/app"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/store"
)

// logxParseLevel 解析日志级别，缺省为 info。
func logxParseLevel(s string) logx.Level {
	if strings.TrimSpace(s) == "" {
		return logx.Info
	}
	return logx.ParseLevel(s)
}

// ---------- 保留管理 ----------

func (s *Server) handleReservationsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.App.Store.ListReservations()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	state := q.Get("state")
	project := q.Get("project")
	owner := q.Get("owner")
	kw := strings.ToLower(q.Get("q"))

	nowT := time.Now()
	out := make([]store.Reservation, 0, len(list))
	for _, it := range list {
		switch state {
		case "active":
			if it.Expired {
				continue
			}
		case "expired":
			if !it.Expired {
				continue
			}
		case "expiring":
			if it.Expired {
				continue
			}
			if t, ok := parseTime(it.ExpiresAt); !ok || t.Sub(nowT) > 72*time.Hour {
				continue
			}
		}
		if project != "" && it.Project != project {
			continue
		}
		if owner != "" && it.Owner != owner {
			continue
		}
		if kw != "" && !strings.Contains(strings.ToLower(it.Owner+it.Project+it.Reason+strconv.Itoa(it.Port)), kw) {
			continue
		}
		out = append(out, it)
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(out), "items": out})
}

type reservationReq struct {
	ID        int64  `json:"id"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol"`
	Owner     string `json:"owner"`
	Project   string `json:"project"`
	Reason    string `json:"reason"`
	ExpiresAt string `json:"expires_at"`
	Actor     string `json:"actor"`
}

func (s *Server) handleReservationsCreate(w http.ResponseWriter, r *http.Request) {
	var req reservationReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		writeErr(w, http.StatusBadRequest, "端口号需在 1-65535")
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "tcp"
	}
	if proto != "tcp" && proto != "udp" {
		writeErr(w, http.StatusBadRequest, "协议仅支持 tcp/udp")
		return
	}
	if req.ExpiresAt != "" {
		if _, ok := parseTime(req.ExpiresAt); !ok {
			writeErr(w, http.StatusBadRequest, "expires_at 需为 YYYY-MM-DD 或 RFC3339")
			return
		}
	}

	// 仅允许保留空闲端口（交互稿 §2 P5：非空闲则拒绝）
	item, err := s.App.GetPort(req.Port, proto)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item != nil && item.State != "free" && item.State != "reserved" {
		writeErr(w, http.StatusConflict, fmt.Sprintf("端口 %d/%s 已被占用（%s），无法保留", req.Port, strings.ToUpper(proto), item.StateLabel))
		return
	}

	id, err := s.App.Store.CreateReservation(store.Reservation{
		Port: req.Port, Protocol: proto, Owner: req.Owner, Project: req.Project,
		Reason: req.Reason, ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{
		Actor: req.Actor, Action: "reserve", Target: fmt.Sprintf("端口 %d/%s", req.Port, strings.ToUpper(proto)),
		Detail: fmt.Sprintf("保留给 %s（项目 %s，原因 %s）", orDash(req.Owner), orDash(req.Project), orDash(req.Reason)),
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleReservationsUpdate(w http.ResponseWriter, r *http.Request) {
	var req reservationReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if req.ID <= 0 {
		writeErr(w, http.StatusBadRequest, "缺少 id")
		return
	}
	if req.ExpiresAt != "" {
		if _, ok := parseTime(req.ExpiresAt); !ok {
			writeErr(w, http.StatusBadRequest, "expires_at 需为 YYYY-MM-DD 或 RFC3339")
			return
		}
	}
	if err := s.App.Store.UpdateReservation(req.ID, req.Owner, req.Project, req.Reason, req.ExpiresAt); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{
		Actor: req.Actor, Action: "reserve_update", Target: fmt.Sprintf("保留 #%d", req.ID),
		Detail: fmt.Sprintf("续期/编辑，过期时间 %s", orDash(req.ExpiresAt)),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReservationsDelete(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		var body struct {
			ID int64 `json:"id"`
		}
		if err := readJSON(r, &body); err == nil && body.ID > 0 {
			idStr = strconv.FormatInt(body.ID, 10)
		}
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "缺少合法的 id（查询参数 id 或请求体 id）")
		return
	}
	if err := s.App.Store.DeleteReservation(id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{
		Action: "unreserve", Target: fmt.Sprintf("保留 #%d", id), Detail: "释放保留",
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func parseTime(v string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ---------- 系统端口字典 ----------

func (s *Server) handleDictGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	entries, err := s.App.Store.EffectiveDict()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	builtin, _ := s.App.Store.BuiltinDict()
	custom, _ := s.App.Store.ListDictUser()
	overrides, _ := s.App.Store.ListDictOverrides()

	typ := q.Get("type")
	category := q.Get("category")
	kw := strings.ToLower(strings.TrimSpace(q.Get("q")))

	cats := map[string]int{}
	out := make([]dict.Entry, 0, len(entries))
	for _, e := range entries {
		cats[e.Category]++
		if typ != "" && e.Type != typ {
			continue
		}
		if category != "" && e.Category != category {
			continue
		}
		if kw != "" {
			hay := strings.ToLower(e.DisplayName + e.Category + strconv.Itoa(e.Port) + e.Description)
			if !strings.Contains(hay, kw) {
				continue
			}
		}
		out = append(out, e)
	}
	type catStat struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	var catList []catStat
	for _, name := range s.App.Dict().Categories() {
		catList = append(catList, catStat{Name: name, Count: cats[name]})
	}
	// 追加用户自定义类别
	known := map[string]bool{}
	for _, c := range catList {
		known[c.Name] = true
	}
	var extra []string
	for name := range cats {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		catList = append(catList, catStat{Name: name, Count: cats[name]})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":          len(entries),
		"builtin_count":  len(builtin),
		"custom_count":   len(custom),
		"override_count": len(overrides),
		"categories":     catList,
		"entries":        out,
	})
}

type dictPostReq struct {
	Action      string `json:"action"` // create | update | override | restore | delete
	ID          int64  `json:"id"`
	Port        int    `json:"port"`
	PortEnd     int    `json:"port_end"`
	Protocol    string `json:"protocol"`
	Category    string `json:"category"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Actor       string `json:"actor"`
}

func (s *Server) handleDictPost(w http.ResponseWriter, r *http.Request) {
	var req dictPostReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	action := strings.ToLower(strings.TrimSpace(req.Action))
	if action == "" {
		writeErr(w, http.StatusBadRequest, "缺少 action")
		return
	}
	proto := strings.ToLower(strings.TrimSpace(req.Protocol))
	if proto == "" {
		proto = "tcp"
	}
	if proto != "tcp" && proto != "udp" && proto != "both" {
		writeErr(w, http.StatusBadRequest, "协议仅支持 tcp/udp/both")
		return
	}

	switch action {
	case "create":
		if req.Port < 1 || req.Port > 65535 {
			writeErr(w, http.StatusBadRequest, "端口需在 1-65535")
			return
		}
		if req.PortEnd != 0 && (req.PortEnd < req.Port || req.PortEnd > 65535) {
			writeErr(w, http.StatusBadRequest, "结束端口非法")
			return
		}
		if strings.TrimSpace(req.DisplayName) == "" {
			writeErr(w, http.StatusBadRequest, "名称不能为空")
			return
		}
		if strings.TrimSpace(req.Category) == "" {
			req.Category = "自定义"
		}
		id, err := s.App.Store.AddDictUser(store.DictUserEntry{
			Port: req.Port, PortEnd: req.PortEnd, Protocol: proto, Category: req.Category,
			DisplayName: req.DisplayName, Description: req.Description,
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{Actor: req.Actor, Action: "dict_create",
			Target: fmt.Sprintf("字典 %d/%s", req.Port, strings.ToUpper(proto)), Detail: "新增自定义条目：" + req.DisplayName})
		if err := s.App.ReloadDict(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})

	case "update":
		if req.ID <= 0 {
			writeErr(w, http.StatusBadRequest, "缺少 id")
			return
		}
		if err := s.App.Store.UpdateDictUser(req.ID, req.Port, req.PortEnd, proto, req.Category, req.DisplayName, req.Description); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{Actor: req.Actor, Action: "dict_update",
			Target: fmt.Sprintf("字典 #%d", req.ID), Detail: "编辑自定义条目"})
		if err := s.App.ReloadDict(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "override":
		if strings.TrimSpace(req.DisplayName) == "" {
			writeErr(w, http.StatusBadRequest, "名称不能为空")
			return
		}
		if err := s.App.Store.PutDictOverride(req.Port, proto, req.DisplayName, req.Description); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{Actor: req.Actor, Action: "dict_override",
			Target: fmt.Sprintf("字典 %d/%s", req.Port, strings.ToUpper(proto)), Detail: "覆盖内置名称：" + req.DisplayName})
		if err := s.App.ReloadDict(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "restore":
		if err := s.App.Store.DeleteDictOverride(req.Port, proto); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{Actor: req.Actor, Action: "dict_restore",
			Target: fmt.Sprintf("字典 %d/%s", req.Port, strings.ToUpper(proto)), Detail: "恢复内置名称"})
		if err := s.App.ReloadDict(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	case "delete":
		if req.ID <= 0 {
			writeErr(w, http.StatusBadRequest, "缺少 id")
			return
		}
		if err := s.App.Store.DeleteDictUser(req.ID); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{Actor: req.Actor, Action: "dict_delete",
			Target: fmt.Sprintf("字典 #%d", req.ID), Detail: "删除自定义条目"})
		if err := s.App.ReloadDict(); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})

	default:
		writeErr(w, http.StatusBadRequest, "未知 action："+action)
	}
}

func (s *Server) handleDictExport(w http.ResponseWriter, r *http.Request) {
	builtin, err := s.App.Store.BuiltinDict()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, _ := s.App.Store.ListDictUser()
	overrides, _ := s.App.Store.ListDictOverrides()
	payload := map[string]any{
		"version":     s.App.Cfg.Version,
		"exported_at": time.Now().Format(time.RFC3339),
		"builtin":     builtin,
		"user":        user,
		"overrides":   overrides,
	}
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=port-ledger-dict-%s.json", time.Now().Format("2006-01-02")))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

type dictImportReq struct {
	User      []store.DictUserEntry `json:"user"`
	Overrides []store.DictOverride  `json:"overrides"`
}

func (s *Server) handleDictImport(w http.ResponseWriter, r *http.Request) {
	var req dictImportReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON 格式不正确: "+err.Error())
		return
	}
	preview := r.URL.Query().Get("preview") == "1"

	existing, _ := s.App.Store.ListDictUser()
	existingKeys := map[string]bool{}
	for _, e := range existing {
		existingKeys[fmt.Sprintf("%d-%d/%s/%s", e.Port, e.PortEnd, e.Protocol, e.Category)] = true
	}
	newCount, dupCount := 0, 0
	for _, e := range req.User {
		k := fmt.Sprintf("%d-%d/%s/%s", e.Port, e.PortEnd, e.Protocol, e.Category)
		if existingKeys[k] {
			dupCount++
		} else {
			newCount++
		}
	}
	if len(req.User) == 0 && len(req.Overrides) == 0 {
		writeErr(w, http.StatusBadRequest, "文件中没有可导入的字典条目")
		return
	}
	if preview {
		writeJSON(w, http.StatusOK, map[string]any{
			"preview": true, "new": newCount, "duplicate": dupCount, "overrides": len(req.Overrides),
		})
		return
	}

	ledger := &store.Ledger{DictUser: req.User, DictOverrides: req.Overrides}
	counts, err := s.App.Store.ImportLedger(ledger, "merge")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{Action: "dict_import",
		Target: "系统字典", Detail: fmt.Sprintf("导入 %d 条自定义、%d 条覆盖", counts["dict_user"], counts["dict_overrides"])})
	if err := s.App.ReloadDict(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": counts})
}

// ---------- 审计与日志 ----------

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 200
	}
	list, err := s.App.Store.ListAudit(q.Get("action"), q.Get("target"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": len(list), "items": list})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	date := q.Get("date")
	if date != "" {
		content, err := s.App.Log.ReadScanLog(date, 2000)
		if err != nil {
			writeErr(w, http.StatusNotFound, "该日期没有扫描日志: "+date)
			return
		}
		if q.Get("download") == "1" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Content-Disposition", "attachment; filename=scan-"+date+".log")
			_, _ = io.WriteString(w, content)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"date": date, "content": content})
		return
	}
	if q.Get("download") == "1" {
		http.ServeFile(w, r, s.App.Log.AppLogPath())
		return
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 300
	}
	entries := s.App.Log.Tail(logxParseLevel(q.Get("level")), q.Get("q"), limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": entries, "dates": s.App.Log.ScanLogDates(),
		"app_log": "app.log",
	})
}

// ---------- 备份 ----------

func (s *Server) handleBackupExport(w http.ResponseWriter, r *http.Request) {
	ledger, err := s.App.Store.ExportLedger(s.App.Cfg.Version)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	name := fmt.Sprintf("port-ledger-%s.json", time.Now().Format("2006-01-02"))
	// 同时落盘到 /data/backups，便于群晖 Hyper Backup 覆盖
	if s.App.Cfg.BackupDir != "" {
		if err := os.MkdirAll(s.App.Cfg.BackupDir, 0o755); err == nil {
			if b, err := json.MarshalIndent(ledger, "", "  "); err == nil {
				_ = os.WriteFile(filepath.Join(s.App.Cfg.BackupDir, name), b, 0o644)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(ledger)
}

type backupImportReq struct {
	Mode string `json:"mode"` // merge | replace
	store.Ledger
}

func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	var req backupImportReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "JSON 格式不正确: "+err.Error())
		return
	}
	mode := req.Mode
	if mode != "replace" {
		mode = "merge"
	}
	if len(req.Ports) == 0 && len(req.Reservations) == 0 && len(req.DictUser) == 0 && len(req.DictOverrides) == 0 {
		writeErr(w, http.StatusBadRequest, "文件中没有可导入的台账数据")
		return
	}
	if r.URL.Query().Get("preview") == "1" {
		writeJSON(w, http.StatusOK, map[string]any{
			"preview": true, "ports": len(req.Ports), "reservations": len(req.Reservations),
			"dict_user": len(req.DictUser), "dict_overrides": len(req.DictOverrides),
			"settings": len(req.Settings), "mode": mode,
		})
		return
	}
	counts, err := s.App.Store.ImportLedger(&req.Ledger, mode)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{Action: "backup_import", Target: "台账",
		Detail: fmt.Sprintf("导入 %d 命名、%d 保留（%s）", counts["ports"], counts["reservations"], mode)})
	if err := s.App.ApplySettings(); err != nil {
		s.App.Log.Warnf("导入后应用设置失败", map[string]any{"error": err.Error()})
	}
	if err := s.App.ReloadDict(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"imported": counts})
}

// ---------- 设置 ----------

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	eff, err := s.App.EffectiveSettings()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"values":            eff,
		"env_defaults":      s.App.SettingDefaults(),
		"restart_required":  app.RestartRequiredKeys,
	})
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return
	}
	if err := app.ValidateSettings(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var changed []string
	for k, v := range req {
		if k == "actor" {
			continue
		}
		if err := s.App.Store.PutSetting(k, fmt.Sprint(v)); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		changed = append(changed, k)
	}
	sort.Strings(changed)
	if err := s.App.ApplySettings(); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.App.Store.AddAudit(store.Audit{Action: "setting_update", Target: "设置",
		Detail: "更新：" + strings.Join(changed, ", ")})

	var restart []string
	for _, k := range changed {
		for _, rk := range app.RestartRequiredKeys {
			if k == rk {
				restart = append(restart, k)
			}
		}
	}
	eff, _ := s.App.EffectiveSettings()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "changed": changed, "restart_required": restart, "values": eff,
	})
}

// ---------- 静态资源 ----------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.Assets == nil {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(r.URL.Path, "/")
	if p == "" {
		p = "index.html"
	}
	f, err := s.Assets.Open(p)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		s.serveIndex(w, r)
		return
	}
	ct := mime.TypeByExtension(filepath.Ext(p))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if strings.HasSuffix(p, ".html") {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, p, st.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	if s.IndexRaw == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(s.IndexRaw)
}
