package reconcile

import (
	"testing"

	"github.com/kyson-666/port-ledger/internal/config"
	"github.com/kyson-666/port-ledger/internal/dict"
	"github.com/kyson-666/port-ledger/internal/scanner"
	"github.com/kyson-666/port-ledger/internal/store"
)

func testDict() *dict.Dict {
	return dict.New([]dict.Entry{
		{Port: 5000, Protocol: "tcp", Category: "系统管理", DisplayName: "DSM 网页 HTTP"},
		{Port: 6690, Protocol: "tcp", Category: "文档同步", DisplayName: "Synology Drive 文件同步与备份"},
		{Port: 123, Protocol: "udp", Category: "系统管理", DisplayName: "NTP 时间同步"},
	})
}

func find(obs []store.Observation, port int, proto string) (store.Observation, bool) {
	for _, o := range obs {
		if o.Port == port && o.Protocol == proto {
			return o, true
		}
	}
	return store.Observation{}, false
}

func TestRunPriority(t *testing.T) {
	obs := Run(Input{
		Dict: testDict(),
		Listeners: []scanner.Listener{
			{Port: 9000, Protocol: "tcp", BindAddr: "0.0.0.0", PID: 42, ProcessName: "portainer"},
			{Port: 8081, Protocol: "tcp", BindAddr: "0.0.0.0", PID: 43, ProcessName: "docker-proxy"},
			{Port: 5000, Protocol: "tcp", BindAddr: "0.0.0.0", PID: 44, ProcessName: "dsm"},
			{Port: 8443, Protocol: "tcp", BindAddr: "0.0.0.0", PID: 100, ProcessName: "nginx"},
		},
		Containers: []scanner.ContainerPort{
			{Port: 8081, Protocol: "tcp", ContainerName: "web", Image: "nginx:alpine"},
			{Port: 5000, Protocol: "tcp", ContainerName: "conflict-app", Image: "evil:1"},
		},
		Compose:      []scanner.ComposePort{{Port: 7000, Protocol: "tcp", File: "/compose/a.yml", Service: "web"}},
		RProxy:       []scanner.ReverseProxyRule{{Port: 8443, Protocol: "tcp", HTTPS: true, Host: "nas.example.com", BackendPort: 5001, Description: "测试服务"}},
		Reservations: map[string]store.Reservation{"21000/tcp": {ID: 1, Port: 21000, Protocol: "tcp", Owner: "kyson"}},
		Ranges:       []config.Range{{Lo: 1, Hi: 65535}},
		SelfKeys:     map[string]bool{"8899/tcp": true},
	})

	cases := []struct {
		port   int
		proto  string
		state  string
		source string
	}{
		{8899, "tcp", StateSelf, SrcSelf},
		{8443, "tcp", StateRProxy, SrcRProxy}, // 反代优先于进程
		{5000, "tcp", StateSystem, SrcDict},   // system 优先于 docker
		{6690, "tcp", StateSystem, SrcDict},
		{123, "udp", StateSystem, SrcDict},
		{8081, "tcp", StateInUseDock, SrcDocker}, // docker 优先于 host
		{9000, "tcp", StateInUseHost, SrcProc},
		{7000, "tcp", StateConfigured, SrcCompose},
		{21000, "tcp", StateReserved, SrcReserve},
	}
	for _, c := range cases {
		o, ok := find(obs, c.port, c.proto)
		if !ok {
			t.Errorf("缺少观测 %d/%s", c.port, c.proto)
			continue
		}
		if o.State != c.state || o.Source != c.source {
			t.Errorf("%d/%s = %s(%s)，期望 %s(%s)", c.port, c.proto, o.State, o.Source, c.state, c.source)
		}
	}

	// 冲突叠加：5000 命中系统字典且被容器占用
	o, _ := find(obs, 5000, "tcp")
	if !o.Conflict {
		t.Errorf("5000/tcp 应标记冲突")
	}
	if o.ContainerName != "conflict-app" || o.Image != "evil:1" {
		t.Errorf("冲突时应保留容器信息: %+v", o)
	}

	// 未冲突的系统端口
	o2, _ := find(obs, 6690, "tcp")
	if o2.Conflict {
		t.Errorf("6690/tcp 不应标记冲突")
	}

	// docker 与 host 同时占用时不算冲突
	o3, _ := find(obs, 8081, "tcp")
	if o3.Conflict {
		t.Errorf("容器与进程同时占用不应视为系统端口冲突")
	}
	if o3.ContainerName != "web" {
		t.Errorf("8081 应保留容器名: %+v", o3)
	}

	// self 端口不应被标记冲突
	o4, _ := find(obs, 8899, "tcp")
	if o4.Conflict {
		t.Errorf("self 端口不应标记冲突")
	}
}

func TestRunFreeNotEnumerated(t *testing.T) {
	obs := Run(Input{
		Dict:   testDict(),
		Ranges: []config.Range{{Lo: 1, Hi: 65535}},
	})
	if _, ok := find(obs, 21500, "tcp"); ok {
		t.Fatal("空闲端口不应出现在观测列表中（由查询层枚举）")
	}
	for _, o := range obs {
		if o.State == StateFree {
			t.Fatalf("观测不应包含 free 状态: %+v", o)
		}
	}
}

func TestRunRespectsRanges(t *testing.T) {
	obs := Run(Input{
		Dict:   testDict(),
		Ranges: []config.Range{{Lo: 1000, Hi: 2000}},
	})
	if _, ok := find(obs, 5000, "tcp"); ok {
		t.Fatal("范围外的字典端口不应被纳入")
	}
	if _, ok := find(obs, 6690, "tcp"); ok {
		t.Fatal("范围外的字典端口不应被纳入")
	}
}

func TestRunActiveEvidence(t *testing.T) {
	obs := Run(Input{
		Dict:   testDict(),
		Active: map[string]bool{"9100/tcp": true},
	})
	o, ok := find(obs, 9100, "tcp")
	if !ok || o.State != StateInUseHost || o.Source != SrcActive {
		t.Fatalf("主动扫描结论应记为 in-use(host)/active: %+v ok=%v", o, ok)
	}
}

func TestRunSorted(t *testing.T) {
	obs := Run(Input{
		Dict:      testDict(),
		Listeners: []scanner.Listener{{Port: 9000, Protocol: "tcp"}, {Port: 80, Protocol: "tcp"}},
	})
	for i := 1; i < len(obs); i++ {
		if obs[i-1].Port > obs[i].Port {
			t.Fatalf("结果应按下行端口号排序: %d 在 %d 之前", obs[i-1].Port, obs[i].Port)
		}
	}
}

func TestPriorityMapping(t *testing.T) {
	order := []string{StateSelf, StateRProxy, StateSystem, StateInUseDock, StateInUseHost, StateConfigured, StateReserved, StateFree}
	for i := 0; i < len(order)-1; i++ {
		if Priority(order[i]) >= Priority(order[i+1]) {
			t.Errorf("优先级顺序错误: %s(%d) 应高于 %s(%d)", order[i], Priority(order[i]), order[i+1], Priority(order[i+1]))
		}
	}
}

func TestCounts(t *testing.T) {
	obs := []store.Observation{
		{State: StateSystem},
		{State: StateSystem, Conflict: true},
		{State: StateInUseDock},
		{State: StateSelf},
	}
	c := Counts(obs)
	if c[StateSystem] != 2 || c[StateInUseDock] != 1 || c[StateSelf] != 1 {
		t.Fatalf("计数不符: %+v", c)
	}
	if c["conflict"] != 1 {
		t.Fatalf("冲突计数不符: %+v", c)
	}
}
