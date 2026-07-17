# Stow Core 最小实现计划

## 目标

从空目录重建一个可以直接运行的家庭物品库存服务，只解决以下问题：

1. 物品管理：新增、列表、详情、修改、删除。
2. 入库：增加物品库存。
3. 出库：减少物品库存，库存不足时整个操作失败。
4. 盘点：把账面库存修正为实际数量。
5. 查询单个物品的库存变动流水。

## 明确不做

- MCP
- 用户、登录、Token 和权限
- 幂等键
- 批次、生产日期、有效期和 FEFO
- 采购、提醒、报表和仪表盘
- 备份与恢复接口
- OpenAPI
- 复杂迁移框架
- 多数据库支持
- 前端页面

## 技术方案

- Go 标准库 `net/http`
- Go 标准库 `database/sql`
- `modernc.org/sqlite` 纯 Go SQLite 驱动，不需要 CGO
- 单进程、单 SQLite 文件
- JSON REST API

## 数据模型

### items

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER | 主键 |
| `name` | TEXT | 物品名称，必填 |
| `category` | TEXT | 分类，可空 |
| `unit` | TEXT | 单位，必填 |
| `location` | TEXT | 存放位置，可空 |
| `quantity` | INTEGER | 当前库存，必须大于等于 0 |
| `created_at` | TEXT | UTC 创建时间 |
| `updated_at` | TEXT | UTC 更新时间 |

### movements

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER | 主键 |
| `item_id` | INTEGER | 物品 ID |
| `type` | TEXT | `stock_in`、`stock_out` 或 `adjust` |
| `change` | INTEGER | 库存变化量，可正可负 |
| `quantity_after` | INTEGER | 操作后的库存 |
| `note` | TEXT | 备注，可空 |
| `created_at` | TEXT | UTC 操作时间 |

## 业务规则

- 新物品初始库存固定为 0。
- 入库数量必须大于 0。
- 出库数量必须大于 0。
- 库存不足时禁止出库，不允许部分成功。
- 盘点后的实际数量必须大于等于 0。
- 盘点数量与账面数量相同时不写流水。
- 入库、出库和盘点都在同一事务中更新物品库存并写入流水。
- 物品有库存时禁止删除；库存为 0 时允许删除，同时删除其历史流水。

## REST API

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

## 实施顺序

1. 建立新目录、Go 模块、配置和 `.gitignore`。
2. 实现 SQLite 初始化和建表。
3. 实现物品 CRUD。
4. 实现入库、出库和盘点事务。
5. 实现 REST 路由与统一 JSON 错误。
6. 添加业务测试。
7. 运行 `gofmt`、`go test ./...`、`go vet ./...` 和 `go build ./cmd/server`。

## 验收标准

- 服务首次启动自动创建数据库和表。
- 可以完整完成：创建物品 -> 入库 -> 出库 -> 盘点 -> 查询流水。
- 任何操作都不能产生负库存。
- 业务操作失败时库存和流水都不发生变化。
- 测试、静态检查和构建全部通过。
