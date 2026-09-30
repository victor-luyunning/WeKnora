# FMS-WeKnora 本地 Docker 操作手册

## 三个容器分别做什么

在 Docker Desktop 的 `weknora` 项目中：

| 容器 | 作用 | 是否保存业务数据 |
| --- | --- | --- |
| `WeKnora-postgres-dev` | WeKnora 数据库：账号、配置、FMS 镜像和同步记录 | 是，数据在 `weknora_postgres-data-dev` |
| `WeKnora-redis-dev` | 队列和缓存 | 是，数据在 `weknora_redis_data_dev` |
| `fms-weknora` | WeKnora 应用 API：登录、管理页面后端、FMS 同步 | 应用本身无须单独保存数据 |

FMS 后端仍运行在宿主机 `8000`，`fms-weknora` 通过
`host.docker.internal:8000`访问它。FMS 的数据库和 Qdrant 属于另一个
`fms-source-development` 项目，不要和 WeKnora 的 PostgreSQL 混淆。

## Docker Desktop 里的固定入口

打开 `Containers`，展开 `weknora`，找到 `fms-weknora`：

- 端口：`127.0.0.1:8080`
- 状态：应为 `healthy`
- 重启策略：`unless-stopped`
- Docker Desktop 重启后会自动恢复
- 点该行的重启按钮即可重启应用，不需要终端命令

不要再用宿主机 `go run ./cmd/server` 启动第二套 WeKnora，否则会和
Docker 容器争用 `8080`，并造成“关了容器仍然能登录”的误判。

## 数据和代码

- PostgreSQL 数据通过 Docker Volume 保存，删除容器不会删除它。
- WeKnora 文件数据映射到仓库的 `.local-data/files`。
- `fms-weknora` 使用当前仓库源码启动，代码变更后点 Docker Desktop 的
  重启会重新编译并加载；不需要手动找进程。
- FMS 的 `8000` 进程必须保持运行，否则 WeKnora 页面仍可登录，但 FMS
  同步会失败。

## 判断是否正常

浏览器访问 WeKnora 后端健康地址：

```text
http://127.0.0.1:8080/health
```

返回 `{"status":"ok"}` 且 Docker Desktop 显示 `healthy`，才表示当前
登录和同步使用的是 Docker 中的 `fms-weknora`。
