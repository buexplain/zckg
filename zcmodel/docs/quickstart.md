# 快速开始

> 返回 [zcmodel 文档地图](zcmodel.md)。本文用最小成本跑通一次生成，并逐项解释 `Input`。

## 1. 最小可运行示例

`Generate` 接收一个 `Input`（表名 + 列定义），在 `OutputDir` 下写出 `{FileName}.go`。下面这个程序一次生成 `user_order.go`：

```go
package main

import "github.com/buexplain/zckg/zcmodel"

func main() {
	// Nullable 与 Default 为指针：nil 表示「未知 / 无默认值」，需辅助函数取字面量地址
	nullable := func(v bool) *bool { return &v }
	strPtr := func(v string) *string { return &v }

	err := zcmodel.Generate(zcmodel.Input{
		OutputDir:        "./model",   // 目录名 "model" 即生成文件的包名
		Database:         "test_db",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		TableComment:     "订单表",
		ColumnTagName:    "db",                  // 与 zcdb 的列映射标签保持一致
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns: []*zcmodel.Column{
			{Name: "id", Type: "bigint unsigned", Nullable: nullable(false), PrimaryKey: true, Extra: "auto_increment", Comment: "主键"},
			{Name: "order_no", Type: "varchar(64)", Nullable: nullable(false), Comment: "订单号"},
			{Name: "amount", Type: "decimal(10,2)", Nullable: nullable(false), Default: strPtr("0.00")},
			{Name: "remark", Type: "varchar(255)", Nullable: nullable(true), Comment: "备注"},
			{Name: "created_at", Type: "datetime", Nullable: nullable(false), Default: strPtr("CURRENT_TIMESTAMP"), Comment: "创建时间"},
		},
		// 索引块（可选）：留空则结构体 doc 注释中不生成索引清单
		Indexes: []zcmodel.IndexInfo{
			{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Primary: true},
			{Name: "uk_order_no", Columns: []string{"order_no"}, Unique: true},
		},
	})
	if err != nil {
		panic(err)
	}
	// 生成 ./model/user_order.go：UserOrderEntity + UserOrderDO + ToDO/ToEntity
}
```

## 2. 生成出来长什么样

生成文件由 `package model`、必要的 import、Entity 结构体与 `ToDO` 方法、DO 结构体与 `ToEntity` 方法组成。下面是上面示例的 Entity 部分（完整产物另含 `package model`、`import "time"` 与两个方法体）：

```go
// UserOrderEntity test_db.user_order 订单表，entity结构体，常用于数据库读取操作。
//
// 索引:
//   - PRIMARY KEY (id)
//   - UNIQUE KEY uk_order_no (order_no)
type UserOrderEntity struct {
	ID        int64     `json:"id" db:"id" primary_key:"true" ddl:"bigint unsigned NOT NULL AUTO_INCREMENT" description:"主键"`
	OrderNo   string    `json:"orderNo" db:"order_no" ddl:"varchar(64) NOT NULL" description:"订单号"`
	Amount    float64   `json:"amount" db:"amount" ddl:"decimal(10,2) NOT NULL DEFAULT 0.00"`
	Remark    string    `json:"remark" db:"remark" ddl:"varchar(255) NULL" description:"备注"`
	CreatedAt time.Time `json:"createdAt" db:"created_at" ddl:"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP" description:"创建时间"`
}
```

DO 结构体字段类型全部是 `any`，tag 与 Entity 完全一致；`ToDO` 逐字段直接赋值，`ToEntity` 用类型断言还原并跳过失败字段。完整规则见 [生成代码契约](generated-code.md)。

## 3. 逐项解释 Input

- `OutputDir`：输出目录，不得为空，不存在时自动创建；**目录名即新建文件的包名**（相对路径 `.` 也解析为实际目录名，根目录等无法推导时回退 `main`）。存量文件再生成时尊重原 `package` 声明。
- `Database`：只影响结构体 doc 注释里的限定名（`test_db.user_order`）；为空时只显示表名。
- `Dialect`：`mysql` / `postgres` / `sqlite`，决定列类型映射表与归一化规则。
- `TableName`：表名，`FileName` 为空时原样作文件名，同时经 `ToPascalCase` 推导 `UserOrderEntity` / `UserOrderDO`。表名会做路径穿越、Windows 非法字符、首字符与导出标识符校验。
- `FileName`：可选，替换文件名（不含 `.go`）。表名触发 Go 文件名构建规则（如 `order_linux`、`user_test`、`_tmp`）时用它改名。
- `TableComment`：进入结构体 doc 注释；为空时用“表”。
- `ColumnTagName`：列映射 tag 名（如 `db`）；为空则不生成列映射 tag。
- `JsonTagValueCase`：json tag 风格；留空不自动生成 json tag，但单列显式 `StructFieldInfo.JsonTagValue` 仍会生成。
- `Columns`：列定义；为空时生成仅含空结构体与互转方法的文件；元素不得为 `nil`。
- `Indexes`：索引元数据，非空时渲染为结构体 doc 注释中的索引块；排序固定为 PRIMARY → UNIQUE（名字典序）→ 普通（名字典序）。

`Nullable` / `Default` 用指针区分“未知 / 无默认值”与零值：`Nullable == nil` 时 `ddl` 片段省略 NULL 标记；`Default == nil` 表示无默认值，非 nil 一律追加 `DEFAULT` 段（空串渲染为 `DEFAULT ''`）。字段级细节见 [字段、tag 与 ddl](tags-ddl-index.md)。

## 4. 配合 zcdb Schema 从真实数据库生成

`NewDBDao` 必须传五个参数：第四参数为列映射标签名（空串使用 `db`），第五参数为默认 SQL 短业务标识（此处传 `""` 保持无注释基线）。默认注释仅影响 Builder 的最终编译 SQL，DAO 原始 SQL 和 Schema 元数据查询不会自动追加，表/列注释读取及模型生成行为不变。第五参数不是表/列说明，不得透传外部输入、请求体或秘密；覆盖、清空及 Clone 行为见 [zcdb Builder 核心](../../zcdb/docs/reference/builder-core.md)的 Comment 小节。

```go
package main

import (
	"context"

	"github.com/buexplain/zckg/zcdb"
	"github.com/buexplain/zckg/zcmodel"
	_ "github.com/go-sql-driver/mysql"
)

func main() {
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
	})
	if err != nil {
		panic(err)
	}
	dao, err := zcdb.NewDBDao(pool, "mysql", nil, "", "")
	if err != nil {
		panic(err)
	}
	defer dao.Close()

	// 1. 读取表结构（列名、列类型、列注释、可空性、主键标记与 MySQL EXTRA）
	inspector, err := dao.Schema()
	if err != nil {
		panic(err)
	}
	cols, err := inspector.Columns(context.Background(), "user_order")
	if err != nil {
		panic(err)
	}
	columns := make([]*zcmodel.Column, 0, len(cols))
	for _, c := range cols {
		nullable := c.Nullable // 取副本地址：Column.Nullable 为 *bool，而 SchemaInspector 给出的可空性恒已知
		columns = append(columns, &zcmodel.Column{
			Name: c.Name, Type: c.Type, Comment: c.Comment,
			Nullable: &nullable, Default: c.Default, PrimaryKey: c.PrimaryKey, Extra: c.Extra,
		})
	}

	// 2. 读取索引（主键、唯一、普通索引）——生成结构体 doc 注释中的索引块
	indexes, err := inspector.Indexes(context.Background(), "user_order")
	if err != nil {
		panic(err)
	}
	idxInfos := make([]zcmodel.IndexInfo, 0, len(indexes))
	for _, i := range indexes {
		idxInfos = append(idxInfos, zcmodel.IndexInfo{
			Name: i.Name, Columns: i.Columns, Unique: i.Unique, Primary: i.Primary,
		})
	}

	// 3. 生成模型代码
	err = zcmodel.Generate(zcmodel.Input{
		OutputDir:        "./model",
		Database:         "test",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		ColumnTagName:    "db",
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns:          columns,
		Indexes:          idxInfos,
	})
	if err != nil {
		panic(err)
	}
}
```

一键生成的产物即“表的完整面貌”：每列的 `ddl` 片段（类型 / 可空 / 默认值 / MySQL `EXTRA`）、主键列的 `primary_key` tag，以及 Entity 与 DO 的 doc 注释中的索引块。索引列的特殊形态（MySQL 前缀索引 `col(n)`、表达式索引 `#expr`、PG 的 INCLUDE 列不输出、SQLite 合成主键行）见 [zcdb Schema 元数据查询](../../zcdb/docs/reference/schema.md)的 Indexes 小节。

## 5. 显式指定字段信息（覆盖自动推导）

`StructFieldInfo` 的每个字段都允许调用方预先指定，`Generate` 只对留空字段做自动推导：

```go
// JSON 列映射为自定义类型、特殊命名等场景，预先填好 StructFieldInfo 即可
func example() {
	columns := []*zcmodel.Column{
		{
			Name: "extra", Type: "json", Comment: "扩展信息",
			StructFieldInfo: zcmodel.StructFieldInfo{
				Name:   "Extra",
				Type:   "map[string]any", // 手工指定类型，跳过映射表
				Import: "",                // 内置类型无需 import
			},
		},
	}
	_ = columns
}
```

## 6. 再生成：在生成文件里写代码

可以在产物中自由添加自定义方法、常量、注释。表结构变化后再次调用 `Generate` 即可原地更新：生成声明被替换、用户声明与注释保留在原位置、缺失 import 自动补齐、不再引用的 import 自动移除。契约与边界见 [增量再生成](regeneration.md)，算法细节见 [骨架保持式合并](skeleton-merge.md)。

## 下一步

- 字段与 tag 规则：[字段、tag 与 ddl](tags-ddl-index.md)
- 列类型与命名：[类型映射](types-and-naming.md)
- 完整 API 与错误表：[对外 API](api.md)
- 产物契约：[生成代码契约](generated-code.md)
