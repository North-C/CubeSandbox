# CubeSandbox one-click optimized Arm64 离线安装指南

本文记录基于 `one-click optimized` Arm64 release 包在内网环境中的离线安装流程。

## Release 信息

已验证的 release 包：

```text
cube-sandbox-one-click-90a87ac-arm64-optimized.tar.gz
SHA256: c48620d458aa0b702c7b5b321a5b5c880a9572a7a743a81fa2a5f9cf10f974cc
revision: 90a87ac-arm64-optimized
guest_image_version: 20260530-114120
```

包内顶层结构包含：

```text
README.md
VERSION.txt
assets/kernel-artifacts/cube-kernel-scf.zip
assets/package/sandbox-package.tar.gz
env.example
install.sh
install-compute.sh
online-install.sh
down.sh
smoke.sh
lib/common.sh
```

安装脚本会自动解压 `assets/package/sandbox-package.tar.gz`，安装组件到
`/usr/local/services/cubetoolbox`，并加载包内 Docker 镜像。目标机不需要访问公网拉取
MySQL、Redis、CoreDNS、OpenResty 或 cube-proxy 镜像。

## 目标机前置条件

目标机建议使用 openEuler Arm64 环境，并满足：

```bash
uname -m
docker version
test -e /dev/kvm && echo kvm-ok
findmnt -T /data/cubelet -o TARGET,FSTYPE,OPTIONS
getenforce 2>/dev/null || true
```

要求：

- `uname -m` 应为 `aarch64`
- Docker engine 已安装并可启动
- `rg`、`awk`、`systemctl`、`ip`、`ss` 等基础命令可用
- 若目标机没有 `resolvectl`，需要 `NetworkManager` 可用；若缺少 `dnsmasq`，需要已预装
  `dnsmasq` 或配置可用的内网软件源
- `/dev/kvm` 可用
- `/data/cubelet` 位于 XFS 文件系统
- 内网 DNS 可用，且 `/etc/resolv.conf` 存在

安装前建议检查端口冲突：

```bash
ss -lntup | grep -E ':3306|:23306|:6379|:26379|:13000|:18089|:19090|:12088|:53' || true
```

如果目标机已有 MySQL、Redis 或 Web 服务，建议使用非默认端口，避免 Docker 端口映射冲突。

## 上传并解压

```bash
mkdir -p /opt/cubesandbox-oneclick
cd /opt/cubesandbox-oneclick

sha256sum cube-sandbox-one-click-90a87ac-arm64-optimized.tar.gz
tar -xzf cube-sandbox-one-click-90a87ac-arm64-optimized.tar.gz
cd cube-sandbox-one-click-90a87ac-arm64-optimized

cat VERSION.txt
```

确认 SHA256 与 release 信息一致。

## 准备 .env

不要手写完整 `.env`，推荐从包内模板复制后修改：

```bash
cp env.example .env
```

内网 all-in-one 控制节点推荐修改以下配置：

```bash
ONE_CLICK_TARGET_ARCH=arm64
ONE_CLICK_DEPLOY_ROLE=control
ONE_CLICK_RUN_QUICKCHECK=1
ONE_CLICK_ENABLE_TENCENT_DOCKER_MIRROR=0

CUBEMASTER_ADDR=127.0.0.1:18089
CUBE_API_BIND=0.0.0.0:13000

NETWORK_AGENT_HEALTH_ADDR=127.0.0.1:19090
NETWORK_AGENT_READY_TIMEOUT=240

CUBE_SANDBOX_MYSQL_PORT=23306
CUBE_SANDBOX_REDIS_PORT=26379

CUBE_PROXY_HTTP_PORT=10081
CUBE_PROXY_HTTPS_PORT=10444

WEB_UI_HOST_PORT=12088
CUBEMASTER_METRIC_LOOP=0
```

如果当前 Docker 不支持 `host.docker.internal:host-gateway`，或者 WebUI 反代需要直接访问宿主
CubeAPI，可额外设置：

```bash
WEB_UI_UPSTREAM=http://172.17.0.1:13000
```

如果 Docker 支持 `host-gateway`，也可以使用：

```bash
WEB_UI_UPSTREAM=http://host.docker.internal:13000
```

可以通过 Docker 版本判断是否支持 `host-gateway`。若需要实际验证，请使用目标机本地已有镜像，
避免在内网环境触发公网拉取：

```bash
docker version
docker run --rm --add-host host.docker.internal:host-gateway <local-image> true
```

## DNS 准备

one-click 会通过 `cube-sandbox-dns.service` 配置宿主机 `cube.app` split DNS。内网环境中，
请先确认 `/etc/resolv.conf` 存在且能解析内网依赖：

```bash
cat /etc/resolv.conf
```

如目标机没有有效 resolv.conf，可按内网 DNS 地址创建：

```bash
cat > /etc/resolv.conf <<'EOF'
nameserver <内网DNS地址>
options timeout:2 attempts:2
EOF
```

然后确认 NetworkManager 或 systemd-resolved 路径可用：

```bash
command -v resolvectl || true
systemctl status NetworkManager --no-pager -l || true
```

## 执行安装

```bash
bash install.sh
```

`install.sh` 会：

- 读取当前目录 `.env`
- 解压 `assets/package/sandbox-package.tar.gz`
- 安装组件到 `/usr/local/services/cubetoolbox`
- 将 `.env` 复制为 `/usr/local/services/cubetoolbox/.one-click.env`
- 加载包内 Docker 镜像
- 安装并启动 systemd units
- 在 `ONE_CLICK_RUN_QUICKCHECK=1` 时执行 quickcheck

如果目标机启用了 SELinux enforcing，且安装后出现 systemd `203/EXEC`，执行：

```bash
restorecon -Rv /usr/local/services/cubetoolbox /etc/systemd/system/cube-sandbox-*.service
systemctl daemon-reload
systemctl restart cube-sandbox-control.target
```

## 验证

基础检查：

```bash
systemctl --failed --no-pager
systemctl status cube-sandbox-control.target --no-pager -l
```

核心服务状态：

```bash
systemctl show \
  cube-sandbox-network-agent.service \
  cube-sandbox-cube-api.service \
  cube-sandbox-cubemaster.service \
  cube-sandbox-cubelet.service \
  cube-sandbox-mysql.service \
  cube-sandbox-redis.service \
  cube-sandbox-coredns.service \
  cube-sandbox-cube-proxy.service \
  cube-sandbox-dns.service \
  cube-sandbox-webui.service \
  -p Id -p ActiveState -p SubState -p Result -p ExecMainStatus --no-pager
```

HTTP 健康检查：

```bash
curl -fsS 127.0.0.1:19090/healthz
curl -fsS 127.0.0.1:19090/readyz
curl -fsS 127.0.0.1:13000/health
```

期望输出：

```text
ok
ready
{"status":"ok","sandboxes":0}
```

## 常见问题

### cube-sandbox-dns.service 卡住或超时

查看：

```bash
journalctl -u cube-sandbox-dns.service -b --no-pager -n 160 -o short-precise
journalctl -u NetworkManager -b --no-pager -n 160 -o short-precise
ss -lntup | grep ':53' || true
ip -d link show cube-dns0 2>/dev/null || true
```

如果日志显示 `killed, status=15/TERM`，通常是 `TimeoutStartSec` 到期后 systemd 杀掉
`dns-host-route-up.sh`。需要继续确认脚本卡在 `resolvectl`、`systemctl restart
NetworkManager`，还是等待 `169.254.254.53:53`。

### WebUI host-gateway 不支持

错误示例：

```text
invalid argument "host.docker.internal:host-gateway"
```

处理：

```bash
docker version
docker run --rm --add-host host.docker.internal:host-gateway <local-image> true
```

如果 Docker 不支持，改 `.env`：

```bash
WEB_UI_UPSTREAM=http://172.17.0.1:13000
```

然后重启：

```bash
systemctl restart cube-sandbox-webui.service
```

### MySQL 端口映射失败

错误通常出现在 Docker 端口映射阶段：

```text
driver failed programming external connectivity on endpoint cube-sandbox-mysql
```

查看：

```bash
ss -lntup | grep -E ':3306|:23306' || true
docker ps -a | grep cube-sandbox-mysql || true
journalctl -u docker -b --no-pager -n 120
```

若默认 `3306` 冲突，推荐在 `.env` 中使用：

```bash
CUBE_SANDBOX_MYSQL_PORT=23306
```

修改后重新启动：

```bash
docker rm -f cube-sandbox-mysql 2>/dev/null || true
systemctl restart cube-sandbox-mysql.service
systemctl restart cube-sandbox-cubemaster.service
```

### seed cubemaster metrics 失败

该服务会向 Redis 写入本地 CubeMaster 指标，不是容器型服务。查看：

```bash
journalctl -u cube-sandbox-seed-cubemaster-metrics.service -b --no-pager -n 120 -o short-precise
systemctl status cube-sandbox-redis.service --no-pager -l
docker ps -a | grep redis || true
```

如果 Redis 未启动，先修复 Redis。若不需要周期写入指标，可保持：

```bash
CUBEMASTER_METRIC_LOOP=0
```

## 卸载

停止服务：

```bash
systemctl stop cube-sandbox-control.target cube-sandbox-compute.target 2>/dev/null || true
```

执行包内卸载脚本：

```bash
cd /opt/cubesandbox-oneclick/cube-sandbox-one-click-90a87ac-arm64-optimized
bash down.sh
```

如需清理 Docker 容器：

```bash
docker rm -f cube-sandbox-mysql cube-sandbox-redis cube-proxy cube-proxy-coredns cube-webui 2>/dev/null || true
```

删除 Docker volumes 或 `/usr/local/services/cubetoolbox` 会清理数据，执行前需要确认不再需要保留。

## 远端验证记录

验证时间：2026-06-08

`root@192.168.25.65`：

- 系统架构：`aarch64`
- Docker：`25.0.5`
- `/data/cubelet`：XFS
- release 包 SHA256 与本文记录一致
- 包内存在 `env.example`、`install.sh`、`VERSION.txt`
- 已验证 `cp env.example .env` 后修改端口、`CUBE_API_BIND`、`WEB_UI_UPSTREAM` 等变量的流程
- 已确认包内安装后包含离线 Docker 镜像 tar：
  `mysql-8.0.tar`、`redis-7-alpine.tar`、`coredns-1.14.2.tar`、
  `openresty-1.21.4.1-6-alpine-fat.tar`、`cube-proxy-one-click.tar`

`root@192.168.25.61`：

- 使用同一 optimized release 包完成安装
- `systemctl --failed --no-pager` 为 0
- `network-agent`、`cube-api`、`cubemaster`、`cubelet`、`mysql`、`redis`、
  `coredns`、`cube-proxy`、`dns`、`webui` 均为 active
- 健康检查返回：
  - `127.0.0.1:19090/healthz`: `ok`
  - `127.0.0.1:19090/readyz`: `ready`
  - `127.0.0.1:13000/health`: `{"status":"ok","sandboxes":0}`
