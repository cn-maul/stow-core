# Stow Core

Stow Core 是一个本地物品库存服务，支持分类、位置、批次、到期日期管理。

## 启动

```bash
go run ./cmd/server
```

服务会读取当前目录下的 `stow.config.json`。

## 配置文件

```json
{
  "addr": "127.0.0.1:8080",
  "db": "data/stow.db",
  "keys": ["stow-aB12Cd"]
}
```

- `addr`：监听地址，默认 `127.0.0.1:8080`
- `db`：数据库路径，默认 `data/stow.db`
- `keys`：认证密钥列表，格式 `stow-xxxxxx`（6 位数字字母），留空则不认证

环境变量 `STOW_CONFIG` 可指定配置文件路径。

## 认证

配置了 `keys` 后，所有请求需携带 `X-Stow-Key` 头：

```bash
curl -H "X-Stow-Key: stow-aB12Cd" http://127.0.0.1:8080/health
```

## 开发验证

```bash
go test ./...
go vet ./...
go build -o stow-core.exe ./cmd/server
```

## 文档

- [API 文档](docs/api.md) — 所有接口的详细说明与示例
