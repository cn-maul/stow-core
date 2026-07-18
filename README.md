# Stow Core

Stow Core 是一个本地物品库存服务，面向家庭、工作室和小型场景，使用 Go 和 SQLite 提供轻量级 REST API。

项目支持物品、分类、存放位置、库存批次、到期日期和库存流水管理，并提供数据导入导出、API 密钥认证和一个简单的浏览器调试页面。

## 快速开始

```bash
go run ./cmd/server
```

服务默认读取当前目录下的 `stow.config.json`，数据库默认创建在 `data/stow.db`。

也可以通过环境变量指定配置文件：

```bash
STOW_CONFIG=/path/to/stow.config.json go run ./cmd/server
```

Windows PowerShell：

```powershell
$env:STOW_CONFIG = "C:\path\to\stow.config.json"
go run ./cmd/server
```

## 配置示例

```json
{
  "addr": "127.0.0.1:8080",
  "db": "data/stow.db",
  "keys": ["stow-aB12Cd"]
}
```

配置 `keys` 后，请求需要携带 `X-Stow-Key` 或 `Authorization: Bearer` 认证头；留空则不启用认证。

## 项目验证

```bash
go test ./...
go vet ./...
go build ./cmd/server
```

## 文档

- [API 文档](docs/api.md)：接口、数据模型、认证、库存规则、导入导出格式和错误响应的完整说明
