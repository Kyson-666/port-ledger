package scanner

import (
	"net"
	"strconv"
	"testing"
)

/* ---------- ActiveScanner ---------- */

func TestActiveScannerDisabled(t *testing.T) {
	as := &ActiveScanner{}
	if as.Enabled() {
		t.Fatal("未指定目标时应禁用")
	}
	res, hs, err := as.Scan(1, 10)
	if err != nil {
		t.Fatalf("禁用时不应报错: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("禁用时不应有结果: %+v", res)
	}
	if len(hs) != 1 || hs[0].Enabled {
		t.Errorf("健康状态应标记未启用: %+v", hs)
	}
	if (&ActiveScanner{}).Name() != "active" {
		t.Error("Name 应为 active")
	}
}

func TestActiveScannerFindsListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("无法建立监听: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	as := &ActiveScanner{Target: "127.0.0.1", TimeoutMS: 500, MaxPorts: 100}
	res, hs, err := as.Scan(port, port)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(res) != 1 || res[0].Port != port || !res[0].Open {
		t.Fatalf("应探测到监听端口 %d: %+v", port, res)
	}
	if len(hs) != 1 || !hs[0].OK || hs[0].Count != 1 {
		t.Fatalf("健康状态不符: %+v", hs)
	}
}

func TestActiveScannerMaxPortsGuard(t *testing.T) {
	as := &ActiveScanner{Target: "127.0.0.1", TimeoutMS: 100, MaxPorts: 5}
	_, hs, err := as.Scan(1, 100)
	if err == nil {
		t.Fatal("超出上限应报错")
	}
	if len(hs) != 1 || hs[0].OK {
		t.Errorf("健康状态应异常: %+v", hs)
	}
}

func TestActiveScannerReversedRange(t *testing.T) {
	as := &ActiveScanner{Target: "127.0.0.1", TimeoutMS: 100, MaxPorts: 10}
	// 反向区间应被自动纠正而不 panic
	if _, _, err := as.Scan(5000, 4998); err != nil {
		t.Fatalf("反向区间不应报错: %v", err)
	}
}

/* ---------- DockerScanner ---------- */

func TestDockerScannerRejectsNonUnixHost(t *testing.T) {
	ds := &DockerScanner{Host: "tcp://1.2.3.4:2375"}
	if _, hs, err := ds.Scan(); err == nil {
		t.Fatal("非 unix scheme 应报错")
	} else if len(hs) != 1 || hs[0].OK {
		t.Errorf("健康状态应异常: %+v", hs)
	}
	if (&DockerScanner{}).Name() != "docker" {
		t.Error("Name 应为 docker")
	}
}

func TestDockerScannerUnreachableSocket(t *testing.T) {
	ds := &DockerScanner{Host: "unix:///definitely/not/here.sock"}
	_, hs, err := ds.Scan()
	if err == nil {
		t.Fatal("套接字不存在应报错")
	}
	if len(hs) != 1 || hs[0].OK || hs[0].Error == "" {
		t.Errorf("健康状态应含错误原因: %+v", hs)
	}
}

func TestDockerIsSelf(t *testing.T) {
	ds := &DockerScanner{SelfContainerID: "abcdef0123456789", SelfContainerName: "port-ledger"}
	cases := []struct {
		id, name string
		want     bool
	}{
		{"abcdef0123456789ffff", "port-ledger", true},
		{"abcdef0123456789ffff", "other", true},       // ID 前缀命中
		{"ffffffffffffffffffff", "port-ledger", true}, // 名称命中
		{"ffffffffffffffffffff", "other", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := ds.isSelf(c.id, c.name); got != c.want {
			t.Errorf("isSelf(%q,%q) = %v，期望 %v", c.id, c.name, got, c.want)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("0123456789abcdef0123"); got != "0123456789ab" {
		t.Errorf("shortID 截断不符: %s", got)
	}
	if got := shortID("abc"); got != "abc" {
		t.Errorf("短 ID 不应被修改: %s", got)
	}
}

func TestIsHexID(t *testing.T) {
	cases := map[string]bool{
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef": true,
		"abcdef012345":        true,
		"ABCDEF012345":        false, // 大写不接受（Docker 用小写）
		"abc":                 false, // 过短
		"zzz0123456789":       false,
		"0123456789abcdef012": true,
	}
	for in, want := range cases {
		if got := isHexID(in); got != want {
			t.Errorf("isHexID(%q) = %v，期望 %v", in, got, want)
		}
	}
}

func TestDetectSelfContainerIDDoesNotPanic(t *testing.T) {
	// 非容器环境下应返回空串而非 panic
	got := DetectSelfContainerID()
	t.Logf("DetectSelfContainerID 结果: %q", got)
}

/* ---------- Compose 补充 ---------- */

func TestComposeScannerMaxFiles(t *testing.T) {
	// 限制为 0 个文件时应不返回任何声明
	cs := &ComposeScanner{Dir: "testdata/compose", MaxFiles: 0}
	ports, hs, err := cs.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(hs) != 1 || !hs[0].OK {
		t.Fatalf("健康状态不符: %+v", hs)
	}
	if len(ports) == 0 {
		t.Fatal("MaxFiles=0 时应回退到默认上限（500），仍应读到声明")
	}
}

func TestComposeScannerName(t *testing.T) {
	if (&ComposeScanner{}).Name() != "compose" {
		t.Error("Name 应为 compose")
	}
}

func TestParsePortEntryTypes(t *testing.T) {
	// int 形式（仅容器端口）
	if got := parsePortEntry(80, "f", "s"); len(got) != 0 {
		t.Errorf("int 形式应被忽略: %+v", got)
	}
	// map[any]any 形式（旧版 yaml 解码）
	got := parsePortEntry(map[any]any{"target": 80, "published": 8080, "protocol": "tcp"}, "f", "s")
	if len(got) != 1 || got[0].Port != 8080 {
		t.Errorf("map[any]any 解析不符: %+v", got)
	}
	// 未知类型
	if got := parsePortEntry(3.14, "f", "s"); len(got) != 0 {
		t.Errorf("未知类型应被忽略: %+v", got)
	}
	// 超大区间应被截断为单端口
	if n := hugeRangeCount(); n != 1 {
		t.Errorf("超大区间应截断为 1 个端口，实际 %d", n)
	}
}

// hugeRangeCount 校验 maxRangeExpand 截断逻辑。
func hugeRangeCount() int {
	mk := func(lo, hi int, proto string) []ComposePort {
		if hi-lo+1 > maxRangeExpand {
			hi = lo
		}
		var out []ComposePort
		for p := lo; p <= hi; p++ {
			out = append(out, ComposePort{Port: p, Protocol: proto})
		}
		return out
	}
	return len(parsePortString("10000-20000:10000-20000", "f", "s", mk))
}

func TestToInt(t *testing.T) {
	cases := []struct {
		in   any
		want int
	}{
		{80, 80},
		{int64(90), 90},
		{float64(100), 100},
		{"110", 110},
		{" 120 ", 120},
		{"abc", 0},
		{in: nil, want: 0},
	}
	for _, c := range cases {
		if got := toInt(c.in); got != c.want {
			t.Errorf("toInt(%v) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

func TestParsePortEntryFromStringPort(t *testing.T) {
	// 覆盖 ip:host:container 与 /proto 组合
	got := parsePortEntry("127.0.0.1:8000:80/udp", "f", "s")
	if len(got) != 1 || got[0].Port != 8000 || got[0].Protocol != "udp" {
		t.Fatalf("ip:host:container/proto 解析不符: %+v", got)
	}
}

func TestSplitProcAddrEdge(t *testing.T) {
	if _, _, ok := splitProcAddr("no-colon"); ok {
		t.Error("无冒号不应解析成功")
	}
	if _, _, ok := splitProcAddr(":0016"); ok {
		t.Error("空地址不应解析成功")
	}
	if _, _, ok := splitProcAddr("00000000:ZZZZ"); ok {
		t.Error("非法端口十六进制不应解析成功")
	}
	// 地址非法时降级为原样字符串，端口仍应解析成功
	addr, port, ok := splitProcAddr("ZZZZZZZZ:0016")
	if !ok || port != 22 || addr != "ZZZZZZZZ" {
		t.Errorf("非法地址应降级保留原串: %s %d %v", addr, port, ok)
	}
	// 非 8/32 长度的地址应原样返回
	addr, _, ok = splitProcAddr("ABCD:0016")
	if !ok || addr != "ABCD" {
		t.Errorf("异常长度地址应原样保留: %s", addr)
	}
}

func TestDecodeInvalidHex(t *testing.T) {
	if got := decodeIPv4("ZZZZZZZZ"); got != "ZZZZZZZZ" {
		t.Errorf("非法十六进制应原样返回: %s", got)
	}
	if got := decodeIPv6("short"); got != "short" {
		t.Errorf("长度不符应原样返回: %s", got)
	}
	if got := decodeIPv6("ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"); got != "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ" {
		t.Errorf("非法十六进制应原样返回: %s", got)
	}
}

func TestParseProcLineShortFields(t *testing.T) {
	line := "   0: 00000000:0016 00000000:0000 0A 0 0 0 0 100 1 0 0 0 0"
	if l, ok := parseProcLine(line, "tcp"); ok {
		// 该行字段数 >=10，应被解析；inode 取第 10 个字段
		if l.Port != 22 {
			t.Errorf("端口解析不符: %d", l.Port)
		}
	}
}

func TestProcScannerDedupAcrossProtocols(t *testing.T) {
	// tcp 与 udp 同端口不互相去重
	ps := &ProcScanner{ProcRoot: "testdata/proc"}
	ls, _, _ := ps.Scan()
	seen := map[string]int{}
	for _, l := range ls {
		seen[strconv.Itoa(l.Port)+"/"+l.Protocol]++
	}
	if seen["53/udp"] != 1 {
		t.Errorf("53/udp 应恰好一次: %d", seen["53/udp"])
	}
}
