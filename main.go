// PortLedger — 群晖 Docker 端口盘点与用途管理工具。
//
// 单二进制 + 内嵌前端 + 纯 Go SQLite，可编译为 linux/amd64、linux/arm64 并可落到 FROM scratch。
package main

import (
	"context"
	"crypto/tls"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kyson-666/port-ledger/internal/api"
	"github.com/kyson-666/port-ledger/internal/app"
	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/store"
	"github.com/kyson-666/port-ledger/internal/tlsx"
)

//go:embed all:web
var webFS embed.FS

func main() {
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	healthCheck := flag.Bool("health", false, "对本地管理端口做一次健康检查后退出（供容器 HEALTHCHECK 使用）")
	flag.Parse()
	if *showVersion {
		fmt.Println(config.Version)
		return
	}
	if *healthCheck {
		if err := runHealthCheck(); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		fmt.Println("healthy")
		return
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

// runHealthCheck 请求本地 /api/health。scratch 镜像内没有 shell 与 curl，
// 因此由二进制自身承担健康探针。
func runHealthCheck() error {
	port := os.Getenv("ADMIN_PORT")
	if port == "" {
		port = "443"
	}
	scheme := "http"
	if v := os.Getenv("TLS_SELF_SIGNED"); v == "1" || v == "true" {
		scheme = "https"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(scheme + "://127.0.0.1:" + port + "/api/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("状态码 %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	lg, err := logx.New(cfg.LogDir, logx.ParseLevel(cfg.LogLevel), true)
	if err != nil {
		return err
	}
	defer lg.Close()

	lg.Infof("PortLedger 启动", map[string]any{
		"version": cfg.Version, "admin_port": cfg.AdminPort, "data_dir": cfg.DataDir,
		"proc_root": cfg.ProcRoot, "docker_host": cfg.DockerHost,
		"scan_ranges": cfg.ScanRanges, "scan_interval_s": int(cfg.ScanInterval.Seconds()),
		"tls_self_signed": cfg.TLSSelfSigned,
	})

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer st.Close()

	builtin, err := dict.Builtin()
	if err != nil {
		return err
	}
	if err := st.SeedDict(builtin); err != nil {
		return fmt.Errorf("写入内置字典失败: %w", err)
	}

	a, err := app.New(cfg, st, lg)
	if err != nil {
		return err
	}
	if err := a.ApplySettings(); err != nil {
		lg.Warnf("应用库内设置失败，继续使用环境变量", map[string]any{"error": err.Error()})
	}

	// 扫描明细保留清理
	if n := lg.CleanupScanLogs(30); n > 0 {
		lg.Infof("清理过期扫描日志", map[string]any{"removed": n})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 首启自动全量扫描
	n, err := st.CountScans()
	if err != nil {
		return err
	}
	if cfg.ScanOnStart && n == 0 {
		go func() {
			lg.Infof("执行首次全量扫描", nil)
			if _, err := a.Scan(app.ScanOptions{}); err != nil {
				lg.Errorf("首次扫描失败", map[string]any{"error": err.Error()})
			}
		}()
	}

	// 周期扫描 + 每日日志清理
	go scheduler(ctx, a, lg)

	assets, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	srv, err := api.New(a, lg, assets)
	if err != nil {
		return err
	}

	addr := ":" + strconv.Itoa(cfg.AdminPort)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	useTLS := cfg.TLSSelfSigned || (cfg.TLSCert != "" && cfg.TLSKey != "")
	if useTLS {
		var cert tls.Certificate
		if cfg.TLSCert != "" && cfg.TLSKey != "" {
			cert, err = tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		} else {
			cert, err = tlsx.SelfSigned([]string{cfg.ScanTargetHost, "port-ledger"})
		}
		if err != nil {
			return err
		}
		httpSrv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}

	errCh := make(chan error, 1)
	go func() {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			errCh <- err
			return
		}
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		lg.Infof("管理台已就绪", map[string]any{"url": scheme + "://0.0.0.0" + addr, "tls": useTLS})
		if useTLS {
			errCh <- httpSrv.ServeTLS(ln, "", "")
		} else {
			errCh <- httpSrv.Serve(ln)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case sig := <-sigCh:
		lg.Infof("收到退出信号，正在关闭", map[string]any{"signal": sig.String()})
		shctx, shcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shcancel()
		_ = httpSrv.Shutdown(shctx)
	}
	return nil
}

// scheduler 按 SCAN_INTERVAL 周期扫描，并每日清理过期扫描日志。
func scheduler(ctx context.Context, a *app.App, lg *logx.Logger) {
	lastCleanup := time.Now()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastScan := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			iv := a.Cfg.ScanInterval
			if iv > 0 && now.Sub(lastScan) >= iv {
				lastScan = now
				if a.Scanning() {
					continue
				}
				if _, err := a.Scan(app.ScanOptions{}); err != nil {
					lg.Warnf("周期扫描失败", map[string]any{"error": err.Error()})
				}
			}
			if now.Sub(lastCleanup) >= 24*time.Hour {
				lastCleanup = now
				if n := lg.CleanupScanLogs(30); n > 0 {
					lg.Infof("清理过期扫描日志", map[string]any{"removed": n})
				}
			}
		}
	}
}
