#!/usr/bin/env bash
set -euo pipefail

DEFAULT_IMAGE="ghcr.io/daodao97/focusbi:latest"
DEFAULT_DIR="./focusbi"
DEFAULT_PORT="8080"

log() {
  printf '[focusbi] %s\n' "$*"
}

die() {
  printf '[focusbi] ERROR: %s\n' "$*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing command: $1"
}

compose_cmd() {
  if docker compose version >/dev/null 2>&1; then
    printf 'docker compose'
    return
  fi
  if command -v docker-compose >/dev/null 2>&1; then
    printf 'docker-compose'
    return
  fi
  die "Docker Compose is required"
}

ask() {
  local prompt="$1"
  local default="$2"
  local value
  if [[ "${FOCUSBI_ASSUME_YES:-}" == "1" ]]; then
    printf '%s' "$default"
    return
  fi
  read -r -p "$prompt [$default]: " value </dev/tty || value=""
  printf '%s' "${value:-$default}"
}

ask_secret() {
  local prompt="$1"
  local default="$2"
  local value
  if [[ "${FOCUSBI_ASSUME_YES:-}" == "1" ]]; then
    printf '%s' "$default"
    return
  fi
  read -r -s -p "$prompt [auto-generated]: " value </dev/tty || value=""
  printf '\n' >/dev/tty
  printf '%s' "${value:-$default}"
}

random_hex() {
  local bytes="${1:-32}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
    return
  fi
  if [[ -r /dev/urandom ]]; then
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    date +%s%N | sha256sum | awk '{print $1}'
    return
  fi
  date +%s%N | shasum -a 256 | awk '{print $1}'
}

yaml_dq() {
  local value="$1"
  value="${value//\\/\\\\}"
  value="${value//\"/\\\"}"
  printf '"%s"' "$value"
}

write_file() {
  local path="$1"
  local content="$2"
  if [[ -e "$path" && "${FOCUSBI_FORCE:-}" != "1" ]]; then
    die "$path already exists; set FOCUSBI_FORCE=1 to overwrite"
  fi
  printf '%s\n' "$content" >"$path"
}

# yes/no 询问, 返回 "1" / "0"。第二参为默认 (y/n)。
ask_yesno() {
  local prompt="$1"
  local default="$2"
  local ans
  ans="$(ask "$prompt (y/n)" "$default")"
  case "$ans" in
    y|Y|yes|YES|1|true) printf '1' ;;
    *) printf '0' ;;
  esac
}

# compose 里的 mysql 服务块 (自托管时使用)。
mysql_service() {
  cat <<'YAML'
  mysql:
    image: mysql:8.4
    environment:
      MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}
      MYSQL_DATABASE: focusbi
    command: --character-set-server=utf8mb4 --collation-server=utf8mb4_unicode_ci
    volumes:
      - ./data/mysql:/var/lib/mysql
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "127.0.0.1", "-uroot", "-p${MYSQL_ROOT_PASSWORD}"]
      interval: 5s
      timeout: 5s
      retries: 30
    restart: unless-stopped

YAML
}

# compose 里的 redis 服务块 (自托管时使用)。
redis_service() {
  cat <<'YAML'
  redis:
    image: redis:latest
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 20
    restart: unless-stopped

YAML
}

# compose 里的 app 服务块。$1/$2 = 是否自托管 mysql/redis (用于 depends_on)。
app_service() {
  local self_mysql="$1"
  local self_redis="$2"
  cat <<'YAML'
  app:
    image: ${IMAGE:-ghcr.io/daodao97/focusbi:latest}
    command: ["/app/app", "--bind", "0.0.0.0:8080", "--app-env", "prod"]
    environment:
      ENABLE_CRON: "${ENABLE_CRON:-true}"   # 启用定时任务调度
    ports:
      - "${PORT:-8080}:8080"
    volumes:
      # 挂载配置 (含 jwt_secret / 数据源); 改这里即可, 无需重建镜像
      - ./conf.prod.yaml:/app/conf.prod.yaml:ro
    restart: unless-stopped
YAML

  # depends_on: 仅对自托管的依赖加健康检查等待。
  if [[ "$self_mysql" == "1" || "$self_redis" == "1" ]]; then
    printf '    depends_on:\n'
    if [[ "$self_mysql" == "1" ]]; then
      printf '      mysql:\n        condition: service_healthy\n'
    fi
    if [[ "$self_redis" == "1" ]]; then
      printf '      redis:\n        condition: service_healthy\n'
    fi
  fi
}

# 组装 compose.yaml: app 必有, mysql/redis 按选择决定是否纳入。
build_compose() {
  local self_mysql="$1"
  local self_redis="$2"
  printf 'services:\n'
  [[ "$self_mysql" == "1" ]] && mysql_service
  [[ "$self_redis" == "1" ]] && redis_service
  app_service "$self_mysql" "$self_redis"
}

app_config() {
  local mysql_dsn="$1"
  local redis_addr="$2"
  local site_url="$3"
  local jwt_secret="$4"
  local query_timeout="$5"
  cat <<YAML
database:
  - name: default
    driver: mysql
    dsn: $(yaml_dq "$mysql_dsn")

redis:
  - name: default
    addr: $(yaml_dq "$redis_addr")

engine:
  query_timeout: $(yaml_dq "$query_timeout")
  query_concurrency: 8
  script_timeout: "3m"
  # 脚本 fetch() 外呼权限: 空/"off" 禁用; "on" 允许公网; 或逗号分隔的 URL 白名单
  script_fetch: "off"

schedule:
  enabled: true

security:
  public_share_enabled: true

ai:
  provider: ""
  base_url: ""
  api_key: ""
  model: ""

turnstile:
  site_key: ""
  secret_key: ""

site:
  url: $(yaml_dq "$site_url")
  jwt_secret: $(yaml_dq "$jwt_secret")
YAML
}

# 生成 deploy.sh: 拉新镜像并滚动重启, 方便后续更新。
deploy_script() {
  cat <<'SH'
#!/usr/bin/env bash
# 更新 FocusBI: 拉取最新镜像并重启。在本目录 (含 compose.yaml) 执行。
set -euo pipefail

cd "$(dirname "$0")"

if docker compose version >/dev/null 2>&1; then
  COMPOSE="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE="docker-compose"
else
  echo "需要 Docker Compose" >&2
  exit 1
fi

echo "[deploy] 拉取最新镜像…"
$COMPOSE pull

echo "[deploy] 重启服务…"
$COMPOSE up -d

echo "[deploy] 清理旧镜像…"
docker image prune -f >/dev/null 2>&1 || true

echo "[deploy] 完成。日志: $COMPOSE logs -f app"
SH
}

main() {
  need_cmd docker
  local compose
  compose="$(compose_cmd)"

  local install_dir image port enable_cron query_timeout site_url jwt_secret
  install_dir="${FOCUSBI_INSTALL_DIR:-$(ask 'Install directory' "$DEFAULT_DIR")}"
  image="${IMAGE:-$(ask 'Docker image' "$DEFAULT_IMAGE")}"
  port="${PORT:-$(ask 'Host port' "$DEFAULT_PORT")}"
  enable_cron="${ENABLE_CRON:-$(ask 'Enable scheduled-task cron? true/false' 'true')}"
  query_timeout="${ENGINE_QUERY_TIMEOUT:-$(ask 'SQL query timeout' '3m')}"

  # MySQL / Redis 各自选择: compose 自托管 还是 外部地址。
  local self_mysql self_redis
  if [[ -n "${FOCUSBI_SELF_MYSQL:-}" ]]; then
    self_mysql="$FOCUSBI_SELF_MYSQL"
  else
    self_mysql="$(ask_yesno 'Run MySQL in compose? (n = use external MySQL)' 'y')"
  fi
  if [[ -n "${FOCUSBI_SELF_REDIS:-}" ]]; then
    self_redis="$FOCUSBI_SELF_REDIS"
  else
    self_redis="$(ask_yesno 'Run Redis in compose? (n = use external Redis)' 'y')"
  fi

  mkdir -p "$install_dir"
  cd "$install_dir"
  [[ "$self_mysql" == "1" || "$self_redis" == "1" ]] && mkdir -p data

  site_url="${SITE_URL:-$(ask 'Public site URL' "http://127.0.0.1:${port}")}"
  jwt_secret="${SITE_JWT_SECRET:-}"
  if [[ -z "$jwt_secret" ]]; then
    jwt_secret="${JWT_SECRET:-$(ask_secret 'JWT secret' "$(random_hex 32)")}"
  fi

  # 数据源 / Redis 地址: 自托管走服务名, 外部询问。
  local mysql_dsn redis_addr mysql_root_password=""
  if [[ "$self_mysql" == "1" ]]; then
    mysql_root_password="${MYSQL_ROOT_PASSWORD:-$(ask_secret 'MySQL root password' "$(random_hex 16)")}"
    mysql_dsn="root:${mysql_root_password}@tcp(mysql:3306)/focusbi?charset=utf8mb4&parseTime=True&loc=Local"
  else
    mysql_dsn="${MYSQL_DSN:-$(ask 'MySQL DSN' 'user:pass@tcp(mysql.example.com:3306)/focusbi?charset=utf8mb4&parseTime=True&loc=Local')}"
  fi
  if [[ "$self_redis" == "1" ]]; then
    redis_addr="redis:6379"
  else
    redis_addr="${REDIS_ADDR:-$(ask 'Redis addr' 'redis.example.com:6379')}"
  fi

  # .env: 供 compose 插值 IMAGE / PORT / ENABLE_CRON (+ 自托管 mysql 密码)。
  local env_content="IMAGE=${image}
PORT=${port}
ENABLE_CRON=${enable_cron}"
  if [[ "$self_mysql" == "1" ]]; then
    env_content="${env_content}
MYSQL_ROOT_PASSWORD=${mysql_root_password}"
  fi
  write_file ".env" "$env_content"

  write_file "compose.yaml" "$(build_compose "$self_mysql" "$self_redis")"
  write_file "conf.prod.yaml" "$(app_config "$mysql_dsn" "$redis_addr" "$site_url" "$jwt_secret" "$query_timeout")"
  write_file "deploy.sh" "$(deploy_script)"
  chmod +x deploy.sh

  write_file "README.deploy.md" "FocusBI deployment

Commands:
  ${compose} pull
  ${compose} up -d
  ${compose} logs -f app
  ${compose} down
  ./deploy.sh            # 更新到最新镜像并重启

Files:
  .env              image / port / scheduler settings (+ mysql password when self-hosted)
  conf.prod.yaml    FocusBI runtime config (datasource / redis / jwt / site)
  compose.yaml
  deploy.sh         一键更新脚本
  data/             MySQL/Redis data when self-hosted
"

  log "Files written to $(pwd)"
  log "Next steps:"
  printf '  cd %s\n' "$(pwd)"
  printf '  %s up -d\n' "$compose"
  printf '  %s logs -f app\n' "$compose"
  log "Update later with: ./deploy.sh"
  log "After startup, open ${site_url}"
}

main "$@"
