.PHONY: all build run test clean tidy docker-build proto help

PROJECT_NAME := gokit
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

## proto: 重新生成 protobuf 代码（需安装 protoc / protoc-gen-go / protoc-gen-go-grpc）
proto:
	protoc --go_out=. --go_opt=module=goKit \
		--go-grpc_out=. --go-grpc_opt=module=goKit \
		api/proto/authz/v1/authz.proto

## clean: 清理编译产物
clean:
	rm -rf bin/
	rm -f coverage.out
