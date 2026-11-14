package dict

import "testing"

func TestBuiltinLoads(t *testing.T) {
	es, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin 解析失败: %v", err)
	}
	if len(es) < 100 {
		t.Fatalf("内置字典条目过少: %d", len(es))
	}
	for _, e := range es {
		if e.Port < 1 || e.Port > 65535 {
			t.Errorf("条目端口越界: %+v", e)
		}
		if e.PortEnd != 0 && (e.PortEnd < e.Port || e.PortEnd > 65535) {
			t.Errorf("条目区间非法: %+v", e)
		}
		if e.Protocol != "tcp" && e.Protocol != "udp" && e.Protocol != "both" {
			t.Errorf("协议非法: %+v", e)
		}
		if e.Category == "" || e.DisplayName == "" {
			t.Errorf("缺少必填字段: %+v", e)
		}
		if e.Type != "builtin" {
			t.Errorf("内置条目 Type 应为 builtin: %+v", e)
		}
	}
}

func TestLookupExactAndProtocol(t *testing.T) {
	d := New(mustBuiltin(t))

	cases := []struct {
		port     int
		proto    string
		wantName string
		found    bool
	}{
		{6690, "tcp", "Synology Drive 文件同步与备份", true},
		{6690, "udp", "", false},
		{5000, "tcp", "DSM 网页 HTTP", true},
		{5001, "tcp", "DSM 网页 HTTPS", true},
		{873, "tcp", "rsync / 共享文件夹同步", true},
		{6281, "tcp", "Hyper Backup Vault", true},
		{22, "tcp", "SSH / SFTP", true},
		{123, "udp", "NTP 时间同步", true},
		{123, "tcp", "", false},                // 仅 udp
		{161, "tcp", "SNMP / 资源监控", true},      // both 覆盖 tcp
		{161, "udp", "SNMP / 资源监控", true},      // both 覆盖 udp
		{8505, "tcp", "MailPlus Server", true}, // 区间 8500-8520
		{6969, "tcp", "BT（旧版本）", true},         // 区间 6890-6999
		{7654, "tcp", "HA 数据同步", true},         // 区间 7400-7999
		{21000, "tcp", "", false},              // 未命中
	}
	for _, c := range cases {
		got, ok := d.Lookup(c.port, c.proto)
		if ok != c.found {
			t.Errorf("Lookup(%d,%s) found=%v，期望 %v", c.port, c.proto, ok, c.found)
			continue
		}
		if ok && got.DisplayName != c.wantName {
			t.Errorf("Lookup(%d,%s) = %q，期望 %q", c.port, c.proto, got.DisplayName, c.wantName)
		}
	}
}

func TestSinglePortBeatsRange(t *testing.T) {
	d := New([]Entry{
		{Port: 3260, PortEnd: 3265, Protocol: "tcp", Category: "虚拟化", DisplayName: "区间名"},
		{Port: 3260, PortEnd: 0, Protocol: "tcp", Category: "备份同步", DisplayName: "单端口名"},
	})
	got, ok := d.Lookup(3260, "tcp")
	if !ok || got.DisplayName != "单端口名" {
		t.Fatalf("单端口条目应优先于区间条目，实际: %+v ok=%v", got, ok)
	}
	got2, ok2 := d.Lookup(3262, "tcp")
	if !ok2 || got2.DisplayName != "区间名" {
		t.Fatalf("区间内端口应命中区间条目，实际: %+v ok=%v", got2, ok2)
	}
}

func TestStableTieBreak(t *testing.T) {
	// 端口 80 在字典中有两条（文档同步 / Web 服务），稳定排序应保持首次出现者优先
	d := New(mustBuiltin(t))
	got, ok := d.Lookup(80, "tcp")
	if !ok {
		t.Fatal("端口 80 应命中字典")
	}
	if got.Category != "文档同步" {
		t.Errorf("端口 80 应命中「文档同步」条目（首次出现），实际 %s", got.Category)
	}
}

func TestCategoriesAndSize(t *testing.T) {
	d := New(mustBuiltin(t))
	cats := d.Categories()
	if len(cats) < 8 {
		t.Errorf("类别数量过少: %v", cats)
	}
	seen := map[string]bool{}
	for _, c := range cats {
		if seen[c] {
			t.Errorf("类别重复: %s", c)
		}
		seen[c] = true
	}
	if d.Size() < 100 {
		t.Errorf("Size = %d", d.Size())
	}
}

func TestEntriesReturnsCopy(t *testing.T) {
	d := New(mustBuiltin(t))
	a := d.Entries()
	a[0].DisplayName = "被修改"
	b := d.Entries()
	if b[0].DisplayName == "被修改" {
		t.Fatal("Entries 应返回副本，外部修改不应影响内部状态")
	}
}

func mustBuiltin(t *testing.T) []Entry {
	t.Helper()
	es, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	return es
}
