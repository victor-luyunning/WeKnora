# 与 FMS 共用外部入口部署

本文只说明：服务器已有 FMS、Docker 和 Compose 时，如何部署 WeKnora 项目环境。

目标链路：

```text
外部同一个 FMS 地址
        │
        └── /zswek/ ──> FMS frontend ──> host.docker.internal:18083
                                      │       (服务器 Docker 网关：172.17.0.1)
                                      └── WeKnora frontend
                                            └── WeKnora app: app:8080

WeKnora app ──> host.docker.internal:18002
               (服务器 Docker 网关：172.17.0.1)
                    └── FMS backend
```

WeKnora 不新增公网端口，也不占用 FMS 的 `18082`。`18083` 和 `18002` 都是服务器内部桥接端口。
注意：`127.0.0.1` 只对当前容器/当前宿主机回环接口可见，其他容器访问不到；FMS 与 WeKnora
跨容器访问时，必须绑定 Docker host-gateway（本服务器实测为 `172.17.0.1`）。

## 1. 创建 WeKnora 项目环境

第一次部署从当前项目的个人仓库拉取 `fms-weknora` 分支，不要拉取腾讯上游仓库。

当前项目仓库：

```text
git@github.com:victor-luyunning/WeKnora.git
```

服务器项目目录使用 `/srv/weknora`：

```bash
sudo mkdir -p /srv/weknora
sudo chown -R "$USER":"$USER" /srv/weknora

git clone --branch fms-weknora --single-branch \
  git@github.com:victor-luyunning/WeKnora.git \
  /srv/weknora

cd /srv/weknora
cp .env.example .env
chmod 600 .env
```

服务器需要提前配置 GitHub Deploy Key，确认：

```bash
ssh -T git@github.com
```

如果服务器已经有 `/srv/weknora`，不要再次 clone；进入该目录确认当前分支是 `fms-weknora`，并且只在 `.env` 不存在时执行 `cp .env.example .env`。

如果刚刚已经执行了上面的 `cp` 和 `chmod`，可以直接执行下面这一整段。它会从 FMS 生产环境文件读取外部域名和桥接 token，补齐 WeKnora 生产参数，校验 Compose，然后构建并启动当前源码；已有非示例密钥会保留：

仓库已经提供同样逻辑的脚本。服务器拉取最新 `fms-weknora` 后，优先直接执行：

```bash
cd /srv/weknora
git pull --ff-only origin fms-weknora
bash scripts/server-first-deploy.sh
```

下面的完整展开版用于需要逐行查看或排错的情况。

```bash
cd /srv/weknora

FMS_ENV=/srv/fms/app/.env.docker.production
FMS_ORIGIN="$(grep '^CORS_ALLOWED_ORIGINS=' "$FMS_ENV" | head -n1 | cut -d= -f2- | tr -d '\"' | cut -d, -f1)"
FMS_TOKEN="$(grep '^FMS_WEKNORA_BRIDGE_TOKEN=' "$FMS_ENV" | head -n1 | cut -d= -f2- | tr -d '\"' | tr -d '\r')"

if [ -z "$FMS_ORIGIN" ] || [ -z "$FMS_TOKEN" ]; then
  echo "缺少 FMS 外部域名或桥接 token。请检查："
  echo "  $FMS_ENV"
  echo "  CORS_ALLOWED_ORIGINS=..."
  echo "  FMS_WEKNORA_BRIDGE_TOKEN=..."
  exit 1
fi

export FMS_ORIGIN FMS_TOKEN
python3 - <<'PY'
from pathlib import Path
import os
import re
import secrets

path = Path(".env")
text = path.read_text(encoding="utf-8")
origin = os.environ["FMS_ORIGIN"].rstrip("/")
token = os.environ["FMS_TOKEN"]

def current(key):
    match = re.search(rf"(?m)^{re.escape(key)}=(.*)$", text)
    return match.group(1).strip() if match else ""

def keep_or_generate(key, length):
    value = current(key)
    examples = {"postgres123!@#", "redis123!@#", "", "latest"}
    if value and value not in examples and not value.startswith("<"):
        return value
    return secrets.token_hex(length)

values = {
    "WEKNORA_VERSION": "0.8.0",
    "GIN_MODE": "release",
    "AUTO_MIGRATE": "true",
    "FRONTEND_PORT": "18083",
    "FRONTEND_BIND_ADDRESS": "172.17.0.1",
    "VITE_BASE_PATH": "/zswek/",
    "APP_BIND_ADDRESS": "127.0.0.1",
    "FRONTEND_BASE_URL": f"{origin}/zswek",
    "APP_EXTERNAL_URL": f"{origin}/zswek",
    "DB_DRIVER": "postgres",
    "DB_HOST": "postgres",
    "DB_PORT": "5432",
    "DB_USER": "weknora",
    "DB_PASSWORD": keep_or_generate("DB_PASSWORD", 24),
    "DB_NAME": "weknora",
    "STREAM_MANAGER_TYPE": "redis",
    "REDIS_ADDR": "redis:6379",
    "REDIS_PASSWORD": keep_or_generate("REDIS_PASSWORD", 24),
    "RETRIEVE_DRIVER": "postgres",
    "STORAGE_TYPE": "local",
    "LOCAL_STORAGE_BASE_DIR": "/data/files",
    "JWT_SECRET": keep_or_generate("JWT_SECRET", 32),
    "SYSTEM_AES_KEY": keep_or_generate("SYSTEM_AES_KEY", 16),
    "DISABLE_REGISTRATION": "false",
    "FMS_BASE_URL": "http://host.docker.internal:18002",
    "FMS_SERVICE_TOKEN": token,
    "FMS_PAGE_SIZE": "100",
    "FMS_REQUEST_TIMEOUT": "30s",
}

lines = text.splitlines()
for key, value in values.items():
    pattern = re.compile(rf"^{re.escape(key)}=.*$")
    replacement = f"{key}={value}"
    for index, line in enumerate(lines):
        if pattern.match(line):
            lines[index] = replacement
            break
    else:
        lines.append(replacement)

path.write_text("\n".join(lines).rstrip() + "\n", encoding="utf-8")
PY

chmod 600 .env
docker compose -f docker-compose.yml config --quiet
BUILDKIT_PROGRESS=plain \
APK_MIRROR_ARG=mirrors.aliyun.com \
APT_MIRROR=http://mirrors.aliyun.com \
docker compose -f docker-compose.yml build app frontend docreader
docker compose -f docker-compose.yml up -d --no-build app frontend docreader
docker compose -f docker-compose.yml ps app frontend docreader
```

这段命令只依赖 FMS 已经写好的两个值：`CORS_ALLOWED_ORIGINS` 和 `FMS_WEKNORA_BRIDGE_TOKEN`。如果 FMS 的 `CORS_ALLOWED_ORIGINS` 还是示例中的 `localhost`，先把它改成实际浏览器访问的外部 origin；不能把 `127.0.0.1:18082` 当成外网地址。

## 2. 配置 WeKnora `.env`

编辑 `/srv/weknora/.env`，至少设置：

```dotenv
WEKNORA_VERSION=0.8.0
GIN_MODE=release
AUTO_MIGRATE=true

# 构建阶段镜像源：APT 与 Python pip/uv 分开配置
APT_MIRROR=mirrors.aliyun.com
PYTHON_PACKAGE_INDEX=https://mirrors.aliyun.com/pypi/simple/

# WeKnora frontend：FMS frontend 代理到这里
FRONTEND_PORT=18083
# FMS 容器通过 host.docker.internal 访问；本服务器网关为 172.17.0.1
FRONTEND_BIND_ADDRESS=172.17.0.1
VITE_BASE_PATH=/zswek/

# WeKnora app：只绑定本机；容器内 frontend 仍通过 app:8080 访问它
APP_BIND_ADDRESS=127.0.0.1

# 使用 FMS 的同一个外部域名
FRONTEND_BASE_URL=https://<FMS外部域名>/zswek
# 使用 local 文件存储时，外部文件/图片链接也走同一个入口
APP_EXTERNAL_URL=https://<FMS外部域名>/zswek

# WeKnora 自己的 PostgreSQL / Redis，不与 FMS 共用数据
DB_DRIVER=postgres
DB_HOST=postgres
DB_PORT=5432
DB_USER=weknora
DB_PASSWORD=<WeKnora自己的数据库密码>
DB_NAME=weknora

STREAM_MANAGER_TYPE=redis
REDIS_ADDR=redis:6379
REDIS_PASSWORD=<WeKnora自己的Redis密码>

RETRIEVE_DRIVER=postgres
STORAGE_TYPE=local
LOCAL_STORAGE_BASE_DIR=/data/files

JWT_SECRET=<随机长字符串>
SYSTEM_AES_KEY=<正好32个字符>
```

生成密钥：

```bash
openssl rand -hex 24   # DB_PASSWORD
openssl rand -hex 24   # REDIS_PASSWORD
openssl rand -hex 32   # JWT_SECRET
openssl rand -hex 16   # SYSTEM_AES_KEY，正好32个字符
```

不要把 FMS 的数据库密码、`DATABASE_URL` 或 Redis 配置直接复制给 WeKnora。两边共用服务器和外部入口，但数据服务独立。

首次部署时先保持：

```dotenv
DISABLE_REGISTRATION=false
```

注册第一个 WeKnora 管理账号并确认能登录后，再改成 `true` 并重启 WeKnora。

## 3. 启动 WeKnora

服务器使用当前项目根目录的标准 `docker-compose.yml`，并且必须从当前项目源码构建镜像。不能只执行 `docker compose pull`，否则会使用仓库声明的远程镜像，当前项目的代码修改不会进入镜像：

```bash
cd /srv/weknora
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.yml build --pull app frontend docreader
docker compose -f docker-compose.yml up -d
docker compose -f docker-compose.yml ps
```

不要使用：

```text
docker-compose.dev.yml             # 本地开发编排
docker-compose.fms-bridge-dev.yml  # FMS 本机联调编排
```

启动后核心服务是 `frontend`、`app`、`postgres`、`redis`、`docreader`。`AUTO_MIGRATE=true` 会由 `app` 自动执行数据库迁移。

## 4. 配置 FMS 代理：文件位置和具体内容

以下路径都是 FMS 项目中的真实文件，不是在 WeKnora 项目里新建。

### 4.1 FMS 服务器私有环境文件

服务器文件：

```text
/srv/fms/app/.env.docker.production
```

由 `/srv/fms/app/scripts/compose.sh production ...` 自动加载。可在这个文件中显式加入或确认：

```dotenv
WEKNORA_FRONTEND_URL=http://host.docker.internal:18083
```

本机源码对应的模板位置是：

```text
/Users/luyunning/Intership Project/file-manage-system/.env.docker.example:162-164
```

### 4.2 FMS server Compose

文件：

```text
/srv/fms/app/docker-compose.server.yml
```

在 `frontend` 服务中确认以下内容，当前仓库对应第 37-44 行。这个配置写在 Compose 文件里，不写进 `.env`：

```yaml
extra_hosts:
  - "host.docker.internal:host-gateway"
```

还必须确认 FMS backend 也绑定到 Docker host-gateway。当前服务器的正确写法是：

```yaml
ports:
  - "172.17.0.1:${BACKEND_HOST_PORT:?BACKEND_HOST_PORT must be set}:8000"
```

如果这里仍是 `127.0.0.1:...:8000`，WeKnora app 虽然能解析
`host.docker.internal`，但访问 FMS backend 仍会超时。先在服务器上确认网关地址：

```bash
docker exec WeKnora-app getent hosts host.docker.internal
docker exec fms-production-frontend-1 getent hosts host.docker.internal
```

两边应返回同一个地址；若不是 `172.17.0.1`，以下配置中的地址全部替换为实际返回值。
首次修复 FMS backend 绑定时，在服务器执行：

```bash
cd /srv/fms/app
cp docker-compose.server.yml docker-compose.server.yml.bak
sed -i 's#127\.0\.0\.1:${BACKEND_HOST_PORT#172.17.0.1:${BACKEND_HOST_PORT#' docker-compose.server.yml
sh scripts/compose.sh production config --quiet
sh scripts/compose.sh production up -d --no-build backend
```

这只是端口监听范围调整，不需要重建 FMS backend 镜像。以后更新 FMS 的
`docker-compose.server.yml` 时，要保留这项绑定，否则 WeKnora 的 FMS 桥接会再次出现
`context deadline exceeded`。

### 4.3 FMS 前端 Nginx

文件：

```text
/srv/fms/app/frontend/nginx.conf
```

当前仓库对应第 24-38 行，确认有：

```nginx
location ^~ /zswek/ {
    proxy_pass ${WEKNORA_FRONTEND_URL}/;
}
```

如果 4.1、4.2、4.3 已存在，不需要再添加第二份配置。只启动 WeKnora 即可；只有 FMS 的环境文件或 Nginx/Compose 文件发生变化时，才执行：

```bash
cd /srv/fms/app
sh scripts/compose.sh production up -d frontend
sh scripts/compose.sh production ps
```

如果修改了 `frontend/nginx.conf` 或 `docker-compose.server.yml`，先重新构建 FMS frontend：

```bash
cd /srv/fms/app
sh scripts/compose.sh production build frontend
sh scripts/compose.sh production up -d frontend
```

外部访问地址就是：

```text
https://<FMS外部域名>/zswek/
```

## 5. 验收

先在服务器本机验证 WeKnora：

```bash
cd /srv/weknora
curl -fsS http://127.0.0.1:8080/health
curl -fsSI http://172.17.0.1:18083/
```

再从容器内部验证跨容器链路：

```bash
docker exec WeKnora-app curl -fsS --max-time 10 http://host.docker.internal:18002/health
docker exec fms-production-frontend-1 wget -S --spider -T 10 http://host.docker.internal:18083/ 2>&1
```

只有这两条都成功后，才在 FMS 页面点击 WeKnora 的“运行”。桥接失败时先看：

```bash
cd /srv/weknora
docker compose -f docker-compose.yml logs --since=10m app | grep -E 'FMS|projection|failed|succeeded'
```

再验证 FMS 代理：

```bash
curl -I https://<FMS外部域名>/zswek/
```

浏览器访问同一个外部域名的 `/zswek/` 路径，注册、配置模型、上传小文档并完成一次问答。

## 6. 排错

WeKnora 服务：

```bash
cd /srv/weknora
docker compose -f docker-compose.yml ps
docker compose -f docker-compose.yml logs --tail=300 app
docker compose -f docker-compose.yml logs --tail=200 frontend postgres redis docreader
```

FMS 代理：

```bash
cd /srv/fms/app
sh scripts/compose.sh production logs --tail=200 frontend
```

按顺序检查：

1. WeKnora frontend 是否绑定 `172.17.0.1:18083`，而不是只绑定 `127.0.0.1:18083`。
2. FMS backend 是否绑定 `172.17.0.1:18002`，而不是只绑定 `127.0.0.1:18002`。
3. FMS frontend 是否配置 `host.docker.internal:18083`。
4. FMS frontend 和 WeKnora app 是否都配置 `host-gateway`。
5. `VITE_BASE_PATH` 是否严格为 `/zswek/`，包含末尾 `/`。
6. 外部访问是否使用 `/zswek/`，而不是 `/`。

## 7. 后续更新部署（以后照此执行）

下面是服务器更新 WeKnora 的固定流程。默认只更新 WeKnora，不重启 FMS；`.env`、数据库卷、文件卷
都会保留。先确认工作区没有服务器临时改动：

```bash
cd /srv/weknora
git fetch origin fms-weknora
git checkout fms-weknora
git pull --ff-only origin fms-weknora
git status --short
docker compose -f docker-compose.yml config --quiet
BUILDKIT_PROGRESS=plain \
APK_MIRROR_ARG=mirrors.aliyun.com \
APT_MIRROR=http://mirrors.aliyun.com \
docker compose -f docker-compose.yml build app frontend docreader
docker compose -f docker-compose.yml up -d --no-build app frontend docreader
docker compose -f docker-compose.yml ps app frontend docreader
curl -fsS http://127.0.0.1:8080/health
curl -fsSI http://172.17.0.1:18083/
docker exec WeKnora-app curl -fsS --max-time 10 http://host.docker.internal:18002/health
```

第一次部署、明确要刷新基础镜像或需要获取安全更新时，才把构建命令改成：

```bash
BUILDKIT_PROGRESS=plain \
APK_MIRROR_ARG=mirrors.aliyun.com \
APT_MIRROR=http://mirrors.aliyun.com \
docker compose -f docker-compose.yml build --pull app frontend docreader
```

`--pull` 只刷新 Dockerfile 中 `FROM` 的基础镜像，不是启动参数；它可能让原本命中的缓存失效，
因此日常更新不要每次都加。`BUILDKIT_PROGRESS=plain` 用于显示真实步骤，避免终端只看到长时间停在
`86/94` 而无法判断是否仍在工作。

只更新 WeKnora 不需要重启 FMS backend。只有 FMS 的 `/zswek/` 代理配置变化时，才重启 FMS frontend。
如果 FMS backend 的绑定配置变化，执行 `sh scripts/compose.sh production up -d --no-build backend`；
不需要重新构建镜像。

点击 FMS 的 WeKnora“运行”前，必须先通过本节最后一条 `docker exec WeKnora-app ...:18002/health`
检查。网络检查失败时不要重复点击“运行”，也不需要重新构建 WeKnora。

不要执行 `docker compose down -v`，否则会删除 WeKnora 的数据库和文件卷。

## 8. 2026-09-30 现场验证记录

- 服务器为 `amd64`；`app`、`frontend`、`docreader` 三个镜像均已成功构建，产物分别为
  `wechatopenai/weknora-app:0.8.0`、`wechatopenai/weknora-ui:0.8.0` 和
  `wechatopenai/weknora-docreader:0.8.0`。
- `RedundantTargetPlatform` 只是 Dockerfile 的冗余平台声明警告，不是构建失败原因。
- `86/94` 长时间不动时，优先用本文件的 `BUILDKIT_PROGRESS=plain` 命令观察；服务器资源和磁盘正常时，
  可能只是 Rust 编译或镜像导出阶段较慢。
- 本次 FMS 桥接失败的根因是容器访问 `host.docker.internal` 时到达
  `172.17.0.1`，但 FMS backend/WeKnora frontend 曾只监听 `127.0.0.1`。修复绑定后无需重建镜像，
  先做容器内健康检查，再运行桥接。
- 日志中的 Langfuse DNS/遥测告警与 FMS 桥接网络失败是两件事，不能用它判断桥接失败原因。

## 相关文档

- [安装部署总览](./02-installation.md)
- [快速上手](./03-quickstart.md)
- [配置详解](./04-configuration.md)
- FMS 侧手册：`/Users/luyunning/Intership Project/file-manage-system/生产服务器操作手册.md`
