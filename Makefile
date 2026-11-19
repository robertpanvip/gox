# GoX 构建与测试入口（需 Go 1.27+）
GO := go
TOOLCHAIN := GOTOOLCHAIN=go1.27.3
CGO := CGO_ENABLED=1

.PHONY: build test lsp-test clean all

all: build

## build: 构建 gox、gox-lsp、libgox.so
build:
	$(TOOLCHAIN) $(GO) build -o gox ./cmd/gox
	$(TOOLCHAIN) $(GO) build -o gox-lsp ./cmd/gox-lsp
	$(TOOLCHAIN) $(CGO) $(GO) build -buildmode=c-shared -o libgox.so ./bindings/cabi
	@echo "构建完成: gox gox-lsp libgox.so"

## test: 转译测试（内置 24 项自检）+ Go 单元测试
test:
	$(TOOLCHAIN) $(GO) run ./cmd/gox test
	$(TOOLCHAIN) $(GO) test ./...

## lsp-test: LSP 端到端驱动脚本
lsp-test:
	$(TOOLCHAIN) $(GO) test ./internal/lsp/... -v
	bash testdata/lsp-e2e.sh

## clean: 清理构建产物
clean:
	rm -f gox gox-lsp libgox.so libgox.h
