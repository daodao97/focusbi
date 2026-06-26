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

normalize_mode() {
  local mode="$1"
  case "$mode" in
    1|stack|all|compose) printf 'stack' ;;
    2|external|app|single) printf 'external' ;;
    *) die "invalid install mode: $mode" ;;
  esac
}

stack_compose() {
  cat <<'YAML'
services:
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

  redis:
    image: redis:latest
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 30
    restart: unless-stopped

  app:
    image: ${FOCUSBI_IMAGE}
    command: ["/app/app", "--bind", "0.0.0.0:8080"]
    environment:
      APP_ENV: "dev"
      ENABLE_CRON: "${ENABLE_CRON}"
      SITE_URL: "${SITE_URL}"
      SITE_JWT_SECRET: "${SITE_JWT_SECRET}"
      ENGINE_QUERY_TIMEOUT: "${ENGINE_QUERY_TIMEOUT}"
      AI_PROVIDER: "${AI_PROVIDER}"
      AI_BASE_URL: "${AI_BASE_URL}"
      AI_API_KEY: "${AI_API_KEY}"
      AI_MODEL: "${AI_MODEL}"
      TURNSTILE_SITE_KEY: "${TURNSTILE_SITE_KEY}"
      TURNSTILE_SECRET_KEY: "${TURNSTILE_SECRET_KEY}"
    ports:
      - "${FOCUSBI_PORT}:8080"
    volumes:
      - ./conf.dev.yaml:/app/conf.dev.yaml:ro
    depends_on:
      mysql:
        condition: service_healthy
      redis:
        condition: service_healthy
    restart: unless-stopped
YAML
}

external_compose() {
  cat <<'YAML'
services:
  app:
    image: ${FOCUSBI_IMAGE}
    command: ["/app/app", "--bind", "0.0.0.0:8080"]
    environment:
      APP_ENV: "dev"
      ENABLE_CRON: "${ENABLE_CRON}"
      SITE_URL: "${SITE_URL}"
      SITE_JWT_SECRET: "${SITE_JWT_SECRET}"
      ENGINE_QUERY_TIMEOUT: "${ENGINE_QUERY_TIMEOUT}"
      AI_PROVIDER: "${AI_PROVIDER}"
      AI_BASE_URL: "${AI_BASE_URL}"
      AI_API_KEY: "${AI_API_KEY}"
      AI_MODEL: "${AI_MODEL}"
      TURNSTILE_SITE_KEY: "${TURNSTILE_SITE_KEY}"
      TURNSTILE_SECRET_KEY: "${TURNSTILE_SECRET_KEY}"
    ports:
      - "${FOCUSBI_PORT}:8080"
    volumes:
      - ./conf.dev.yaml:/app/conf.dev.yaml:ro
    restart: unless-stopped
YAML
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

main() {
  need_cmd docker
  local compose
  compose="$(compose_cmd)"

  local install_dir image port enable_cron query_timeout mode raw_mode site_url jwt_secret
  install_dir="${FOCUSBI_INSTALL_DIR:-$(ask 'Install directory' "$DEFAULT_DIR")}"
  image="${FOCUSBI_IMAGE:-$(ask 'Docker image' "$DEFAULT_IMAGE")}"
  port="${FOCUSBI_PORT:-$(ask 'Host port' "$DEFAULT_PORT")}"
  enable_cron="${ENABLE_CRON:-$(ask 'Enable subscription scheduler? true/false' 'true')}"
  query_timeout="${ENGINE_QUERY_TIMEOUT:-$(ask 'SQL query timeout' '3m')}"

  if [[ -n "${FOCUSBI_MODE:-}" ]]; then
    mode="$(normalize_mode "$FOCUSBI_MODE")"
  else
    if [[ "${FOCUSBI_ASSUME_YES:-}" != "1" ]]; then
      printf '\nChoose install mode:\n' >/dev/tty
      printf '  1) stack    FocusBI + MySQL + Redis (recommended for a new server)\n' >/dev/tty
      printf '  2) external FocusBI only, use existing MySQL + Redis\n' >/dev/tty
    fi
    raw_mode="$(ask 'Mode' '1')"
    mode="$(normalize_mode "$raw_mode")"
  fi

  mkdir -p "$install_dir"
  cd "$install_dir"
  mkdir -p data

  site_url="${SITE_URL:-$(ask 'Public site URL' "http://127.0.0.1:${port}")}"
  jwt_secret="${SITE_JWT_SECRET:-}"
  if [[ -z "$jwt_secret" ]]; then
    jwt_secret="${JWT_SECRET:-$(ask_secret 'JWT secret' "$(random_hex 32)")}"
  fi

  local mysql_dsn redis_addr mysql_root_password
  if [[ "$mode" == "stack" ]]; then
    mysql_root_password="${MYSQL_ROOT_PASSWORD:-$(ask_secret 'MySQL root password' "$(random_hex 16)")}"
    mysql_dsn="root:${mysql_root_password}@tcp(mysql:3306)/focusbi?charset=utf8mb4&parseTime=True&loc=Local"
    redis_addr="redis:6379"
    write_file ".env" "FOCUSBI_IMAGE=${image}
FOCUSBI_PORT=${port}
ENABLE_CRON=${enable_cron}
SITE_URL=${site_url}
SITE_JWT_SECRET=${jwt_secret}
ENGINE_QUERY_TIMEOUT=${query_timeout}
AI_PROVIDER=${AI_PROVIDER:-}
AI_BASE_URL=${AI_BASE_URL:-}
AI_API_KEY=${AI_API_KEY:-}
AI_MODEL=${AI_MODEL:-}
TURNSTILE_SITE_KEY=${TURNSTILE_SITE_KEY:-}
TURNSTILE_SECRET_KEY=${TURNSTILE_SECRET_KEY:-}
MYSQL_ROOT_PASSWORD=${mysql_root_password}"
    write_file "docker-compose.yml" "$(stack_compose)"
  else
    mysql_dsn="${MYSQL_DSN:-$(ask 'MySQL DSN' 'user:pass@tcp(mysql.example.com:3306)/focusbi?charset=utf8mb4&parseTime=True&loc=Local')}"
    redis_addr="${REDIS_ADDR:-$(ask 'Redis addr' 'redis.example.com:6379')}"
    write_file ".env" "FOCUSBI_IMAGE=${image}
FOCUSBI_PORT=${port}
ENABLE_CRON=${enable_cron}
SITE_URL=${site_url}
SITE_JWT_SECRET=${jwt_secret}
ENGINE_QUERY_TIMEOUT=${query_timeout}
AI_PROVIDER=${AI_PROVIDER:-}
AI_BASE_URL=${AI_BASE_URL:-}
AI_API_KEY=${AI_API_KEY:-}
AI_MODEL=${AI_MODEL:-}
TURNSTILE_SITE_KEY=${TURNSTILE_SITE_KEY:-}
TURNSTILE_SECRET_KEY=${TURNSTILE_SECRET_KEY:-}"
    write_file "docker-compose.yml" "$(external_compose)"
  fi

  write_file "conf.dev.yaml" "$(app_config "$mysql_dsn" "$redis_addr" "$site_url" "$jwt_secret" "$query_timeout")"
  write_file "README.deploy.md" "FocusBI deployment

Commands:
  ${compose} pull
  ${compose} up -d
  ${compose} logs -f app
  ${compose} down

Files:
  .env              image, port and scheduler settings
  conf.dev.yaml     FocusBI runtime config
  docker-compose.yml
  data/             MySQL/Redis data when using stack mode
"

  log "Files written to $(pwd)"
  log "Next steps:"
  printf '  cd %s\n' "$(pwd)"
  printf '  %s pull\n' "$compose"
  printf '  %s up -d\n' "$compose"
  printf '  %s logs -f app\n' "$compose"
  log "After startup, open ${site_url}"
}

main "$@"
