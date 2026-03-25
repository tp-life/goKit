.PHONY: all build run build-tui run-tui build-tradeprobe run-tradeprobe test clean tidy docker-build help

PROJECT_NAME := nexus
APP_NAME := server
MAIN_FILE := cmd/server/main.go
TUI_MAIN_FILE := cmd/tui/main.go
TRADE_PROBE_MAIN_FILE := cmd/tradeprobe/main.go

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

## run-tui: 启动 TUI（需先启动 API 服务）
run-tui:
	go run $(TUI_MAIN_FILE)

## run-tradeprobe: 运行交易探针（可额外追加 ARGS='--action inspect'）
run-tradeprobe:
	go run $(TRADE_PROBE_MAIN_FILE) $(ARGS)

## build: 编译二进制文件
build:
	@echo "Building $(APP_NAME)..."
	go build -o bin/$(APP_NAME) $(MAIN_FILE)

## build-tui: 编译 TUI 二进制
build-tui:
	@echo "Building tui..."
	go build -o bin/tui $(TUI_MAIN_FILE)

## build-tradeprobe: 编译交易探针二进制
build-tradeprobe:
	@echo "Building tradeprobe..."
	go build -o bin/tradeprobe $(TRADE_PROBE_MAIN_FILE)

## test: 运行单元测试
test:
	go test ./... -v

## docker-build: 构建 Docker 镜像
docker-build:
	docker build -t $(PROJECT_NAME):latest .

## clean: 清理编译产物
clean:
	rm -rf bin/
	rm -f coverage.out
