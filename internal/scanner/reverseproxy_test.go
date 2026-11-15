package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseReverseProxy(t *testing.T) {
	data := []byte(`{"0b22bf8d-1234":{` +
		`"backend":{"fqdn":"localhost","port":5000,"protocol":0},` +
		`"customize_headers":[],"description":"DSM",` +
		`"frontend":{"acl":null,"fqdn":"dsm.example.com","https":{"hsts":false,"http2":false},"port":443,"protocol":1},` +
		`"proxy_connect_timeout":60},"version":2,` +
		`"aa":{"backend":{"fqdn":"localhost","port":8080,"protocol":0},"description":"仅端口无域名","frontend":{"fqdn":null,"port":9443,"protocol":0}},` +
		`"bb":{"backend":{"port":1},"frontend":{"port":0,"protocol":0}}}`)
	rules, err := parseReverseProxy(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("应解析出 2 条规则（跳过 version 与非法端口），实际 %d: %+v", len(rules), rules)
	}
	byPort := map[int]ReverseProxyRule{}
	for _, r := range rules {
		byPort[r.Port] = r
	}
	r443, ok := byPort[443]
	if !ok {
		t.Fatal("缺少 443 规则")
	}
	if !r443.HTTPS || r443.Host != "dsm.example.com" || r443.BackendPort != 5000 || r443.Description != "DSM" {
		t.Errorf("443 规则字段不符: %+v", r443)
	}
	r9443, ok := byPort[9443]
	if !ok {
		t.Fatal("缺少 9443 规则")
	}
	if r9443.HTTPS || r9443.Host != "" || r9443.BackendPort != 8080 {
		t.Errorf("9443 规则字段不符: %+v", r9443)
	}
}

func TestReverseProxyScannerDisabled(t *testing.T) {
	rs := &ReverseProxyScanner{Candidates: []string{filepath.Join(t.TempDir(), "nope.json"), ""}}
	if rs.Enabled() {
		t.Error("文件不存在应返回 Enabled=false")
	}
	rules, healths, err := rs.Scan()
	if err != nil {
		t.Fatalf("Scan 不应返回 error（可降级）: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("无文件应返回 0 规则: %d", len(rules))
	}
	if len(healths) != 1 || healths[0].OK {
		t.Errorf("Health 应标记异常: %+v", healths)
	}
}

func TestReverseProxyScannerScan(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ReverseProxy.json")
	content := []byte(`{"x":{"frontend":{"port":8443,"protocol":1,"fqdn":"nas.lan"},"backend":{"port":5000},"description":"A"}}`)
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatal(err)
	}
	rs := &ReverseProxyScanner{Candidates: []string{p}}
	rules, healths, err := rs.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(rules) != 1 || rules[0].Port != 8443 || !rules[0].HTTPS {
		t.Fatalf("规则不符: %+v", rules)
	}
	if !healths[0].OK || healths[0].Count != 1 {
		t.Fatalf("Health 不符: %+v", healths)
	}
}

func TestReverseProxyScannerEmptyRules(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ReverseProxy.json")
	// DSM 未启用反代时是这种形态
	if err := os.WriteFile(p, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rs := &ReverseProxyScanner{Candidates: []string{p}}
	rules, healths, _ := rs.Scan()
	if len(rules) != 0 {
		t.Fatalf("空规则文件应返回 0 条: %+v", rules)
	}
	if !healths[0].OK {
		t.Fatal("空规则不是异常（用户只是没配反代）")
	}
}
