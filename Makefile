.PHONY: all build run test clean tidy docker-build help

PROJECT_NAME := nexus
APP_NAME := server
MAIN_FILE := cmd/server/main.go

# 默认目标
all: build

## help: 显示帮助信息
help:
	@echo "Usage:"
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

## tidy: 整理 Go 依赖
tidy:
	go mod tidy

## run: 本地运行项目
run:
	go run $(MAIN_FILE)

## build: 编译二进制文件
build: prepare-web
	@echo "Building $(APP_NAME)..."
	go build -o bin/$(APP_NAME) $(MAIN_FILE)

## prepare-web: 准备 Web 文件用于 embed
prepare-web:
	@echo "准备 Web 文件用于 embed..."
	@mkdir -p cmd/server/web
	@if [ -d "web/dist" ]; then \
		cp -r web/dist cmd/server/web/; \
		echo "Web 文件已复制到 cmd/server/web/dist"; \
	else \
		echo "警告: web/dist 目录不存在，请先构建前端 (cd web && npm run build)"; \
	fi

## test: 运行单元测试
test:
	go test ./... -v

## docker-build: 构建 Docker 镜像
docker-build:
	docker build -t $(PROJECT_NAME):latest .

## clean: 清理编译产物
clean:
	rm -rf bin/
	rm -rf cmd/server/web/
	rm -f coverage.out
