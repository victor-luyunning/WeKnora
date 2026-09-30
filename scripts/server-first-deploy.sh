#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

FMS_ENV="${FMS_ENV:-/srv/fms/app/.env.docker.production}"
if [[ ! -f "$FMS_ENV" ]]; then
  echo "找不到 FMS 生产环境文件：$FMS_ENV" >&2
  exit 1
fi

FMS_ORIGIN="$(grep '^CORS_ALLOWED_ORIGINS=' "$FMS_ENV" | head -n1 | cut -d= -f2- | tr -d '\"' | cut -d, -f1)"
FMS_TOKEN="$(grep '^FMS_WEKNORA_BRIDGE_TOKEN=' "$FMS_ENV" | head -n1 | cut -d= -f2- | tr -d '\"' | tr -d '\r')"

if [[ -z "$FMS_ORIGIN" || -z "$FMS_TOKEN" ]]; then
  echo "FMS 外部域名或桥接 token 未配置。请检查：" >&2
  echo "  $FMS_ENV" >&2
  echo "  CORS_ALLOWED_ORIGINS=..." >&2
  echo "  FMS_WEKNORA_BRIDGE_TOKEN=..." >&2
  exit 1
fi

export FMS_ORIGIN FMS_TOKEN
python3 - <<'PY'
from pathlib import Path
import os
import re
import secrets

path = Path(".env")
if not path.exists():
    raise SystemExit("找不到 .env；先执行 cp .env.example .env")
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
    "FRONTEND_BIND_ADDRESS": "127.0.0.1",
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
    "GOPROXY_ARG": "https://goproxy.cn,direct",
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
docker compose -f docker-compose.yml build --pull app frontend docreader
docker compose -f docker-compose.yml up -d
docker compose -f docker-compose.yml ps
