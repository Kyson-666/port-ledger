// Package api 提供 REST 接口与前端静态资源。
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kyson-666/port-ledger/internal/app"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/store"
)

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// Server 承载 HTTP 路由。
type Server struct {
	App      *app.App
	Log      *logx.Logger
	Assets   fs.FS  // 前端资源根（web/ 目录）
	IndexRaw []byte // 预读的 index.html，供 SPA 回退
}

// New 构造 Server。
func New(a *app.App, lg *logx.Logger, assets fs.FS) (*Server, error) {
	s := &Server{App: a, Log: lg, Assets: assets}
	if assets != nil {
		if b, err := fs.ReadFile(assets, "index.html"); err == nil {
			s.IndexRaw = b
		}
	}
	return s, nil
}

// Routes 注册全部路由。
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/scan", s.handleScan)

	mux.HandleFunc("GET /api/ports", s.handlePortsList)
	mux.HandleFunc("GET /api/ports/{port}", s.handlePortGet)
	mux.HandleFunc("PUT /api/ports/{port}", s.handlePortPut)

	mux.HandleFunc("GET /api/free", s.handleFree)

	mux.HandleFunc("GET /api/reservations", s.handleReservationsList)
	mux.HandleFunc("POST /api/reservations", s.handleReservationsCreate)
	mux.HandleFunc("PUT /api/reservations", s.handleReservationsUpdate)
	mux.HandleFunc("DELETE /api/reservations", s.handleReservationsDelete)

	mux.HandleFunc("GET /api/system-dict", s.handleDictGet)
	mux.HandleFunc("POST /api/system-dict", s.handleDictPost)
	mux.HandleFunc("GET /api/dict/export", s.handleDictExport)
	mux.HandleFunc("POST /api/dict/import", s.handleDictImport)

	mux.HandleFunc("GET /api/audit", s.handleAudit)
	mux.HandleFunc("GET /api/logs", s.handleLogs)

	mux.HandleFunc("GET /api/backup/export", s.handleBackupExport)
	mux.HandleFunc("POST /api/backup/import", s.handleBackupImport)

	mux.HandleFunc("GET /api/settings", s.handleSettingsGet)
	mux.HandleFunc("PUT /api/settings", s.handleSettingsPut)

	mux.HandleFunc("GET /", s.handleStatic)

	return withCommonHeaders(mux)
}

// 所有响应禁止缓存，避免升级后前端拿到旧 HTML / 旧数据（沿用既有工程约定）。
func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	defer io.Copy(io.Discard, r.Body)
	dec := json.NewDecoder(io.LimitReader(r.Body, 8<<20))
	return dec.Decode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	last, err := s.App.Store.LatestScan()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	scanCount, _ := s.App.Store.CountScans()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        s.App.Cfg.Version,
		"uptime_seconds": int(time.Since(s.App.StartedAt()).Seconds()),
		"scanning":       s.App.Scanning(),
		"last_scan":      last,
		"scan_count":     scanCount,
		"source_health":  s.App.SourceHealth(),
		"dict_size":      s.App.Dict().Size(),
		"config": map[string]any{
			"admin_port":          s.App.Cfg.AdminPort,
			"scan_interval":       int(s.App.Cfg.ScanInterval.Seconds()),
			"scan_ranges":         s.App.Cfg.ScanRanges,
			"scan_host_proc":      s.App.Cfg.ScanHostProc,
			"scan_docker_socket":  s.App.Cfg.ScanDockerSocket,
			"scan_compose_dir":    s.App.Cfg.ScanComposeDir,
			"tls_self_signed":     s.App.Cfg.TLSSelfSigned,
			"active_scan_target":  s.App.Cfg.ScanTargetHost,
		},
	})
}

type scanRequest struct {
	Active bool `json:"active"`
	From   int  `json:"from"`
	To     int  `json:"to"`
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	var req scanRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
			return
		}
	}
	if v := r.URL.Query().Get("active"); v == "1" || v == "true" {
		req.Active = true
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("from")); err == nil {
		req.From = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("to")); err == nil {
		req.To = v
	}

	res, err := s.App.Scan(app.ScanOptions{Active: req.Active, Lo: req.From, Hi: req.To})
	if err != nil {
		if errors.Is(err, app.ErrScanning) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePortsList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))
	from, _ := strconv.Atoi(q.Get("from"))
	to, _ := strconv.Atoi(q.Get("to"))
	res, err := s.App.List(app.ListQuery{
		Q:          q.Get("q"),
		States:     splitCSV(q.Get("state")),
		Protocol:   q.Get("protocol"),
		SourceKind: q.Get("source"),
		Project:    q.Get("project"),
		From:       from,
		To:         to,
		Page:       page,
		PageSize:   pageSize,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handlePortGet(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port < 1 || port > 65535 {
		writeErr(w, http.StatusBadRequest, "端口号非法")
		return
	}
	item, err := s.App.GetPort(port, r.URL.Query().Get("protocol"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item == nil {
		writeErr(w, http.StatusNotFound, "端口不存在")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type portPutRequest struct {
	Protocol string `json:"protocol"`
	Name     string `json:"name"`
	Note     string `json:"note"`
	Tags     string `json:"tags"`
	Project  string `json:"project"`
	Actor    string `json:"actor"`
}

func (s *Server) handlePortPut(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port < 1 || port > 65535 {
		writeErr(w, http.StatusBadRequest, "端口号非法")
		return
	}
	var req portPutRequest
	if err := readJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
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
	if len([]rune(req.Name)) > 60 {
		writeErr(w, http.StatusBadRequest, "名称长度不得超过 60 字")
		return
	}
	if len([]rune(req.Note)) > 200 {
		writeErr(w, http.StatusBadRequest, "备注长度不得超过 200 字")
		return
	}
	tags := strings.TrimSpace(req.Tags)
	if n := len(splitCSV(tags)); n > 8 {
		writeErr(w, http.StatusBadRequest, "标签最多 8 个")
		return
	}

	actor := req.Actor
	empty := strings.TrimSpace(req.Name) == "" && strings.TrimSpace(req.Note) == "" &&
		tags == "" && strings.TrimSpace(req.Project) == ""
	target := fmt.Sprintf("端口 %d/%s", port, strings.ToUpper(proto))
	if empty {
		// 全空 → 删除记录（等价于回退系统名）
		prev, _ := s.App.Store.GetPortMeta(port, proto)
		if err := s.App.Store.DeletePortMeta(port, proto); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		old := ""
		if prev != nil {
			old = prev.Name
		}
		_ = s.App.Store.AddAudit(store.Audit{
			Actor: actor, Action: "rename", Target: target,
			Detail: fmt.Sprintf("回退系统名（原自定义名：%s）", orDash(old)),
		})
	} else {
		prev, _ := s.App.Store.GetPortMeta(port, proto)
		old := ""
		if prev != nil {
			old = prev.Name
		}
		if err := s.App.Store.PutPortMeta(store.PortMeta{
			Port: port, Protocol: proto, Name: req.Name, Note: req.Note,
			Tags: tags, Project: req.Project, UpdatedBy: actor,
		}); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		_ = s.App.Store.AddAudit(store.Audit{
			Actor: actor, Action: "rename", Target: target,
			Detail: fmt.Sprintf("名称 %s → %s", orDash(old), orDash(req.Name)),
		})
	}

	item, err := s.App.GetPort(port, proto)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleFree(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, _ := strconv.Atoi(q.Get("from"))
	to, _ := strconv.Atoi(q.Get("to"))
	max, _ := strconv.Atoi(q.Get("count"))
	res, err := s.App.Free(from, to, q.Get("protocol"), max)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
