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
- DNS split routing 使用以下路径之一：
  `resolvectl` / `systemd-resolved`、已有且 active 并监听 `127.0.0.1:53` 的
  `dnsmasq.service`，或
  `NetworkManager + dnsmasq`
- 若目标机没有 `resolvectl` 且没有上述宿主 stub `dnsmasq.service`，需要
  `NetworkManager` 可用；若缺少 `dnsmasq`，需要已预装 `dnsmasq` 或配置可用的内网软件源
- `/dev/kvm` 可用
- `/data/cubelet` 位于 XFS 文件系统
- 内网 DNS 可用，且 `/etc/resolv.conf` 存在

安装前建议按 `.env` 中实际配置检查端口冲突。默认端口包括：

```bash
ss -lntup | grep -E ':3306|:6379|:3000|:8089|:19090|:12088|:53' || true
```

如果目标机已有 MySQL、Redis 或 Web 服务，建议使用空闲端口覆盖
`CUBE_SANDBOX_MYSQL_PORT`、`CUBE_SANDBOX_REDIS_PORT`、`CUBE_API_BIND`、
`CUBEMASTER_ADDR`、`CUBE_PROXY_HTTP_PORT`、`CUBE_PROXY_HTTPS_PORT` 或
`WEB_UI_HOST_PORT`，避免 Docker 端口映射冲突。

## 上传并解压

```bash
mkdir -p <work-dir>
cd <work-dir>

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

内网 all-in-one 控制节点建议至少确认以下配置。多数场景可保留 `env.example` 默认值；
只有在端口冲突、外部访问或网络环境不一致时再覆盖：

```bash
ONE_CLICK_TARGET_ARCH=arm64
ONE_CLICK_DEPLOY_ROLE=control
ONE_CLICK_RUN_QUICKCHECK=1
ONE_CLICK_ENABLE_TENCENT_DOCKER_MIRROR=0

CUBEMASTER_ADDR=127.0.0.1:8089
CUBE_API_BIND=0.0.0.0:3000

NETWORK_AGENT_HEALTH_ADDR=127.0.0.1:19090
NETWORK_AGENT_READY_TIMEOUT=120

CUBE_SANDBOX_MYSQL_PORT=3306
CUBE_SANDBOX_REDIS_PORT=6379

CUBE_PROXY_HTTP_PORT=80
CUBE_PROXY_HTTPS_PORT=443

WEB_UI_HOST_PORT=12088
CUBEMASTER_METRIC_LOOP=0
```

如果当前 Docker 不支持 `host.docker.internal:host-gateway`，或者 WebUI 反代需要直接访问宿主
CubeAPI，可额外设置为目标机实际可从容器内访问的宿主地址和 CubeAPI 端口：

```bash
WEB_UI_UPSTREAM=http://<docker-bridge-gateway-or-host-ip>:3000
```

如果 Docker 支持 `host-gateway`，也可以使用：

```bash
WEB_UI_UPSTREAM=http://host.docker.internal:3000
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

然后确认可用的 DNS split routing 路径：

```bash
command -v resolvectl || true
systemctl status dnsmasq --no-pager -l || true
systemctl status NetworkManager --no-pager -l || true
```

新版本安装器的 DNS fallback 顺序为：

1. 若存在 `resolvectl`，使用 `systemd-resolved` 和专用 dummy link `cube-dns0`
2. 若没有 `resolvectl`，但宿主已有 active 且监听 `127.0.0.1:53` 的
   `dnsmasq.service`，复用该服务，新增 `/etc/dnsmasq.d/90-cubeproxy-cube-app.conf`，
   让它额外监听 `169.254.254.53` 并把 `cube.app` 转发到 one-click CoreDNS
3. 若上述两者都不可用，回退到 `NetworkManager + dnsmasq`

这可以避免目标机已有 `dnsmasq.service` 绑定 `127.0.0.1:53` 时，NetworkManager
dnsmasq 插件再次抢占同一端口导致 `cube-sandbox-dns.service` 超时。

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
- 如果目标机启用了 SELinux 且存在 `restorecon`，自动对安装目录、systemd units 和
  `/usr/local/bin` 下 one-click 命令执行 relabel
- 在 `ONE_CLICK_RUN_QUICKCHECK=1` 时执行 quickcheck

新版本安装器会自动处理 SELinux relabel。若使用旧 release 包，或者安装后仍出现 systemd
`203/EXEC`，可手动执行：

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
curl -fsS 127.0.0.1:3000/health
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
dnsmasq`、`systemctl restart NetworkManager`，还是等待 `169.254.254.53:53`。

如果目标机已有宿主 `dnsmasq.service`，且日志出现：

```text
failed to create listening socket for 127.0.0.1: Address already in use
```

新版本安装器会自动走 `system-dnsmasq` fallback，期望状态为：

```bash
cat /usr/local/services/cubetoolbox/coredns/host-dns-mode
cat /etc/dnsmasq.d/90-cubeproxy-cube-app.conf
ss -lnup '( sport = :53 )' | grep -E '127.0.0.1|169.254.254.53|127.0.0.54'
```

其中 `host-dns-mode` 应为 `system-dnsmasq`，`dnsmasq` 应监听
`127.0.0.1:53` 和 `169.254.254.53:53`，CoreDNS 应监听 `127.0.0.54:53`。

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

如果 Docker 不支持，改 `.env`，将地址替换为目标机实际可从容器内访问的宿主地址：

```bash
WEB_UI_UPSTREAM=http://<docker-bridge-gateway-or-host-ip>:3000
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
ss -lntup | grep -E ':3306|:<mysql-host-port>' || true
docker ps -a | grep cube-sandbox-mysql || true
journalctl -u docker -b --no-pager -n 120
```

若默认 `3306` 冲突，在 `.env` 中换成空闲端口：

```bash
CUBE_SANDBOX_MYSQL_PORT=<mysql-host-port>
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

### 1. 停止 one-click systemd 栈

```bash
systemctl stop cube-sandbox-control.target cube-sandbox-compute.target 2>/dev/null || true
systemctl stop cube-sandbox-seed-cubemaster-metrics.timer 2>/dev/null || true
```

### 2. 执行包内卸载脚本

优先使用 release 目录中的 `down.sh`，它会停止 systemd units、回滚 one-click DNS 配置、
删除 one-click 管理的容器和默认数据卷：

```bash
cd <one-click-release-dir>
bash down.sh
```

如果 release 目录已经删除，可直接使用已安装目录中的脚本清理 systemd units：

```bash
/usr/local/services/cubetoolbox/scripts/systemd/remove-units.sh 2>/dev/null || true
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
```

### 3. 检查并清理残留容器和卷

如需手动清理 Docker 容器：

```bash
docker rm -f cube-sandbox-mysql cube-sandbox-redis cube-proxy cube-proxy-coredns cube-webui 2>/dev/null || true
```

如确认不需要保留 MySQL/Redis 数据，再删除默认 volumes：

```bash
docker volume rm cube-sandbox-mysql-data cube-sandbox-redis-data 2>/dev/null || true
```

如果 `.env` 中自定义过容器名或卷名，需要按实际值替换上面的名称。

### 4. 检查挂载点并处理 umount

`cubelet` 使用 mount namespace 和 bind mount。异常退出或强制停止后，删除
`/usr/local/services/cubetoolbox` 时可能遇到：

```text
Device or resource busy
```

先查看相关挂载：

```bash
findmnt -R /usr/local/services/cubetoolbox 2>/dev/null || true
findmnt -R /data/cubelet 2>/dev/null || true
```

常见残留路径包括：

```text
/usr/local/services/cubetoolbox/cubeletmnt
/usr/local/services/cubetoolbox/cubeletmnt/mnt
```

优先按从内到外的顺序正常卸载：

```bash
umount /usr/local/services/cubetoolbox/cubeletmnt/mnt 2>/dev/null || true
umount /usr/local/services/cubetoolbox/cubeletmnt 2>/dev/null || true
```

如果仍提示 busy，先确认是否有遗留 cubelet/cube-shim 进程：

```bash
ps -ef | grep -E 'cubelet|containerd-shim-cube|cube-runtime|firecracker|qemu' | grep -v grep || true
```

确认不再需要保留这些进程后再停止对应服务或终止进程。若仍无法卸载，可使用懒卸载作为最后清理手段：

```bash
umount -l /usr/local/services/cubetoolbox/cubeletmnt/mnt 2>/dev/null || true
umount -l /usr/local/services/cubetoolbox/cubeletmnt 2>/dev/null || true
```

不要对 `/data/cubelet` 本身执行 `umount -l`，除非确认该 XFS 挂载只用于本次 one-click
验证且不会影响其他服务。

### 5. 删除安装目录

确认 systemd、容器、卷和挂载都已清理后，再删除安装目录：

```bash
rm -rf /usr/local/services/cubetoolbox
systemctl daemon-reload
systemctl reset-failed 2>/dev/null || true
```

删除 Docker volumes、`/usr/local/services/cubetoolbox` 或 `/data/cubelet` 下的数据会清理运行数据，
执行前需要确认不再需要保留。
