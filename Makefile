.PHONY: up down clean web web_dev run build

APP_NAME = server
BUILD_DIR = $(PWD)/build

up:
	docker compose up -d

down:
	docker compose down

clean:
	rm -rf ./build
	rm -rf ./web/dist

# 构建前端 (Vue3 + Vite 多页) -> web/dist, 由 Go embed 内嵌
web:
	cd web && pnpm install && pnpm build

# 前端开发服务器 (代理 /api 到 :8099)
web_dev:
	pnpm --dir web run dev

# 构建前端后启动后端 (dev 模式)
run: web
	go run ./cmd --app-env dev --bind :8099

start:
	go run ./cmd --app-env dev --bind :8099

# 构建后端 (内嵌已构建的 web/dist)
build: web
	go build -ldflags="-w -s" -o $(BUILD_DIR)/$(APP_NAME) ./cmd
