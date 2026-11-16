# 生成代码契约

> 返回 [zcmodel 文档地图](zcmodel.md)。本文描述产物“长什么样、有哪些保证”，是实现与用户之间的稳定契约。

## 命名与文件布局

以表 `user_order` 为例：

- 输出文件：`{OutputDir}/user_order.go`（设置 `Input.FileName` 时为 `{OutputDir}/{FileName}.go`）
- Entity 名：`UserOrderEntity`（表名 PascalCase + `Entity`）
- DO 名：`UserOrderDO`（表名 PascalCase + `DO`）

新建文件的布局：

```text
原文件头（build tags、文件级注释；新建文件无）
package 行（新建文件取 OutputDir 绝对路径的末级目录名）
imports

// Entity 生成代码（结构体注释 + 结构体 + ToDO 方法）
// DO 生成代码（结构体注释 + 结构体 + ToEntity 方法）
```

存量文件走「骨架保持式合并」：顶层声明的排列结构（有哪些声明、顺序、各自位置）原封不动，生成声明在各自原位替换、用户声明原位保留——因此再生成的产物顺序等于原文件顺序，不按上图的固定块序重排（见 [增量再生成](regeneration.md)）。

> 生成文件不写入任何生成标记/生成器声明（既不用 Go 官方的 `// Code generated ... DO NOT EDIT.` 标记，也不自拟分区声明）：官方标记会使 staticcheck、golangci-lint 等工具**整体跳过该文件**，而本文件包含用户代码、不应被豁免静态检查；而「生成区替换 / 用户区保留」的契约本身由 `Generate` 的增量再生成行为保证，无需在文件里声明。

## Entity 与 DO 结构体

- tag 顺序固定为：**json → {ColumnTagName} → primary_key → ddl → description**，各 tag 数据为空时跳过：
  - `json` tag 仅在 `JsonTagValue` 非空时生成；
  - `{ColumnTagName}` tag 仅在 `ColumnTagName` 非空时生成（避免产生空 tag 名 `:"colname"`）；tag 值为列名，经 `formatStructTag` 转义（见 [字段、tag 与 ddl](tags-ddl-index.md)）；
  - `primary_key:"true"` 仅在 `Column.PrimaryKey` 为 true 时生成（联合主键多列均标记）；主键在 DDL 中是表级约束，不并入 `ddl` 片段，另由 doc 注释的索引块呈现；
  - `ddl` tag 仅在 `Column.Type` 非空时生成（规则见 [ddl 片段](tags-ddl-index.md)）；
  - `description` tag 仅在列注释非空时生成；
- 字段名与类型按 gofmt 风格对齐（宽度取最长者）；
- DO 的字段类型统一为 `any`，tag 与 Entity 完全一致。

示例（表 `user_order`，`ColumnTagName` 为 `db`，JSON tag 风格 `lowerCamel`，元数据取自 MySQL）：

```go
// UserOrderEntity test_db.user_order 订单表，entity结构体，常用于数据库读取操作。
//
// 索引:
//   - PRIMARY KEY (id)
//   - UNIQUE KEY uk_email (email)
//   - UNIQUE KEY uk_order_no (order_no)
//   - KEY idx_user_status (user_id, status)
type UserOrderEntity struct {
	ID        int64     `json:"id" db:"id" primary_key:"true" ddl:"bigint unsigned NOT NULL AUTO_INCREMENT" description:"主键"`
	OrderNo   string    `json:"orderNo" db:"order_no" ddl:"varchar(32) NOT NULL" description:"订单号"`
	UserID    int64     `json:"userId" db:"user_id" ddl:"bigint NOT NULL" description:"用户ID"`
	Email     string    `json:"email" db:"email" ddl:"varchar(64) NOT NULL" description:"用户邮箱"`
	Amount    float64   `json:"amount" db:"amount" ddl:"decimal(10,2) NOT NULL DEFAULT 0.00" description:"订单金额（保留两位小数）"`
	Status    string    `json:"status" db:"status" ddl:"enum('pending','paid','refunded') NOT NULL DEFAULT pending" description:"订单状态"`
	Remark    string    `json:"remark" db:"remark" ddl:"varchar(255) NULL" description:"备注（可为空）"`
	CreatedAt time.Time `json:"createdAt" db:"created_at" ddl:"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP" description:"创建时间"`
}

// UserOrderDO test_db.user_order 订单表，do结构体，常用于数据库写入操作。
//
// 索引:
//   - PRIMARY KEY (id)
//   - UNIQUE KEY uk_email (email)
//   - UNIQUE KEY uk_order_no (order_no)
//   - KEY idx_user_status (user_id, status)
type UserOrderDO struct {
	ID        any `json:"id" db:"id" primary_key:"true" ddl:"bigint unsigned NOT NULL AUTO_INCREMENT" description:"主键"`
	OrderNo   any `json:"orderNo" db:"order_no" ddl:"varchar(32) NOT NULL" description:"订单号"`
	UserID    any `json:"userId" db:"user_id" ddl:"bigint NOT NULL" description:"用户ID"`
	Email     any `json:"email" db:"email" ddl:"varchar(64) NOT NULL" description:"用户邮箱"`
	Amount    any `json:"amount" db:"amount" ddl:"decimal(10,2) NOT NULL DEFAULT 0.00" description:"订单金额（保留两位小数）"`
	Status    any `json:"status" db:"status" ddl:"enum('pending','paid','refunded') NOT NULL DEFAULT pending" description:"订单状态"`
	Remark    any `json:"remark" db:"remark" ddl:"varchar(255) NULL" description:"备注（可为空）"`
	CreatedAt any `json:"createdAt" db:"created_at" ddl:"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP" description:"创建时间"`
}
```

结构体 doc 注释由「表注释 + 索引块」组成；索引块的排序、格式与净化规则见 [索引块](tags-ddl-index.md)。

## ToDO / ToEntity 互转方法

两个方法签名对称，均支持传入已有实例复用（可变参数，传 nil 或不传则新建）：

```go
// Entity → DO：Entity 字段为值类型，直接赋给 DO 的 any 字段
func (e *UserOrderEntity) ToDO(userOrderDO ...*UserOrderDO) *UserOrderDO

// DO → Entity：DO 字段为 any，逐字段类型断言还原为具体类型；断言失败（含 nil）则跳过该字段，保留 Entity 原值
func (d *UserOrderDO) ToEntity(userOrderEntity ...*UserOrderEntity) *UserOrderEntity
```

> `ToEntity` 的跳过语义意味着：DO 中值为 nil 的字段不会覆盖 Entity 的已有值，天然适配“只写入了部分列”的场景。

## 包名

- 仅新建文件时：包名取输出目录**绝对路径**的末级目录名；相对路径 `.` 解析为当前目录的实际名称；位于根目录等无法推导时回退 `main`。
- 存量文件再生成：尊重原文件 `package` 声明，不强制改为目录推导值；`package` 行之前的 build tags/文件级注释按原文前置保留。
- `OutputDir` 的目录名因此应是合法 Go 包名；否则产物无法通过落盘前的 `go/format` 自校验并报错。

## 产物安全

- 落盘前对完整内容执行 `go/format.Source` 语法自校验，任何非法产物都不写出；
- 同目录临时文件 + rename 原子替换，保留原文件权限位；
- 既有文件无法被 `go/parser` 解析时直接报错，绝不覆盖用户代码。

三重防线的实现与失败分支见 [生成流水线](pipeline.md)。

## 相关

- 再生成时会替换/保留什么：[增量再生成](regeneration.md)
- 字段级 tag、ddl 与索引块：[字段、tag 与 ddl](tags-ddl-index.md)
- 算法实现：[骨架保持式合并](skeleton-merge.md)
