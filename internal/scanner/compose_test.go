package scanner

import (
	"path/filepath"
	"testing"
)

func TestComposePortString(t *testing.T) {
	mk := func(lo, hi int, proto string) []ComposePort {
		var out []ComposePort
		for p := lo; p <= hi; p++ {
			out = append(out, ComposePort{Port: p, Protocol: proto})
		}
		return out
	}
	cases := []struct {
		in    string
		ports []int
		n     int
	}{
		{"8080:80", []int{8080}, 1},
		{"127.0.0.1:8443:443", []int{8443}, 1},
		{"9000/tcp", nil, 0}, // 仅容器端口
		{"8080", nil, 0},     // 仅容器端口
		{"7000-7002:7000-7002", []int{7000, 7001, 7002}, 3},
		{"${PORT}:80", nil, 0}, // 变量插值不可静态求值
		{"3000:3000/udp", []int{3000}, 1},
	}
	for _, c := range cases {
		got := parsePortString(c.in, "f.yml", "svc", mk)
		if len(got) != c.n {
			t.Errorf("parsePortString(%q) 得到 %d 条，期望 %d 条（%+v）", c.in, len(got), c.n, got)
			continue
		}
		for i, p := range c.ports {
			if got[i].Port != p {
				t.Errorf("parsePortString(%q)[%d].Port = %d，期望 %d", c.in, i, got[i].Port, p)
			}
		}
	}
}

func TestComposePortMap(t *testing.T) {
	mk := func(lo, hi int, proto string) []ComposePort {
		return []ComposePort{{Port: lo, Protocol: proto}}
	}
	got := parsePortMap(map[string]any{"target": 5432, "published": 15432, "protocol": "tcp"}, "f", "db", mk)
	if len(got) != 1 || got[0].Port != 15432 || got[0].Protocol != "tcp" {
		t.Fatalf("长语法解析不符: %+v", got)
	}
	// mode=host 且无 published → 使用容器端口
	got = parsePortMap(map[string]any{"target": 6060, "mode": "host"}, "f", "svc", mk)
	if len(got) != 1 || got[0].Port != 6060 {
		t.Fatalf("host 模式解析不符: %+v", got)
	}
	// 无 published 且非 host → 跳过
	got = parsePortMap(map[string]any{"target": 80}, "f", "svc", mk)
	if len(got) != 0 {
		t.Fatalf("无宿主端口应跳过: %+v", got)
	}
	// published 为字符串
	got = parsePortMap(map[string]any{"target": 80, "published": "18080"}, "f", "svc", mk)
	if len(got) != 1 || got[0].Port != 18080 {
		t.Fatalf("published 字符串解析不符: %+v", got)
	}
}

func TestParsePortRange(t *testing.T) {
	if lo, hi, ok := parsePortRange("7000-7002"); !ok || lo != 7000 || hi != 7002 {
		t.Fatalf("区间解析不符: %d %d %v", lo, hi, ok)
	}
	if lo, hi, ok := parsePortRange("8080"); !ok || lo != 8080 || hi != 8080 {
		t.Fatalf("单值解析不符: %d %d %v", lo, hi, ok)
	}
	if _, _, ok := parsePortRange("abc"); ok {
		t.Fatal("非法值不应解析成功")
	}
	if _, _, ok := parsePortRange(""); ok {
		t.Fatal("空值不应解析成功")
	}
}

func TestComposeScannerFixture(t *testing.T) {
	cs := &ComposeScanner{Dir: filepath.Join("testdata", "compose")}
	ports, hs, err := cs.Scan()
	if err != nil {
		t.Fatalf("Scan 报错: %v", err)
	}
	if len(hs) != 1 || !hs[0].OK {
		t.Fatalf("健康状态异常: %+v", hs)
	}
	got := map[string]string{}
	for _, p := range ports {
		got[key(p.Port, p.Protocol)] = p.Service
	}
	want := []string{"8080/tcp", "8443/tcp", "7000/tcp", "7001/tcp", "7002/tcp", "15432/tcp", "6060/tcp", "3000/udp"}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("缺少声明的端口: %s（实际: %v）", k, got)
		}
	}
	for _, k := range []string{"9000/tcp", "80/tcp"} {
		if _, ok := got[k]; ok {
			t.Errorf("不应包含仅容器端口: %s", k)
		}
	}
	if len(ports) != len(want) {
		t.Errorf("端口数 = %d，期望 %d（实际: %v）", len(ports), len(want), got)
	}
	// 服务名应被记录
	if got["8080/tcp"] != "web" {
		t.Errorf("8080 的服务名应为 web，实际 %q", got["8080/tcp"])
	}
}

func TestComposeScannerMissingDir(t *testing.T) {
	cs := &ComposeScanner{Dir: filepath.Join("testdata", "no-such-dir")}
	ports, hs, err := cs.Scan()
	if err != nil {
		t.Fatalf("目录缺失时不应返回错误（应降级）: %v", err)
	}
	if len(ports) != 0 {
		t.Errorf("不应有结果: %+v", ports)
	}
	if len(hs) != 1 || hs[0].OK {
		t.Errorf("健康状态应为异常: %+v", hs)
	}
}
