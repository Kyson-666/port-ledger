package tlsx

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"testing"
	"time"
)

func TestSelfSignedCertificate(t *testing.T) {
	cert, err := SelfSigned([]string{"192.168.1.10", "nas.local", ""})
	if err != nil {
		t.Fatalf("SelfSigned: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("证书链为空")
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("解析证书: %v", err)
	}

	if leaf.Subject.CommonName != "PortLedger Self-Signed" {
		t.Errorf("CN 不符: %s", leaf.Subject.CommonName)
	}
	if !leaf.NotAfter.After(time.Now().AddDate(0, 11, 0)) {
		t.Errorf("有效期过短: %v", leaf.NotAfter)
	}
	if !leaf.NotBefore.Before(time.Now()) {
		t.Errorf("NotBefore 应在过去: %v", leaf.NotBefore)
	}

	dns := map[string]bool{}
	for _, d := range leaf.DNSNames {
		dns[d] = true
	}
	if !dns["localhost"] || !dns["nas.local"] {
		t.Errorf("DNS 名称缺失: %v", leaf.DNSNames)
	}
	ips := map[string]bool{}
	for _, ip := range leaf.IPAddresses {
		ips[ip.String()] = true
	}
	for _, want := range []string{"127.0.0.1", "::1", "192.168.1.10"} {
		if !ips[want] {
			t.Errorf("缺少 IP SAN %s（实际 %v）", want, leaf.IPAddresses)
		}
	}

	var serverAuth bool
	for _, u := range leaf.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth {
			serverAuth = true
		}
	}
	if !serverAuth {
		t.Error("缺少 ServerAuth 用途")
	}
	if leaf.IsCA {
		t.Error("自签叶证书不应标记为 CA")
	}
}

func TestSelfSignedHandshake(t *testing.T) {
	cert, err := SelfSigned(nil)
	if err != nil {
		t.Fatalf("SelfSigned: %v", err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = conn.Write([]byte("pong"))
	}()

	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("TLS 握手失败: %v", err)
	}
	defer client.Close()

	if got := client.ConnectionState().Version; got < tls.VersionTLS12 {
		t.Errorf("协商版本过低: %d", got)
	}
	buf, err := io.ReadAll(io.LimitReader(client, 4))
	if err != nil || string(buf) != "pong" {
		t.Fatalf("读到 %q，错误 %v", buf, err)
	}
}

func TestSelfSignedUniqueSerials(t *testing.T) {
	a, err := SelfSigned(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SelfSigned(nil)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(a.Certificate[0])
	cb, _ := x509.ParseCertificate(b.Certificate[0])
	if ca.SerialNumber.Cmp(cb.SerialNumber) == 0 {
		t.Error("两次生成的序列号应不同")
	}
}

func TestListenerErrorPath(t *testing.T) {
	// 构造一个必然失败的监听地址，确认错误可被上层感知
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("无法建立基准监听")
	}
	defer ln.Close()
	if _, err := tls.Listen("tcp", ln.Addr().String(), &tls.Config{}); err == nil {
		t.Error("重复占用同一地址应返回错误")
	}
}
