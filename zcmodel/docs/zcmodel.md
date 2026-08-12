# zcmodel 模型代码生成模块

## 概述

zcmodel 是一个数据库模型代码生成模块：输入一张表的结构信息（表名、字段名、字段类型、注释），输出一个可直接编译的 Go 源码文件，文件内包含：

- **Entity 结构体**：字段为具体 Go 类型（如 `int64`、`string`、`time.Time`），常用于数据库**读取**后的数据承载；
- **DO 结构体**：字段全部为 `any` 类型，常用于数据库**写入**（零值字段可置 nil 区分"未赋值"与"零值"）；
- **互转方法**：`Entity.ToDO()` 与 `DO.ToEntity()`，支持传入已有实例复用。

核心特性：

- **三种方言**：内置 MySQL / PostgreSQL / SQLite 的列类型 → Go 类型映射表；
- **命名自动转换**：任意风格的表名/列名（snake_case、camelCase、kebab-case 等）自动转为 Go 规范的 PascalCase，`id` 统一转为 `ID`；
- **增量再生成**：目标文件已存在时，通过 AST 解析移除旧的生成代码并重新生成，**用户自定义代码（含自定义方法）完整保留**；
- **注释安全**：字段注释写入 `description` tag 前经过转义净化，含反引号、换行、双引号的注释不会破坏生成代码的语法。

zcmodel 是纯代码生成工具，不连接数据库、不依赖数据库驱动。表结构信息可由调用方手工构造，也可通过 [zcdb](../../zcdb/docs/schema.md) 的 Schema 能力从真实数据库读取。

## 文件结构

```
zcmodel/
├── input.go                # 对外数据结构：Input、Column、StructFieldInfo、NameCase、Dialect
├── generate.go             # 入口 Generate：校验、补全字段信息、组装生成代码并写文件
├── build.go                # 代码拼装：结构体/方法字符串生成、AST 增量写文件（writeOrReplaceStruct）
├── toCase.go               # 命名风格转换：splitWords、toPascalCase、formatJSONTag 等
├── columnTypeToGoType.go   # 列类型归一化与方言类型映射表
├── *_test.go               # 单元测试与集成测试（集成测试对接真实 MySQL/PostgreSQL/SQLite）
└── docs/
    └── zcmodel.md          # 本文档
```

## 生成流程

```
                    ┌──────────────────────────┐
                    │      Generate(input)     │  唯一入口
                    └────────────┬─────────────┘
                                 │
      ① 校验 Dialect / JsonTagValueCase
                                 │
      ② 补全每列的 StructFieldInfo
         ├─ JsonTagValue 为空 → formatJSONTag（按 NameCase 转换）
         ├─ Name 为空        → toPascalCase（列名转 PascalCase）
         ├─ Type 为空        → formatStructFieldType（方言映射，未命中兜底 string）
         └─ Type 为 time.Time 且 Import 为空 → Import = "time"
                                 │
      ③ 生成代码字符串
         ├─ buildStruct：Entity（具体类型）/ DO（any 类型）
         ├─ buildToDOMethod / buildToEntityMethod：互转方法
         └─ 注释：表注释生成到结构体上方的 // 注释
                                 │
      ④ 写文件 {OutputDir}/{TableName}.go
         └─ writeOrReplaceStruct
            ├─ 文件不存在 → 直接创建（package + imports + 生成代码）
            └─ 文件已存在 → AST 解析：移除旧生成代码，
               保留用户代码并重新组织布局，缺失 import 自动补齐
```

## 对外 API

### Generate

```go
func Generate(input Input) error
```

唯一入口。根据 `Input` 生成 Entity/DO 结构体及互转方法，写入 `{OutputDir}/{TableName}.go`。可能返回的错误：

| 错误信息 | 触发场景 |
|---|---|
| `不支持的数据库方言: xxx` | `Dialect` 不是 mysql / postgres / sqlite |
| `invalid json tag value case` | `JsonTagValueCase` 非空但不是合法的 `NameCase` 枚举值 |
| `创建输出目录失败: ...` | `OutputDir` 无法创建 |
| `生成结构体失败: ...` | 目标文件存在但无法被 `go/parser` 解析（用户代码有语法错误）等写文件失败场景 |

### Input

| 字段 | 类型 | 说明 |
|---|---|---|
| `OutputDir` | `string` | 输出目录，不存在时自动创建。**目录名须为合法 Go 包名**（包名取目录名，无法推导时回退 `main`） |
| `Database` | `string` | 数据库名，仅用于生成结构体的注释 |
| `Dialect` | `Dialect` | 数据库方言，决定列类型映射表 |
| `TableName` | `string` | 表名。原样用作输出文件名（`{TableName}.go`），转 PascalCase 后推导结构体名 |
| `TableComment` | `string` | 表注释，生成到结构体注释中；为空时使用"表" |
| `ColumnTagName` | `string` | 列映射 tag 的名称（如 `"column"`、`"db"`），与 zcdb 的列映射标签对应 |
| `JsonTagValueCase` | `NameCase` | JSON tag 的命名风格；**空值表示不生成 json tag** |
| `Columns` | `[]*Column` | 表的所有字段 |

### Column / StructFieldInfo

```go
type Column struct {
    Name            string          // 列名
    Type            string          // 列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"）
    Comment         string          // 列注释，生成 description tag
    StructFieldInfo StructFieldInfo // 生成结构体字段时的信息
}

type StructFieldInfo struct {
    Name         string // 结构体字段名；留空则由列名自动推导（toPascalCase）
    Type         string // 结构体字段类型；留空则由列类型自动映射（未命中兜底 string）
    Import       string // Type 对应的 import 路径（如 time.Time 需 "time"）；留空不引入包
    JsonTagValue string // json tag 的值；留空时按 JsonTagValueCase 自动推导
}
```

**显式优先**：`StructFieldInfo` 的每个字段都允许调用方预先指定，`Generate` 只对留空的字段做自动推导。这使得特殊列（如 JSON 列想映射为自定义结构体类型）可以完全手工控制。

### NameCase

JSON tag 值的命名风格枚举，`IsValid()` 判断合法性：

| 枚举值 | 字符串值 | 示例（列名 user_id） |
|---|---|---|
| `NameCaseLowerCamel` | `lowerCamel` | `userId` |
| `NameCaseUpperCamel` | `upperCamel` | `UserId` |
| `NameCaseLowerSnake` | `lowerSnake` | `user_id` |
| `NameCaseUpperSnake` | `upperSnake` | `USER_ID` |
| `NameCaseLowerKebab` | `lowerKebab` | `user-id` |
| `NameCaseUpperKebab` | `upperKebab` | `USER-ID` |

### Dialect

| 枚举值 | 字符串值 |
|---|---|
| `DialectMysql` | `mysql` |
| `DialectPostgres` | `postgres` |
| `DialectSqlite` | `sqlite` |

## 生成代码详解

### 命名与文件布局

以表 `user_order` 为例：

- 输出文件：`{OutputDir}/user_order.go`
- Entity 名：`UserOrderEntity`（表名 PascalCase + `Entity`）
- DO 名：`UserOrderDO`（表名 PascalCase + `DO`）

文件最终布局（增量再生成时同样遵守）：

```
package {OutputDir 目录名}

imports（原有 import + 缺失的生成代码所需 import）

// Entity 生成代码（结构体注释 + 结构体 + ToDO 方法）
// Entity 自定义方法（用户代码，保留）

// DO 生成代码（结构体注释 + 结构体 + ToEntity 方法）
// DO 自定义方法（用户代码，保留）

// 其他用户代码（保留）
```

### Entity 与 DO 结构体

- tag 顺序固定为：**json → {ColumnTagName} → description**；
- `json` tag 仅在 `JsonTagValue` 非空时生成；`description` tag 仅在列注释非空时生成；
- 字段名与类型按 gofmt 风格对齐（宽度取最长者）；
- DO 的字段类型统一为 `any`，tag 与 Entity 完全一致。

示例（表 `user_order`，`ColumnTagName` 为 `db`，JSON tag 风格 `lowerCamel`）：

```go
// UserOrderEntity test_db.user_order 订单表，entity结构体，常用于数据库读取操作。
type UserOrderEntity struct {
	ID        int64     `json:"id" db:"id" description:"主键"`
	OrderNo   string    `json:"orderNo" db:"order_no" description:"订单号"`
	Amount    float64   `json:"amount" db:"amount"`
	CreatedAt time.Time `json:"createdAt" db:"created_at" description:"创建时间"`
}

// UserOrderDO test_db.user_order 订单表，do结构体，常用于数据库写入操作。
type UserOrderDO struct {
	ID        any `json:"id" db:"id" description:"主键"`
	OrderNo   any `json:"orderNo" db:"order_no" description:"订单号"`
	Amount    any `json:"amount" db:"amount"`
	CreatedAt any `json:"createdAt" db:"created_at" description:"创建时间"`
}
```

### ToDO / ToEntity 互转方法

两个方法签名对称，均支持传入已有实例复用（可变参数，传 nil 或不传则新建）：

```go
// Entity → DO：Entity 字段为值类型，直接赋给 DO 的 any 字段
func (e *UserOrderEntity) ToDO(userOrderDO ...*UserOrderDO) *UserOrderDO

// DO → Entity：DO 字段为 any，逐字段类型断言还原为具体类型；断言失败（含 nil）则跳过该字段，保留 Entity 原值
func (d *UserOrderDO) ToEntity(userOrderEntity ...*UserOrderEntity) *UserOrderEntity
```

> `ToEntity` 的跳过语义意味着：DO 中值为 nil 的字段不会覆盖 Entity 的已有值，天然适配"只写入了部分列"的场景。

## 列类型映射

`Generate` 对未显式指定 `Type` 的列执行：**归一化（normalizeColumnType）→ 查方言映射表（makeColumnTypeToGoTypeMap）→ 未命中兜底 `string`**。

### 归一化规则

列类型先统一小写、去除括号及其内容（长度、精度、枚举值列表）、压缩连续空格，再按方言做特有处理：

| 方言 | 特有处理 | 示例 |
|---|---|---|
| MySQL | 去除 `unsigned`、`zerofill` 修饰符 | `BIGINT(20) UNSIGNED` → `bigint` |
| PostgreSQL | 时区后缀规范化 | `timestamp with time zone` → `timestamptz`、`timestamp without time zone` → `timestamp` |
| SQLite | 去除类型名上的引号 | `"VARCHAR"(255)` → `varchar` |

> 括号内容会被整体移除，但括号后的内容保留，因此 `timestamp(6) with time zone` 能正确归一化为 `timestamptz`。

### MySQL 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| tinyint / smallint / mediumint / int / integer / year | `int` | char / varchar / tinytext / text / mediumtext / longtext | `string` |
| bigint | `int64` | enum / set / json | `string` |
| float / double / decimal / numeric | `float64` | date / time / datetime / timestamp | `time.Time` |
| bool / boolean | `bool` | binary / varbinary / tinyblob / blob / mediumblob / longblob | `[]byte` |

### PostgreSQL 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| smallint / int2 / integer / int / int4 / smallserial / serial | `int` | money | `string` |
| bigint / int8 / bigserial | `int64` | character varying / varchar / character / char / bpchar / text | `string` |
| real / float4 / double precision / float8 / numeric / decimal | `float64` | uuid / json / jsonb / inet / interval | `string` |
| boolean / bool | `bool` | date / time / timetz / timestamp / timestamptz | `time.Time` |
| bytea | `[]byte` | | |

### SQLite 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| tinyint / smallint / mediumint / int / integer / int2 | `int` | character / varchar / varying character / nchar / native character / nvarchar / text / clob | `string` |
| bigint / int8 / unsigned big int | `int64` | json | `string` |
| real / double / double precision / float / numeric / decimal | `float64` | date / datetime / timestamp | `time.Time` |
| boolean / bool | `bool` | blob | `[]byte` |

> 映射表键为**不带长度/精度后缀**的类型名；`time.Time` 会自动引入 `time` 包（调用方未显式指定 `Import` 时）。

## 命名转换规则

### splitWords：任意风格拆词

拆词是全部命名转换的基础，规则：

- 下划线 `_`、连字符 `-`、空格作为分隔符；
- 小写 → 大写转换处拆分（`userName` → `user | Name`）；
- 连续大写后接小写时，在最后一个大写前拆分（`HTTPServer` → `HTTP | Server`）。

### toPascalCase：结构体名 / 字段名

任意风格输入统一转为 PascalCase（每个单词仅首字母大写，其余转小写），唯一特例是单词 `id`（不区分大小写）统一转为 `ID`：

| 输入 | 输出 |
|---|---|
| `user_id` | `UserID` |
| `getUserById` | `GetUserByID` |
| `order-no` | `OrderNo` |
| `HTTPServer` | `HttpServer` |

### formatJSONTag：json tag 值

按 `NameCase` 枚举输出对应风格（见 [NameCase](#namecase) 表）；未识别的风格返回空串。

## 增量再生成与用户代码保护

目标文件已存在时，`writeOrReplaceStruct` 的工作方式：

1. **AST 解析**整个现有文件（`parser.ParseComments`），解析失败则报错返回，绝不覆盖；
2. **识别并移除旧的生成代码**（`isGenerated`）：
   - 名为 `{EntityName}` / `{DOName}` 的 type 声明；
   - Entity 上的 `ToDO` 方法、DO 上的 `ToEntity` 方法；
3. **分类保留用户代码**：
   - Entity 上的自定义方法 → 紧随 Entity 生成代码之后；
   - DO 上的自定义方法 → 紧随 DO 生成代码之后；
   - 其他声明（import 单独收集）→ 放在文件末尾；
4. **补齐 import**：生成代码所需的包（如 `time`）若文件中缺失，自动补充到 import 区；
5. 按固定布局重写整个文件。

特殊情形：文件不存在或内容为空白时，按新建处理（包名取输出目录名，无法推导时回退 `main`）。

### 注释净化（sanitizeTagValue）

列注释写入 `description` tag 前经 `strconv.Quote` 转义并将反引号替换为单引号，保证：

- 反引号不会提前终止 tag 的反引号字符串（语法错误）；
- 换行、回车等控制字符转为 `\n`、`\r` 转义序列，`reflect.StructTag.Lookup` 可完整还原原值；
- 双引号转义为 `\"`，不会被误认为 tag 值的分隔符。

## 使用示例

### 手工构造 Input 生成

```go
package main

import "github.com/buexplain/zckg/zcmodel"

func main() {
	err := zcmodel.Generate(zcmodel.Input{
		OutputDir:        "./model",   // 目录名 "model" 即生成文件的包名
		Database:         "test_db",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		TableComment:     "订单表",
		ColumnTagName:    "db",                  // 与 zcdb 的列映射标签保持一致
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns: []*zcmodel.Column{
			{Name: "id", Type: "bigint(20)", Comment: "主键"},
			{Name: "order_no", Type: "varchar(64)", Comment: "订单号"},
			{Name: "amount", Type: "decimal(10,2)"},
			{Name: "created_at", Type: "datetime", Comment: "创建时间"},
		},
	})
	if err != nil {
		panic(err)
	}
	// 生成 ./model/user_order.go：UserOrderEntity + UserOrderDO + ToDO/ToEntity
}
```

### 配合 zcdb Schema 从真实数据库生成

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
	dao, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		panic(err)
	}
	defer dao.Close()

	// 1. 读取表结构（列名、列类型、列注释）
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
		columns = append(columns, &zcmodel.Column{Name: c.Name, Type: c.Type, Comment: c.Comment})
	}

	// 2. 生成模型代码
	err = zcmodel.Generate(zcmodel.Input{
		OutputDir:        "./model",
		Database:         "test",
		Dialect:          zcmodel.DialectMysql,
		TableName:        "user_order",
		ColumnTagName:    "db",
		JsonTagValueCase: zcmodel.NameCaseLowerCamel,
		Columns:          columns,
	})
	if err != nil {
		panic(err)
	}
}
```

### 显式指定字段信息（覆盖自动推导）

```go
// JSON 列映射为自定义类型、特殊命名等场景，预先填好 StructFieldInfo 即可
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
```

### 生成后二次开发与再生成

用户可以在生成文件中自由添加自定义方法，表结构变化后再次调用 `Generate`：

- Entity/DO 结构体与 ToDO/ToEntity 被**替换**为最新版本；
- 自定义方法（如 `func (e *UserOrderEntity) Validate() error`）**原样保留**，且自动归位到对应结构体的生成代码之后；
- 若新表结构引入了新的依赖包（如新增 datetime 列），`time` import 会被自动补上。

## 注意事项

1. **输出目录名即包名**：`OutputDir` 的最后一级目录名被用作生成文件的 `package` 名，请保证它是合法的 Go 包名（如 `model`）；无法推导时回退为 `main`。
2. **文件名直接使用表名**：输出文件名为 `{TableName}.go`，不做大小写转换；表名含非法文件名字符时由操作系统报错。结构体名则始终经 `toPascalCase` 推导。
3. **未知列类型兜底为 string**：映射表未覆盖的列类型（如 PG 的自定义类型）默认映射为 `string`，避免生成非法代码；需要精确类型时请显式指定 `StructFieldInfo.Type` 与 `Import`。
4. **MySQL 的 BOOL 实际是 TINYINT(1)**：从真实 MySQL 读到的 BOOL/BOOLEAN 列，其存储类型为 `tinyint`，映射结果为 `int` 而非 `bool`。
5. **SQLite 不支持字段注释**：SQLite 元数据中没有列注释，生成的 `description` tag 恒为空。
6. **存量文件必须语法正确**：增量再生成依赖 `go/parser` 解析现有文件，若用户代码存在语法错误，`Generate` 报错返回且不会覆盖文件。
7. **ToEntity 的类型断言语义**：DO 字段为 `any`，断言失败（值为 nil 或类型不符）时该字段被跳过，Entity 保留原值；不会返回错误。
8. **json tag 可选**：`JsonTagValueCase` 传空串则不生成任何 json tag；单列也可通过显式指定 `StructFieldInfo.JsonTagValue` 覆盖全局风格。
