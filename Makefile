# GoX 构建与测试入口（需 Go 1.27+）
GO := go
TOOLCHAIN := GOTOOLCHAIN=go1.27.4
CGO := CGO_ENABLED=1
# GX 组件转译用的工具链（.gx → .go）。与 widgets/*.gx 的编译产物保持一致。
GXTOOLCHAIN ?= GOTOOLCHAIN=go1.27.4

.PHONY: build test lsp-test clean all gxbuild

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

## gxbuild: 把 widgets 下所有 .gx 源转译为同目录同名 .go（GX 组件库自举）
## 每当修改 widgets/*.gx 后必须执行，保持提交产物与源同步。
gxbuild:
	@for f in widgets/*.gx; do \
	  [ -f "$$f" ] || continue; \
	  echo "转译 $$f -> $${f%.gx}.go"; \
	  $(GXTOOLCHAIN) $(GO) run ./cmd/gox build "$$f" -o "$${f%.gx}.go" || exit 1; \
	done
	@echo "GX 组件转译完成"

## clean: 清理构建产物
clean:
	rm -f gox gox-lsp libgox.so libgox.h
