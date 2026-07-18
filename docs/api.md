# API 文档

## 基础

- 基础 URL：`http://127.0.0.1:8080`
- 请求体：`application/json`
- 响应体：`application/json; charset=utf-8`

## 认证

配置了密钥后，所有请求需携带 `X-Stow-Key` 头，或使用 `Authorization: Bearer ...`。密钥格式为 `stow-xxxxxx`（6 位数字字母）。

```bash
curl -H "X-Stow-Key: stow-aB12Cd" http://127.0.0.1:8080/health
# 也可以使用：
curl -H "Authorization: Bearer stow-aB12Cd" http://127.0.0.1:8080/health
```

未认证时返回 **401**。

## 数据模型

### Item（物品）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 物品 ID |
| `name` | string | 物品名称 |
| `category_id` | integer or null | 分类 ID |
| `category` | string | 分类名称；未设置时为空字符串 |
| `location_id` | integer or null | 位置 ID |
| `location` | string | 位置名称；未设置时为空字符串 |
| `quantity` | integer | 当前库存数量 |
| `created_at` | string | 创建时间 (RFC3339) |
| `updated_at` | string | 更新时间 (RFC3339) |

### Category（分类）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 分类 ID |
| `name` | string | 分类名称（唯一） |

### Location（位置）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 位置 ID |
| `name` | string | 位置名称（唯一） |

### Movement（库存流水）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 流水 ID |
| `item_id` | integer | 物品 ID |
| `type` | string | 类型：`stock_in` / `stock_out` / `adjust` |
| `change` | integer | 变化数量（可正可负） |
| `quantity_after` | integer | 变动后库存 |
| `note` | string | 备注 |
| `created_at` | string | 创建时间 |

### Batch（批次）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | integer | 批次 ID |
| `item_id` | integer | 物品 ID |
| `quantity` | integer | 该批次剩余数量 |
| `expiration_date` | string or null | 到期日期（格式 `YYYY-MM-DD`） |
| `created_at` | string | 创建时间 |

每次入库会创建一个批次。出库时按到期日期 **FEFO**（先到期先出）消耗批次；没有到期日期的批次最后消耗，同一到期日期再按创建时间和 ID 排序。盘点会清空所有批次并新建一个无到期日期的批次。

## 物品管理

### 物品列表

```http
GET /api/items
```

**响应 200：**
```json
[
  {
    "id": 1,
    "name": "大米",
    "category_id": 1,
    "category": "食品",
    "location_id": 1,
    "location": "厨房",
    "quantity": 10,
    "created_at": "2026-07-17T12:00:00Z",
    "updated_at": "2026-07-17T12:00:00Z"
  }
]
```

### 创建物品

```http
POST /api/items
Content-Type: application/json

{"name":"大米","category_id":1,"location_id":1}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `name` | 是 | 物品名称 |
| `category_id` | 否 | 分类 ID，优先于 `category` |
| `location_id` | 否 | 位置 ID，优先于 `location` |
| `category` | 否 | 分类名称；不存在时会自动创建分类 |
| `location` | 否 | 位置名称；不存在时会自动创建位置 |

`category`/`location` 是兼容字段。新客户端建议先创建分类和位置，再传递 `category_id`/`location_id`。未提供对应字段时物品不关联分类或位置；同时提供 ID 和名称时以 ID 为准。

**响应 201：** 返回创建的物品对象

### 物品详情

```http
GET /api/items/{id}
```

**响应 200：** 返回物品对象  
**响应 404：** 物品不存在

### 修改物品

```http
PUT /api/items/{id}
Content-Type: application/json

{"name":"大米","category_id":1,"location_id":1}
```

参数与创建一致。**响应 200：** 返回修改后的物品对象。

### 删除物品

```http
DELETE /api/items/{id}
```

仅允许删除零库存物品。**响应 204：** 删除成功。**响应 409：** 物品仍有库存。

## 库存操作

### 入库

创建一个新批次。入库后可通过 `GET /api/items/{id}/batches` 查看批次列表。

```http
POST /api/items/{id}/stock-in
Content-Type: application/json

{"quantity":5,"note":"本周补货","expiration_date":"2027-06-01"}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `quantity` | 是 | 入库数量，必须大于 0 |
| `note` | 否 | 备注 |
| `expiration_date` | 否 | 到期日期，格式 `YYYY-MM-DD` |

**响应 200：** 返回更新后的物品对象。

### 出库

```http
POST /api/items/{id}/stock-out
Content-Type: application/json

{"quantity":2,"note":"日常使用"}
```

`quantity` 必须大于 0。库存不足时返回 **409**，库存不变。

**响应 200：** 返回更新后的物品对象。

### 盘点

```http
POST /api/items/{id}/adjust
Content-Type: application/json

{"quantity":4,"note":"实际盘点"}
```

`quantity` 为实际盘点数量（非负数）。将账面库存修正为目标值。

**响应 200：** 返回更新后的物品对象。如果数量未变化则不产生流水。

### 批次列表

```http
GET /api/items/{id}/batches
```

**响应 200：** 返回批次列表，按 FEFO 顺序排列；无到期日期的批次排在最后。

```json
[
  {
    "id": 1,
    "item_id": 1,
    "quantity": 5,
    "expiration_date": "2027-01-15",
    "created_at": "2026-07-17T12:00:00Z"
  },
  {
    "id": 2,
    "item_id": 1,
    "quantity": 3,
    "expiration_date": null,
    "created_at": "2026-07-17T12:30:00Z"
  }
]
```

### 库存流水

```http
GET /api/items/{id}/movements
```

**响应 200：** 返回流水列表，按时间倒序。

## 分类管理

### 分类列表

```http
GET /api/categories
```

**响应 200：**
```json
[
  {
    "id": 1,
    "name": "饮料酒水"
  }
]
```

### 创建分类

```http
POST /api/categories
Content-Type: application/json

{"name":"饮料酒水"}
```

`name` 必填且唯一。**响应 201。** 重名返回 **409**。

### 分类详情

```http
GET /api/categories/{id}
```

### 修改分类

```http
PUT /api/categories/{id}
Content-Type: application/json

{"name":"饮品"}
```

### 删除分类

```http
DELETE /api/categories/{id}
```

仅允许删除未被任何物品引用的分类。**响应 204。** 有物品引用时返回 **409**。

## 位置管理

### 位置列表

```http
GET /api/locations
```

### 创建位置

```http
POST /api/locations
Content-Type: application/json

{"name":"冰箱"}
```

### 位置详情

```http
GET /api/locations/{id}
```

### 修改位置

```http
PUT /api/locations/{id}
Content-Type: application/json

{"name":"冷藏柜"}
```

### 删除位置

```http
DELETE /api/locations/{id}
```

仅允许删除未被任何物品引用的位置。**响应 204。** 有物品引用时返回 **409**。

## 版本

```http
GET /version
```

**响应 200：**
```json
{"version":"1.0.2"}
```

## 数据导出

```http
GET /api/export
```

**响应 200：**
```json
{
  "version": 2,
  "categories": [{"id": 1, "name": "食品"}],
  "locations": [{"id": 1, "name": "厨房"}],
  "items": [
    {
      "name": "大米",
      "category_id": 1,
      "location_id": 1,
      "batches": [
        {"quantity": 5, "expiration_date": "2027-06-01"}
      ]
    }
  ]
}
```

## 数据导入

```http
POST /api/import
Content-Type: application/json

{
  "version": 2,
  "categories": [{"name": "食品"}],
  "locations": [{"name": "厨房"}],
  "items": [
    {
      "name": "大米",
      "category_id": 1,
      "location_id": 1,
      "batches": [
        {"quantity": 5, "expiration_date": "2027-06-01"}
      ]
    }
  ]
}
```

`version` 必须为整数 `2`。分类和位置按名称去重，物品中的 `category_id`/`location_id` 会通过导出数据中的 ID 映射到本地 ID；引用不存在的 ID 或批次数量小于等于 0 时拒绝整个导入。导入在一个事务中执行，失败时不会保留部分数据。

**响应 200：**
```json
{"status":"imported"}
```

## 健康检查

```http
GET /health
```

**响应 200：**
```json
{"status":"ok"}
```

数据库不可用时返回 **503**。

## 通用错误

| 状态码 | 说明 |
| --- | --- |
| 400 | 请求参数错误 |
| 404 | 资源不存在 |
| 409 | 冲突（库存不足、仍有库存、重名、被引用等） |
| 401 | 未认证或密钥无效 |
| 500 | 服务端内部错误 |

错误响应格式：

```json
{"error":"描述信息"}
```
