package scanner

import (
	"path/filepath"
	"testing"
)

func TestParseProcLineTCPListen(t *testing.T) {
	line := "   0: 00000000:0016 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 100 1 0000000000000000 100 0 0 10 0"
	l, ok := parseProcLine(line, "tcp")
	if !ok {
		t.Fatal("LISTEN 行应被识别")
	}
	if l.Port != 22 || l.Protocol != "tcp" || l.BindAddr != "0.0.0.0" || l.Inode != "100" {
		t.Fatalf("解析结果不符: %+v", l)
	}
}

func TestParseProcLineSkipsNonListen(t *testing.T) {
	// ESTABLISHED（st=01）不应被视为监听
	line := "   3: 00000000:0016 0100007F:C350 01 00000000:00000000 00:00000000 00000000     0        0 200 1 0000000000000000 100 0 0 10 0"
	if _, ok := parseProcLine(line, "tcp"); ok {
		t.Fatal("非 LISTEN 行不应被识别")
	}
	// 表头
	if _, ok := parseProcLine("  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode", "tcp"); ok {
		t.Fatal("表头不应被识别")
	}
	// 字段不足
	if _, ok := parseProcLine("   0: 00000000:0016", "tcp"); ok {
		t.Fatal("字段不足的行不应被识别")
	}
	// 端口为 0
	if _, ok := parseProcLine("   0: 00000000:0000 00000000:0000 0A 0 0 0 0 0 0 0 0", "tcp"); ok {
		t.Fatal("端口 0 不应被识别")
	}
}

func TestParseProcLineUDP(t *testing.T) {
	// 未连接（服务端绑定）→ 识别
	bound := "   0: 00000000:0035 00000000:0000 07 00000000:00000000 00:00000000 00000000     0        0 400 1 0000000000000000 0 0 0 0 0"
	l, ok := parseProcLine(bound, "udp")
	if !ok || l.Port != 53 {
		t.Fatalf("UDP 绑定套接字应被识别: %+v ok=%v", l, ok)
	}
	// 已连接（客户端）→ 跳过
	conn := "   2: 0A00A8C0:E3D5 5C1E0A0A:0035 01 00000000:00000000 00:00000000 00000000     0        0 402 1 0 0 0 0 0"
	if _, ok := parseProcLine(conn, "udp"); ok {
		t.Fatal("UDP 已连接套接字不应被识别")
	}
}

func TestSplitProcAddr(t *testing.T) {
	addr, port, ok := splitProcAddr("00000000:0016")
	if !ok || port != 22 || addr != "0.0.0.0" {
		t.Fatalf("IPv4 通配解析不符: %s %d %v", addr, port, ok)
	}
	addr, _, ok = splitProcAddr("0100007F:1F90")
	if !ok || addr != "127.0.0.1" {
		t.Fatalf("IPv4 回环解析不符: %s", addr)
	}
	addr, _, ok = splitProcAddr("0A00A8C0:0CE4")
	if !ok || addr != "192.168.0.10" {
		t.Fatalf("IPv4 私网解析不符: %s", addr)
	}
	addr, port, ok = splitProcAddr("00000000000000000000000001000000:7688")
	if !ok || addr != "::1" || port != 30344 {
		t.Fatalf("IPv6 ::1 解析不符: %s %d", addr, port)
	}
	if _, _, ok := splitProcAddr("bad"); ok {
		t.Fatal("非法地址不应解析成功")
	}
}

func TestDecodeIPv4(t *testing.T) {
	cases := map[string]string{
		"00000000": "0.0.0.0",
		"0100007F": "127.0.0.1",
		"0A00A8C0": "192.168.0.10",
		"0100A8C0": "192.168.0.1",
	}
	for in, want := range cases {
		if got := decodeIPv4(in); got != want {
			t.Errorf("decodeIPv4(%s) = %s，期望 %s", in, got, want)
		}
	}
}

func TestDecodeIPv6(t *testing.T) {
	cases := map[string]string{
		"00000000000000000000000000000000": "::",
		"00000000000000000000000001000000": "::1",
	}
	for in, want := range cases {
		if got := decodeIPv6(in); got != want {
			t.Errorf("decodeIPv6(%s) = %s，期望 %s", in, got, want)
		}
	}
}

func TestProcScannerFixture(t *testing.T) {
	ps := &ProcScanner{ProcRoot: filepath.Join("testdata", "proc")}
	ls, hs, err := ps.Scan()
	if err != nil {
		t.Fatalf("Scan 报错: %v", err)
	}
	if len(hs) != 1 || !hs[0].OK {
		t.Fatalf("健康状态异常: %+v", hs)
	}
	if hs[0].Count != len(ls) {
		t.Errorf("健康计数 %d 与结果 %d 不一致", hs[0].Count, len(ls))
	}

	got := map[string]int{}
	for _, l := range ls {
		got[key(l.Port, l.Protocol)]++
	}
	want := []string{"22/tcp", "5000/tcp", "3300/tcp", "8080/tcp", "30344/tcp", "53/udp", "5353/udp"}
	if len(ls) != len(want) {
		t.Errorf("监听条目数 = %d，期望 %d（实际: %v）", len(ls), len(want), got)
	}
	for _, k := range want {
		if got[k] != 1 {
			t.Errorf("缺少或重复的条目: %s（实际 %d 次）", k, got[k])
		}
	}
	// 端口 22 在 tcp 与 tcp6 中各出现一次，应被去重
	if got["22/tcp"] != 1 {
		t.Errorf("v4/v6 同端口应去重，实际 %d", got["22/tcp"])
	}
}

func TestProcScannerMissingRoot(t *testing.T) {
	ps := &ProcScanner{ProcRoot: filepath.Join("testdata", "not-exist")}
	ls, hs, err := ps.Scan()
	if err != nil {
		t.Fatalf("缺少目录时不应返回错误（应降级）: %v", err)
	}
	if len(ls) != 0 {
		t.Errorf("不应有结果: %+v", ls)
	}
	if len(hs) != 1 || hs[0].OK {
		t.Errorf("健康状态应为异常: %+v", hs)
	}
	if hs[0].Error == "" {
		t.Errorf("异常时应带原因")
	}
}

func TestProcScannerName(t *testing.T) {
	if (&ProcScanner{}).Name() != "proc" {
		t.Fatal("Name 应为 proc")
	}
}

func key(port int, proto string) string {
	return itoa(port) + "/" + proto
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
