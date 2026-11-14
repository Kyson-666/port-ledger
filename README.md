# PortLedger（端口台账）

群晖 Docker 端口盘点与用途管理工具。扫描已被占用与空闲的端口，为端口登记用途，并自动标注群晖系统保留端口。

- 单二进制 + 内嵌前端 + 纯 Go SQLite，**零外部依赖**
- 多阶段构建 → **scratch**，镜像 ≈ 12 MB，支持 `linux/amd64` 与 `linux/arm64`
- 全部数据源**只读**：不 kill 进程、不改宿主配置、不写宿主文件系统

> 设计依据：`deliverables/port-ledger-design-2026-11-14.md`（技术定稿）、
> `deliverables/port-ledger-ui-interaction-design-2026-11-14.md`（页面与交互）、
> `deliverables/port-ledger-design-system-2026-11-14.md`（配色与组件唯一来源）。

---

## 快速开始（群晖 DSM 7.x / Container Manager）

```bash
mkdir -p /volume1/docker/port-ledger/{data,logs,backups}
# 将 docker-compose.yml 放到 /volume1/docker/port-ledger/ 后：
sudo docker compose up -d
```

访问 `http://<群晖IP>:8899/`（宿主端口按 compose 中的映射）。

若启用了 DSM 防火墙，请在「控制面板 → 安全 → 防火墙」放行所映射的宿主端口。
容器内固定监听 **443**，与宿主 443 不冲突（bridged 网络）。

---

## 环境变量

| 变量 | 默认 | 说明 |
|---|---|---|
| `ADMIN_PORT` | `443` | 容器内监听端口 |
| `TLS_SELF_SIGNED` | `false` | `true` 时自签证书并以 HTTPS 提供服务（需重启） |
| `TLS_CERT` / `TLS_KEY` | — | 指定自定义证书（优先于自签） |
| `DATA_DIR` | `/data` | 数据根目录（库、日志、备份均在其下） |
| `SCAN_HOST_PROC` | `true` | 解析宿主监听表（需只读挂载 `/proc`） |
| `SCAN_DOCKER_SOCKET` | `true` | 读取容器端口映射（需只读挂载 `docker.sock`） |
| `SCAN_COMPOSE_DIR` | `/compose` | compose 声明目录（只读挂载） |
| `SCAN_RANGES` | `1-65535` | 扫描范围，支持 `1-1024,8000-8100` |
| `SCAN_ON_START` | `true` | 空库首启自动全量扫描 |
| `SCAN_INTERVAL` | `300` | 周期扫描秒数，`0` = 仅手动 |
| `PROC_ROOT` | `/host/proc` | 宿主 `/proc` 的容器内挂载点 |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker 套接字 |
| `SELF_CONTAINER_NAME` | `port-ledger` | 用于识别"本工具"端口 |
| `SCAN_TARGET_HOST` | 空（禁用） | 主动扫描目标，见下方限制说明 |
| `LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |

可在管理台「设置」页修改部分项（写入库内并立即生效，除需重启项）。

---

## 持久化路径

```
/volume1/docker/port-ledger/
├── data/port-ledger.db            # SQLite：命名 / 保留 / 观测 / 审计 / 字典 / 设置
├── logs/app.log                   # 应用日志（JSON 行，10MB × 5 轮转）
├── logs/scan/YYYY-MM-DD.log       # 扫描明细（保留 30 天）
└── backups/port-ledger-*.json     # 台账导出
```

容器内挂载点：`/data`、`/data/logs`、`/data/backups`（读写）；`/proc`、`/var/run/docker.sock`、`/volume1/docker`（只读）。

---

## 端口状态

| 状态 | 徽标 | 含义 |
|---|---|---|
| `self` | 本工具 | PortLedger 自身所占端口 |
| `system` | 系统 | 命中群晖系统端口字典 |
| `in-use(docker)` | 容器 | 被容器发布映射占用 |
| `in-use(host)` | 进程 | 被宿主进程监听占用 |
| `configured` | 已声明 | compose 已声明但尚未启动 |
| `reserved` | 已保留 | 用户为待部署项目预留 |
| `free` | 空闲 | 可分配 |

判定优先级（高→低）：`self > system > in-use(docker) > in-use(host) > configured > reserved`。

**冲突叠加**：命中共晖系统字典、同时又被容器/进程/compose 实际占用时，额外显示「冲突」标记（例如 DSM 网页端口被容器抢占）。

---

## 已知限制

1. **主动扫描（`SCAN_TARGET_HOST`）默认禁用**。容器以 bridge 网络运行时，容器内的 `127.0.0.1` 指向容器自身而非宿主，因此主动 TCP 扫描无法发现宿主端口。宿主端口的完整视图由「宿主监听表 + Docker API + compose 声明」三个只读数据源提供。
2. **宿主监听表依赖 `/proc` 挂载**。部分机型或安全策略下若无法只读挂载 `/proc`，该数据源会降级并在「数据源健康」中显式标记，列表不伪造数据。
3. **群晖系统字典为版本化内置数据**。来源为 Synology 官方知识库，冲突项以官方为准；可用平台「系统字典」页覆盖名称或补充自定义条目。
4. **进程名反查**依赖遍历 `/host/proc/<pid>/fd`，需要读取权限；失败时仅缺失进程名，不影响端口占用判定。

---

## 开发

```bash
make vet test        # 静态检查 + 单元/集成测试
make frontend-check  # 前端语法与 import 可达性校验
make build-all       # 交叉编译 linux/amd64 + arm64
bash scripts/e2e-smoke.sh ./bin/port-ledger-linux-amd64   # 端到端冒烟
make image           # 多架构镜像（需 docker buildx）
```

Go 1.22+ 即可；前端为原生 ES Module，无构建步骤，`go:embed` 直接打包。

---

## 安全

- 无认证（设计决策，面向内网）。请勿直接暴露于公网；如需外网访问，建议经群晖反向代理终结 TLS 并叠加访问控制。
- `docker.sock` 与 `/proc` 均为**只读**挂载；本工具不发起任何写操作。
- 容器以 root 运行（绑定 443、读取 `docker.sock` 所需），数据仅落在 `/data`。

## 许可证

MIT
