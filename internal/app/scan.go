package app

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/reconcile"
	"github.com/kyson-666/port-ledger/internal/scanner"
)

// ScanResult 是一次扫描的结果摘要。
type ScanResult struct {
	ScanID int64          `json:"scan_id"`
	Status string         `json:"status"`
	Counts map[string]int `json:"counts"`
	Health []scanner.Health `json:"source_health"`
}

// ScanOptions 控制单次扫描。
type ScanOptions struct {
	Active bool // 是否执行主动扫描
	Lo, Hi int  // 主动扫描范围（Active 为真时生效）
}

// Scan 执行一次完整扫描：采集 → 归并 → 落库 → 记录运行。
func (a *App) Scan(opt ScanOptions) (*ScanResult, error) {
	a.mu.Lock()
	if a.scanning {
		a.mu.Unlock()
		return nil, ErrScanning
	}
	a.scanning = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.scanning = false
		a.mu.Unlock()
	}()

	scope := fmt.Sprintf("ranges=%s", a.Cfg.ScanRanges)
	scanID, err := a.Store.BeginScan(scope)
	if err != nil {
		return nil, err
	}

	var (
		healths    []scanner.Health
		listeners  []scanner.Listener
		containers []scanner.ContainerPort
		composeLs  []scanner.ComposePort
		activeKeys = map[string]bool{}
		failed     int
		enabled    int
	)

	if a.Cfg.ScanHostProc {
		enabled++
		ps := &scanner.ProcScanner{ProcRoot: a.Cfg.ProcRoot}
		l, h, _ := ps.Scan()
		listeners, healths = l, append(healths, h...)
		if len(h) > 0 && !h[0].OK {
			failed++
			a.Log.Warnf("宿主监听表采集失败", map[string]any{"error": h[0].Error})
		}
	}

	if a.Cfg.ScanDockerSocket {
		enabled++
		ds := &scanner.DockerScanner{
			Host:              a.Cfg.DockerHost,
			SelfContainerID:   selfContainerID(),
			SelfContainerName: a.Cfg.SelfContainer,
		}
		c, h, _ := ds.Scan()
		containers, healths = c, append(healths, h...)
		if len(h) > 0 && !h[0].OK {
			failed++
			a.Log.Warnf("Docker API 采集失败", map[string]any{"error": h[0].Error})
		}
	}

	if a.Cfg.ScanComposeDir != "" {
		enabled++
		cs := &scanner.ComposeScanner{Dir: a.Cfg.ScanComposeDir}
		cp, h, _ := cs.Scan()
		composeLs, healths = cp, append(healths, h...)
		if len(h) > 0 && !h[0].OK {
			failed++
			a.Log.Warnf("Compose 声明采集失败", map[string]any{"error": h[0].Error})
		}
	}

	// self 端口：本工具容器发布的宿主端口
	selfKeys := map[string]bool{}
	for _, c := range containers {
		if c.IsSelf {
			selfKeys[fmt.Sprintf("%d/%s", c.Port, c.Protocol)] = true
		}
	}

	if opt.Active || a.Cfg.ScanTargetHost != "" {
		as := &scanner.ActiveScanner{Target: a.Cfg.ScanTargetHost, TimeoutMS: a.Cfg.ActiveTimeoutMS, MaxPorts: 2000}
		if as.Enabled() {
			lo, hi := opt.Lo, opt.Hi
			if lo == 0 {
				lo, hi = 1, 65535
			}
			enabled++
			res, h, err := as.Scan(lo, hi)
			healths = append(healths, h...)
			if err != nil {
				failed++
				a.Log.Warnf("主动扫描失败", map[string]any{"error": err.Error()})
			}
			for _, r := range res {
				activeKeys[fmt.Sprintf("%d/%s", r.Port, r.Proto)] = true
			}
		}
	}

	reservations, err := a.Store.ActiveReservations()
	if err != nil {
		return nil, err
	}
	ranges, err := config.ParseRanges(a.Cfg.ScanRanges)
	if err != nil {
		return nil, err
	}

	obs := reconcile.Run(reconcile.Input{
		Listeners:    listeners,
		Containers:   containers,
		Compose:      composeLs,
		Active:       activeKeys,
		Dict:         a.Dict(),
		Reservations: reservations,
		Ranges:       ranges,
		SelfKeys:     selfKeys,
	})

	if err := a.Store.ReplaceObservations(scanID, obs); err != nil {
		_ = a.Store.FinishScan(scanID, "failed", "", "")
		return nil, err
	}

	counts := reconcile.Counts(obs)
	status := "ok"
	if enabled > 0 && failed == enabled {
		status = "failed"
	} else if failed > 0 {
		status = "partial"
	}

	countsJSON, _ := json.Marshal(counts)
	healthJSON, _ := json.Marshal(healths)
	if err := a.Store.FinishScan(scanID, status, string(countsJSON), string(healthJSON)); err != nil {
		return nil, err
	}

	a.setHealth(healths)
	a.mu.Lock()
	a.lastScanID = scanID
	a.mu.Unlock()

	a.Log.Infof("扫描完成", map[string]any{
		"scan_id": scanID, "status": status,
		"observations": len(obs), "counts": counts,
	})
	a.Log.Scanf(time.Now(), "scan_id=%d status=%s observations=%d counts=%v", scanID, status, len(obs), counts)

	return &ScanResult{ScanID: scanID, Status: status, Counts: counts, Health: healths}, nil
}

var (
	selfIDOnce sync.Once
	selfIDVal  string
)

// selfContainerID 惰性探测本进程所在容器 ID。
func selfContainerID() string {
	selfIDOnce.Do(func() { selfIDVal = scanner.DetectSelfContainerID() })
	return selfIDVal
}
