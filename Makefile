# GoX 后端 / 命令行工具构建与测试入口（需 Go 1.27+）
# 核心语言库（编译器、LSP、widgets、runtime）已拆至 gox-core 仓，
# 本 Makefile 只负责本仓保留的后端模块与命令行工具。
GO := go
TOOLCHAIN := GOTOOLCHAIN=go1.27.4
CGO := CGO_ENABLED=1

# 本仓含 Go 源码的模块（顶层 module 仅作 go.work 容器，无包）。
MODULES := backend/rust/go backend/skia backend/web cmd/gox-tui cmd/gox-idea cmd/gox-term

.PHONY: build test clean all

all: build

## build: 构建三个命令行工具（各自独立 module）
build:
	$(TOOLCHAIN) $(GO) build -o gox-tui ./cmd/gox-tui
	$(TOOLCHAIN) $(GO) build -o gox-idea ./cmd/gox-idea
	$(TOOLCHAIN) $(GO) build -o gox-term ./cmd/gox-term
	@echo "构建完成: gox-tui gox-idea gox-term"

## test: 逐个模块运行测试（go.work 的 ./... 不递归匹配子模块）
test:
	@set -e; for m in $(MODULES); do \
	  echo "==> $$m"; \
	  (cd $$m && $(TOOLCHAIN) $(GO) test ./...) || exit 1; \
	done
	@echo "全部模块测试通过"

## clean: 清理构建产物
clean:
	rm -f gox-tui gox-idea gox-term
