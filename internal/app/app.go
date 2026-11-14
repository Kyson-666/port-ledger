// Package app 编排扫描、归并、视图与健康状态，是 API 层之下的业务门面。
package app

import (
	"errors"
	"sync"
	"time"

	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/logx"
	"github.com/kyson-666/port-ledger/internal/scanner"
	"github.com/kyson-666/port-ledger/internal/store"
)

// ErrScanning 表示已有扫描在进行中。
var ErrScanning = errors.New("已有扫描正在进行")

// App 是应用门面。
type App struct {
	Cfg   *config.Config
	Store *store.Store
	Log   *logx.Logger

	mu         sync.RWMutex
	dictCache  *dict.Dict
	health     []scanner.Health
	lastErr    string
	scanning   bool
	startedAt  time.Time
	lastScanID int64
}

// New 构造 App 并加载生效字典。
func New(cfg *config.Config, st *store.Store, lg *logx.Logger) (*App, error) {
	a := &App{Cfg: cfg, Store: st, Log: lg, startedAt: time.Now()}
	if err := a.ReloadDict(); err != nil {
		return nil, err
	}
	return a, nil
}

// ReloadDict 重建生效字典缓存（内置 + 覆盖 + 自定义）。
func (a *App) ReloadDict() error {
	entries, err := a.Store.EffectiveDict()
	if err != nil {
		return err
	}
	d := dict.New(entries)
	a.mu.Lock()
	a.dictCache = d
	a.mu.Unlock()
	return nil
}

// Dict 返回生效字典。
func (a *App) Dict() *dict.Dict {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.dictCache
}

// Scanning 返回是否有扫描在进行。
func (a *App) Scanning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.scanning
}

// StartedAt 返回进程启动时间。
func (a *App) StartedAt() time.Time { return a.startedAt }

// SourceHealth 最近一次扫描的各数据源健康状态。
func (a *App) SourceHealth() []scanner.Health {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]scanner.Health, len(a.health))
	copy(out, a.health)
	return out
}

func (a *App) setHealth(hs []scanner.Health) {
	a.mu.Lock()
	a.health = hs
	a.mu.Unlock()
}
