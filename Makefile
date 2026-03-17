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
build:
	@echo "Building $(APP_NAME)..."
	go build -o bin/$(APP_NAME) $(MAIN_FILE)

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
