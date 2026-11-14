package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// DockerScanner 通过只读挂载的 docker.sock 读取容器端口发布映射。
type DockerScanner struct {
	Host              string // unix:///var/run/docker.sock
	SelfContainerID   string
	SelfContainerName string
}

// Name 返回数据源名称。
func (d *DockerScanner) Name() string { return "docker" }

type dockerContainer struct {
	ID    string   `json:"Id"`
	Names []string `json:"Names"`
	Image string   `json:"Image"`
	Ports []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// Scan 返回全部容器端口映射。
func (d *DockerScanner) Scan() ([]ContainerPort, []Health, error) {
	client, err := d.client()
	if err != nil {
		return nil, []Health{{Name: "docker", Enabled: true, OK: false, Error: err.Error(), At: time.Now().UTC()}}, err
	}
	req, _ := http.NewRequest(http.MethodGet, "http://docker/containers/json?all=true", nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, []Health{{Name: "docker", Enabled: true, OK: false, Error: err.Error(), At: time.Now().UTC()}}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("docker API 返回 %d", resp.StatusCode)
		return nil, []Health{{Name: "docker", Enabled: true, OK: false, Error: err.Error(), At: time.Now().UTC()}}, err
	}
	var list []dockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, []Health{{Name: "docker", Enabled: true, OK: false, Error: err.Error(), At: time.Now().UTC()}}, err
	}

	var out []ContainerPort
	for _, c := range list {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		isSelf := d.isSelf(c.ID, name)
		for _, p := range c.Ports {
			if p.PublicPort == 0 || p.Type == "" {
				continue // 仅容器内部端口，未发布到宿主
			}
			out = append(out, ContainerPort{
				Port:          p.PublicPort,
				Protocol:      p.Type,
				BindAddr:      p.IP,
				ContainerID:   shortID(c.ID),
				ContainerName: name,
				Image:         c.Image,
				IsSelf:        isSelf,
			})
		}
	}
	return out, []Health{{Name: "docker", Enabled: true, OK: true, Count: len(out), At: time.Now().UTC()}}, nil
}

func (d *DockerScanner) isSelf(id, name string) bool {
	if d.SelfContainerName != "" && name == d.SelfContainerName {
		return true
	}
	if d.SelfContainerID != "" && strings.HasPrefix(id, d.SelfContainerID) {
		return true
	}
	return false
}

func (d *DockerScanner) client() (*http.Client, error) {
	sock := strings.TrimPrefix(d.Host, "unix://")
	if sock == d.Host { // 非 unix scheme
		return nil, fmt.Errorf("仅支持 unix:// 形式的 DOCKER_HOST，收到 %q", d.Host)
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var nd net.Dialer
			return nd.DialContext(ctx, "unix", sock)
		},
		DisableKeepAlives: true,
	}
	return &http.Client{Transport: tr, Timeout: 10 * time.Second}, nil
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// DetectSelfContainerID 尽力识别本进程所在的容器 ID（用于标记 self 状态）。
// 依次尝试：环境变量 → /proc/self/cgroup → /etc/hostname。
func DetectSelfContainerID() string {
	if v := strings.TrimSpace(os.Getenv("SELF_CONTAINER_ID")); v != "" {
		return v
	}
	if b, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			// 形如 0::/docker/<64hex> 或 .../docker-<64hex>.scope
			if i := strings.LastIndex(line, "docker-"); i >= 0 {
				seg := line[i+len("docker-"):]
				seg = strings.TrimSuffix(seg, ".scope")
				if isHexID(seg) {
					return seg
				}
			}
			if i := strings.LastIndex(line, "/docker/"); i >= 0 {
				seg := strings.TrimSpace(line[i+len("/docker/"):])
				if isHexID(seg) {
					return seg
				}
			}
		}
	}
	if b, err := os.ReadFile("/etc/hostname"); err == nil {
		h := strings.TrimSpace(string(b))
		if isHexID(h) {
			return h
		}
	}
	return ""
}

func isHexID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
