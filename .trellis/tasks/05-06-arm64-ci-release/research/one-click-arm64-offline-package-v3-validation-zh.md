# one-click ARM64 离线安装包验证记录（v3）

## 基本信息

- 验证日期：2026-05-30
- 本地工作区：`/home/lyq/Projects/Micro-VM/CubeSandbox`
- 基准提交：`a6f78b8 Merge branch 'feature/arm64-adaptation'`
- 远端机器：`root@192.168.25.61`
- 远端架构：`aarch64`
- 验证目录：`/opt/cubesandbox-oneclick-fix-a6f78b8`

## 本轮修复范围

本轮目标是生成一个在 ARM64 远端机器可直接运行的 one-click 安装包，并把运行依赖的容器镜像一起打进包内。

主要修复点：

- systemd 启动脚本按 `ONE_CLICK_TARGET_ARCH` 选择 ARM64 可用镜像默认值。
- `install.sh` 安装时加载 `sandbox-package/docker-images/*.tar`。
- release bundle 构建支持 `ONE_CLICK_INCLUDE_DOCKER_IMAGES=1` 和 `ONE_CLICK_BUILD_DOCKER_IMAGE_TARS=1`。
- `cubeproxy/.image-build-stamp` 在使用已有镜像 tar 打包时也会写入，避免安装后重复构建 cube-proxy 镜像。
- `network-agent-start.sh` 传入 `--health-listen "${NETWORK_AGENT_HEALTH_ADDR}"`。
- `network-agent-postcheck.sh` 使用 `NETWORK_AGENT_READY_TIMEOUT`，并检查 `/healthz` 与 `/readyz`。
- `cube-proxy-start.sh` 同步 mkcert 可执行性检查；ARM64 上 x86 mkcert 不可运行时使用 `openssl` 生成本地证书。
- `cube-proxy-start.sh` 渲染 `nginx.conf.template`，支持 `CUBE_PROXY_HTTP_PORT` 和 `CUBE_PROXY_HTTPS_PORT`。
- cube-proxy/WebUI postcheck 改为容器状态和 HTTP 检查，避免已启动容器被端口探测误杀后进入重启自冲突。
- network-agent、cube-proxy、WebUI systemd unit 增加 `TimeoutStartSec`，覆盖 ARM64 初始化和容器启动窗口。

## 构建过程

完整 v2 构建在拉取 Docker Hub 镜像阶段失败：

```text
Error response from daemon: Get "https://registry-1.docker.io/v2/": proxyconnect tcp: dial tcp 127.0.0.1:7890: connect: connection refused
```

原因是为了释放 `127.0.0.1:19090` 给 network-agent，临时停止了远端 `mihomo.service`，但 Docker daemon 仍配置了 `127.0.0.1:7890` 代理。

v3 构建复用了 v2 已完成的 ARM64 预编译二进制，并复用上一轮已成功保存的 ARM64 镜像 tar，使用 `ONE_CLICK_INCLUDE_DOCKER_IMAGES=1` 重新打包，未再访问 Docker Hub。

构建日志：

```text
/opt/cubesandbox-oneclick-fix-a6f78b8/logs/build-a6f78b8-arm64-oneclick-fix-v3.log
```

安装日志：

```text
/opt/cubesandbox-oneclick-fix-a6f78b8/logs/install-a6f78b8-arm64-oneclick-fix-v3.log
```

## 最终安装包

```text
/opt/cubesandbox-oneclick-fix-a6f78b8/src-v3/deploy/one-click/dist/cube-sandbox-one-click-a6f78b8-arm64-oneclick-fix-v3.tar.gz
```

大小与校验：

```text
size:   757M
sha256: a218fd8ab4b3e07d32257545da5a53f6be335a262d1631129f774a1dbb0ee7d6
```

包内已包含镜像 tar：

```text
coredns-1.14.2.tar                         71922688
cube-proxy-one-click.tar                   411387392
mysql-8.0.tar                              813694976
openresty-1.21.4.1-6-alpine-fat.tar       406451712
redis-7-alpine.tar                         40317440
```

安装后 `/usr/local/services/cubetoolbox/docker-images` 中镜像校验：

```text
13d507650b6ec9e4f5735e14ac83c64fd60f216d535fc74207a4d43af49b732c  mysql-8.0.tar
b5da06a321340c8b1ca9f00040b64951fee186b8f7a69b1eb4b419206faace45  cube-proxy-one-click.tar
d4ad0087a50e384e7823ab9e08b77aba3e072e9a76429964b90f7582332c3aab  coredns-1.14.2.tar
f0995cef6fad6d41066c613127a4a2f2547feb0226e57f667c6111a0add15c51  redis-7-alpine.tar
f230fd9c3cfe4f8c1d1416357a996c83667f7cf2b7a596ce63eb6d5ddfc928e9  openresty-1.21.4.1-6-alpine-fat.tar
```

## 安装验证结果

安装命令：

```bash
cd /opt/cubesandbox-oneclick-fix-a6f78b8/install-v3/cube-sandbox-one-click-a6f78b8-arm64-oneclick-fix-v3
bash ./install.sh
```

quickcheck 结果：

```text
[quickcheck] role=control
[quickcheck] cubemaster=127.0.0.1:18089
[quickcheck] network-agent-health=127.0.0.1:19090
[quickcheck] cube-api-health=127.0.0.1:13000
[quickcheck] check systemd units
[quickcheck] check container runtime state
[quickcheck] 1/5 check network-agent healthz
[quickcheck] 2/5 check network-agent readyz
[quickcheck] 3/5 check cubemaster /notify/health
[quickcheck] 4/5 check cube-api /health
[quickcheck] 5/5 check essential sockets and config
[quickcheck] OK
[one-click] install complete (role=control)
```

systemd 状态均为 active：

```text
cube-sandbox-control.target
cube-sandbox-network-agent.service
cube-sandbox-cubelet.service
cube-sandbox-mysql.service
cube-sandbox-redis.service
cube-sandbox-coredns.service
cube-sandbox-cubemaster.service
cube-sandbox-cube-api.service
cube-sandbox-cube-proxy.service
cube-sandbox-webui.service
```

容器状态：

```text
cube-webui-v3-a6f78b8            openresty/openresty:1.21.4.1-6-alpine-fat   Up (healthy)
cube-proxy-v3-a6f78b8            cube-proxy:one-click                        Up
cube-sandbox-redis-v3-a6f78b8    redis:7-alpine                              Up (healthy)
cube-sandbox-mysql-v3-a6f78b8    mysql:8.0                                   Up (healthy)
cube-proxy-coredns-v3-a6f78b8    coredns/coredns:1.14.2                      Up
```

HTTP 探测结果：

```text
http://127.0.0.1:19090/healthz           -> ok
http://127.0.0.1:19090/readyz            -> ready
http://127.0.0.1:18089/notify/health     -> ret_code=200
http://127.0.0.1:13000/health            -> status=ok
http://127.0.0.1:12090/                  -> OK
http://127.0.0.1:12090/cubeapi/v1/health -> status=ok
```

监听端口：

```text
127.0.0.1:19090   network-agent
*:18089           cubemaster
0.0.0.0:13000     cube-api
0.0.0.0:10081     cube-proxy HTTP
0.0.0.0:10444     cube-proxy HTTPS
0.0.0.0:12090     WebUI
0.0.0.0:23306     MySQL
0.0.0.0:26379     Redis
127.0.0.55:53     CoreDNS
```

## 注意事项

- 本次验证临时停止了远端 `mihomo.service`，使 network-agent 可以使用默认 `127.0.0.1:19090`。
- v2 构建失败不是代码编译失败，而是 Docker daemon 代理仍指向已停止的 `127.0.0.1:7890`。
- 当前验证覆盖了 one-click control role 的安装、服务启动、quickcheck 和 HTTP 健康检查；尚未在本轮继续创建 sandbox/启动 guest workload。
- Cubelet 日志仍显示 `MetaServerEndpoint:"127.0.0.1:8089"`，但本轮 quickcheck 未依赖该路径；后续若验证节点注册或 sandbox 创建，需要继续修正 Cubelet dynamic config 的 Cubemaster 地址。

## 结论

v3 one-click ARM64 离线安装包已经在 `192.168.25.61` 上完成构建、安装和 quickcheck 验证。该包包含 MySQL、Redis、CoreDNS、OpenResty WebUI、cube-proxy 所需镜像 tar，安装过程不需要重新从 Docker Hub 拉取这些镜像。
