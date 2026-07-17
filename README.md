# Stow Core

Stow Core 是一个最小化的本地物品库存服务，提供物品管理、入库、出库和盘点功能。

当前版本有意保持简单：没有 MCP、用户系统、权限、批次、有效期、幂等和前端页面。

## 功能

- 物品新增、列表、详情、修改和删除
- 入库
- 出库，库存不足时拒绝整个操作
- 盘点，将账面库存修正为实际数量
- 查询单个物品的库存变动流水
- SQLite 本地持久化

## 启动

```bash
go run ./cmd/server
```

默认监听 `127.0.0.1:8080`，数据库文件为 `data/stow.db`。

可以通过环境变量修改：

```text
STOW_ADDR=127.0.0.1:8080
STOW_DB=data/stow.db
```

## API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/health` | 健康检查 |
| `GET` | `/api/items` | 物品列表 |
| `POST` | `/api/items` | 创建物品 |
| `GET` | `/api/items/{id}` | 物品详情 |
| `PUT` | `/api/items/{id}` | 修改物品 |
| `DELETE` | `/api/items/{id}` | 删除零库存物品 |
| `POST` | `/api/items/{id}/stock-in` | 入库 |
| `POST` | `/api/items/{id}/stock-out` | 出库 |
| `POST` | `/api/items/{id}/adjust` | 盘点 |
| `GET` | `/api/items/{id}/movements` | 库存流水 |

### 创建物品

```bash
curl -X POST http://127.0.0.1:8080/api/items \
  -H "Content-Type: application/json" \
  -d '{"name":"大米","category":"食品","unit":"袋","location":"厨房"}'
```

### 入库

```bash
curl -X POST http://127.0.0.1:8080/api/items/1/stock-in \
  -H "Content-Type: application/json" \
  -d '{"quantity":5,"note":"本周补货"}'
```

### 出库

```bash
curl -X POST http://127.0.0.1:8080/api/items/1/stock-out \
  -H "Content-Type: application/json" \
  -d '{"quantity":2,"note":"日常使用"}'
```

### 盘点

```bash
curl -X POST http://127.0.0.1:8080/api/items/1/adjust \
  -H "Content-Type: application/json" \
  -d '{"quantity":4,"note":"实际盘点"}'
```

## 开发验证

```bash
go test ./...
go vet ./...
go build -o stow-core.exe ./cmd/server
```

完整范围和验收标准见 [`MINIMAL_PLAN.md`](MINIMAL_PLAN.md)。
