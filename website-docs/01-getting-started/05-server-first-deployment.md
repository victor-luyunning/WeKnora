# 与 FMS 共用外部入口部署

本文只说明：服务器已有 FMS、Docker 和 Compose 时，如何部署 WeKnora 项目环境。

目标链路：

```text
外部同一个 FMS 地址
        │
        └── /zswek/ ──> FMS frontend ──> 127.0.0.1:18083
                                      │
                                      └── WeKnora frontend
                                            └── WeKnora app
```

WeKnora 不新增公网端口，也不占用 FMS 的 `18082`。`18083` 只供服务器本机的 FMS frontend 访问。

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

## 2. 配置 WeKnora `.env`

编辑 `/srv/weknora/.env`，至少设置：

```dotenv
WEKNORA_VERSION=0.8.0
GIN_MODE=release
AUTO_MIGRATE=true

# WeKnora frontend：FMS frontend 代理到这里
FRONTEND_PORT=18083
FRONTEND_BIND_ADDRESS=127.0.0.1
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
curl -fsSI http://127.0.0.1:18083/
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

1. `127.0.0.1:18083` 是否能访问 WeKnora frontend。
2. FMS frontend 是否配置 `host.docker.internal:18083`。
3. FMS frontend 是否配置 `host-gateway`。
4. `VITE_BASE_PATH` 是否严格为 `/zswek/`，包含末尾 `/`。
5. 外部访问是否使用 `/zswek/`，而不是 `/`。

## 7. 更新当前项目

```bash
cd /srv/weknora
git fetch origin fms-weknora
git checkout fms-weknora
git pull --ff-only origin fms-weknora
docker compose -f docker-compose.yml config --quiet
docker compose -f docker-compose.yml build --pull app frontend docreader
docker compose -f docker-compose.yml up -d
docker compose -f docker-compose.yml ps
```

只更新 WeKnora 不需要重启 FMS backend。只有 FMS 的 `/zswek/` 代理配置变化时，才重启 FMS frontend。

不要执行 `docker compose down -v`，否则会删除 WeKnora 的数据库和文件卷。

## 相关文档

- [安装部署总览](./02-installation.md)
- [快速上手](./03-quickstart.md)
- [配置详解](./04-configuration.md)
- FMS 侧手册：`/Users/luyunning/Intership Project/file-manage-system/生产服务器操作手册.md`
