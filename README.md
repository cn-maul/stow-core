# Stow Core

Stow Core 是一个最小化的本地物品库存服务，提供物品管理、入库、出库和盘点功能。

当前版本有意保持简单：没有 MCP、用户系统、权限、幂等和前端页面。

## 快速开始

```bash
go run ./cmd/server
```

默认监听 `127.0.0.1:8080`，数据库文件为 `data/stow.db`。

可以通过环境变量修改：

```text
STOW_ADDR=127.0.0.1:8080
STOW_DB=data/stow.db
```

## 开发验证

```bash
go test ./...
go vet ./...
go build -o stow-core.exe ./cmd/server
```

## 文档

- [API 文档](docs/api.md) — 所有接口的详细说明与示例

完整范围和验收标准见 [`MINIMAL_PLAN.md`](MINIMAL_PLAN.md)。
