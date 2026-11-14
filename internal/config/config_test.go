package config

import (
	"path/filepath"
	"testing"
)

func TestParseRanges(t *testing.T) {
	cases := []struct {
		in      string
		want    []Range
		wantErr bool
	}{
		{"1-65535", []Range{{1, 65535}}, false},
		{"1-1024,8000-8100", []Range{{1, 1024}, {8000, 8100}}, false},
		{"8080", []Range{{8080, 8080}}, false},
		{" 80 , 443 ", []Range{{80, 80}, {443, 443}}, false},
		{"", nil, true},
		{"0-100", nil, true},     // 下界越界
		{"1-70000", nil, true},   // 上界越界
		{"9000-8000", nil, true}, // 倒置
		{"abc", nil, true},
		{"1-", nil, true},
	}
	for _, c := range cases {
		got, err := ParseRanges(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseRanges(%q) 期望报错，实际成功: %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRanges(%q) 意外报错: %v", c.in, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseRanges(%q) 得到 %v，期望 %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseRanges(%q)[%d] = %v，期望 %v", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"ADMIN_PORT", "DATA_DIR", "SCAN_RANGES", "SCAN_INTERVAL", "TLS_SELF_SIGNED",
		"SCAN_HOST_PROC", "SCAN_DOCKER_SOCKET", "SCAN_COMPOSE_DIR", "SCAN_ON_START", "LOG_LEVEL",
		"PROC_ROOT", "DOCKER_HOST", "SELF_CONTAINER_NAME", "SCAN_TARGET_HOST", "ACTIVE_SCAN_TIMEOUT_MS"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.AdminPort != 443 {
		t.Errorf("默认 AdminPort 期望 443，实际 %d", c.AdminPort)
	}
	if c.DataDir != "/data" {
		t.Errorf("默认 DataDir 期望 /data，实际 %s", c.DataDir)
	}
	if c.ScanRanges != "1-65535" {
		t.Errorf("默认 ScanRanges 期望 1-65535，实际 %s", c.ScanRanges)
	}
	if c.ScanInterval.Seconds() != 300 {
		t.Errorf("默认 ScanInterval 期望 300s，实际 %v", c.ScanInterval)
	}
	if !c.ScanOnStart || !c.ScanHostProc || !c.ScanDockerSocket {
		t.Errorf("默认开关期望全部为 true")
	}
	if c.TLSSelfSigned {
		t.Errorf("默认 TLSSelfSigned 期望 false")
	}
	if c.ProcRoot != "/host/proc" || c.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("默认挂载路径不符: %s / %s", c.ProcRoot, c.DockerHost)
	}
	if c.LogDir != filepath.Join("/data", "logs") || c.BackupDir != filepath.Join("/data", "backups") || c.DBPath != filepath.Join("/data", "port-ledger.db") {
		t.Errorf("派生路径不符: %s / %s / %s", c.LogDir, c.BackupDir, c.DBPath)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("ADMIN_PORT", "8899")
	t.Setenv("SCAN_INTERVAL", "0")
	t.Setenv("TLS_SELF_SIGNED", "true")
	t.Setenv("SCAN_HOST_PROC", "false")
	t.Setenv("DATA_DIR", "/mnt/data")
	t.Setenv("SCAN_RANGES", "1024-65535")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if c.AdminPort != 8899 {
		t.Errorf("AdminPort = %d，期望 8899", c.AdminPort)
	}
	if c.ScanInterval != 0 {
		t.Errorf("ScanInterval = %v，期望 0", c.ScanInterval)
	}
	if !c.TLSSelfSigned {
		t.Errorf("TLSSelfSigned 期望 true")
	}
	if c.ScanHostProc {
		t.Errorf("ScanHostProc 期望 false")
	}
	if c.DBPath != filepath.Join("/mnt/data", "port-ledger.db") {
		t.Errorf("DBPath = %s", c.DBPath)
	}
}

func TestLoadInvalid(t *testing.T) {
	cases := []struct{ key, val string }{
		{"ADMIN_PORT", "70000"},
		{"ADMIN_PORT", "abc"},
		{"SCAN_INTERVAL", "xyz"},
		{"SCAN_RANGES", "9000-8000"},
		{"LOG_LEVEL", "trace"},
	}
	for _, c := range cases {
		t.Run(c.key+"="+c.val, func(t *testing.T) {
			t.Setenv("ADMIN_PORT", "443")
			t.Setenv("SCAN_INTERVAL", "300")
			t.Setenv("SCAN_RANGES", "1-65535")
			t.Setenv("LOG_LEVEL", "info")
			t.Setenv(c.key, c.val)
			if _, err := Load(); err == nil {
				t.Errorf("期望 %s=%s 报错，实际通过", c.key, c.val)
			}
		})
	}
}

func TestVersionNonEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version 不应为空")
	}
}
