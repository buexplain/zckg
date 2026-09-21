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
- **注释安全**：字段注释写入 `description` tag 前经过转义净化，含反引号、换行、双引号的注释不会破坏生成代码的语法；
- **元数据快照**：可选的 `ddl` tag 呈现该列的类 DDL 列定义片段（类型 / 可空 / 默认值 / MySQL EXTRA），`primary_key` tag 标记主键列，`Input.Indexes` 渲染为结构体 doc 注释中的索引清单——使读模型文件的人与 AI 编码代理不必回查数据库即可看到表的面貌。元数据是**生成时刻的快照**，表结构变更后需重新生成才会刷新（不做运行时校验）；
- **混合文件声明**：文件首部写入说明头，声明「生成区在再生成时被替换、用户代码被完整保留」的分区规则（刻意不使用 Go 官方的 `// Code generated ... DO NOT EDIT.` 标记，理由见下方「文件说明头」）。

zcmodel 是纯代码生成工具，不连接数据库、不依赖数据库驱动。表结构信息可由调用方手工构造，也可通过 [zcdb](../../zcdb/docs/schema.md) 的 Schema 能力从真实数据库读取。

## 文件结构

```
zcmodel/
├── input.go                # 对外数据结构：Input、Column、StructFieldInfo、IndexInfo、NameCase、Dialect
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
      ① 校验 Dialect
                                 │
      ② 校验表名（防路径穿越与非法标识符）
         ├─ 拒绝空名、"." / ".."、路径分隔符（/、\）与 Windows 非法字符（<>:"|?*）
         └─ 首字符须为 ASCII 字母或下划线（保证推导的结构体名是合法 Go 标识符）
                                 │
      ③ 补全每列的 StructFieldInfo（只作用于副本，不修改调用方数据）
         ├─ 校验 JsonTagValueCase（非空且非法 → 报错）
         ├─ JsonTagValue 为空且 JsonTagValueCase 非空 → formatJSONTag（按 NameCase 转换）
         ├─ Name 为空        → toPascalCase（列名转 PascalCase）
         ├─ Type 为空        → formatStructFieldType（方言映射，未命中兜底 string）
         └─ Type 为 time.Time 且 Import 为空 → Import = "time"
                                 │
      ④ 字段名检测（避免产出无法编译的代码）
         ├─ 字段名为空 → 报错（附列名）
         ├─ 字段名非合法 Go 标识符（如数字开头列名）→ 报错（附列名）
         └─ 字段名重复（如 user_id 与 userId 同表）→ 报错
                                 │
      ⑤ 生成代码字符串
         ├─ buildStruct：Entity（具体类型）/ DO（any 类型）
         ├─ buildToDOMethod / buildToEntityMethod：互转方法
         └─ doc 注释：表注释 + 索引块（Input.Indexes）生成到结构体上方
                                 │
      ⑥ 写文件 {OutputDir}/{TableName}.go
         ├─ 自动创建输出目录，并二次校验输出路径不逃逸输出目录
         └─ writeOrReplaceStruct
            ├─ 文件不存在 → 直接创建（package + imports + 生成代码）
            ├─ 文件已存在 → AST 解析：移除旧生成代码，
            │  保留用户代码并重新组织布局，缺失 import 自动补齐
            └─ 落盘前 go/format 全量语法自校验 + 临时文件原子写入，
               任何语法非法产物都不落盘（见“落盘安全三重防线”）
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
| `表名为空` | `TableName` 为空字符串 |
| `表名非法: xxx` | `TableName` 为 `.` 或 `..` |
| `表名包含非法字符: xxx` | `TableName` 含路径分隔符（`/`、`\`）或 Windows 非法文件名字符（`< > : " | ? *`） |
| `表名首字符必须为 ASCII 字母或下划线: xxx` | `TableName` 首字符为数字或非 ASCII 字符（如中文表名），会推导出无法编译的结构体名 |
| `不支持的 json tag 命名风格: xxx` | `JsonTagValueCase` 非空但不是合法的 `NameCase` 枚举值 |
| `列 xxx 转换后的字段名为空` | 列名经 `toPascalCase` 转换后为空（如纯分隔符列名） |
| `列 xxx 转换后的字段名 xxx 不是合法的 Go 标识符` | 转换后字段名数字开头等（如列名 `2fa_code`）；请在 `StructFieldInfo.Name` 显式指定合法名称 |
| `列 xxx 与 xxx 转换后的字段名重复: xxx` | 不同列转换后字段名相同（如 `user_id` 与 `userId`） |
| `创建输出目录失败: ...` | `OutputDir` 无法创建 |
| `输出文件路径逃逸输出目录: xxx` | 输出路径经清理后不在 `OutputDir` 内（防御性二次校验） |
| `生成结构体失败: ...` | 目标文件存在但无法被 `go/parser` 解析（用户代码有语法错误）、生成产物未通过落盘前语法自校验等写文件失败场景；内部错误（`解析文件失败` / `创建临时文件失败` / `写入临时文件失败` / `设置文件权限失败` / `关闭临时文件失败` / `替换目标文件失败` / `生成代码存在语法错误`）均统一包裹为该错误 |

> 上表错误文本中的列名与字段名以 `%q` 格式输出（带双引号，如 `列 "2fa_code" …`），表名以原样输出。

### Input

| 字段 | 类型 | 说明 |
|---|---|---|
| `OutputDir` | `string` | 输出目录，不存在时自动创建。**目录名须为合法 Go 包名**（仅新建文件时包名取目录名，无法推导时回退 `main`；存量文件再生成时尊重原 package 声明） |
| `Database` | `string` | 数据库名，仅用于生成结构体的注释 |
| `Dialect` | `Dialect` | 数据库方言，决定列类型映射表 |
| `TableName` | `string` | 表名。原样用作输出文件名（`{TableName}.go`），转 PascalCase 后推导结构体名 |
| `TableComment` | `string` | 表注释，生成到结构体注释中；为空时使用"表" |
| `ColumnTagName` | `string` | 列映射 tag 的名称（如 `"column"`、`"db"`），与 zcdb 的列映射标签对应 |
| `JsonTagValueCase` | `NameCase` | JSON tag 的命名风格；**空值表示不生成 json tag** |
| `Columns` | `[]*Column` | 表的所有字段；为空时生成仅含空结构体与互转方法的文件 |
| `Indexes` | `[]IndexInfo` | 表索引；为空时不生成结构体 doc 注释中的索引块 |

### Column / StructFieldInfo / IndexInfo

```go
type Column struct {
    Name            string          // 列名
    Type            string          // 列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"），ddl 片段的锚点
    Comment         string          // 列注释，生成 description tag
    Nullable        *bool           // 是否可空；nil 表示未知（ddl 片段省略 NULL 标记）
    Default         *string         // 默认值；nil 表示无默认值（方言原生格式，见「ddl 片段」）
    PrimaryKey      bool            // 是否主键列（联合主键多列均为 true），生成 primary_key tag
    Extra           string          // MySQL information_schema 的 EXTRA 列原样（PG/SQLite 恒空）
    StructFieldInfo StructFieldInfo // 生成结构体字段时的信息
}

type StructFieldInfo struct {
    Name         string // 结构体字段名；留空则由列名自动推导（toPascalCase）
    Type         string // 结构体字段类型；留空则由列类型自动映射（未命中兜底 string）
    Import       string // Type 对应的 import 路径（如 time.Time 需 "time"）；留空不引入包
    JsonTagValue string // json tag 的值；留空时按 JsonTagValueCase 自动推导
}

type IndexInfo struct {
    Name    string   // 索引名（仅唯一/普通索引渲染该名字；主键行渲染为 PRIMARY KEY (cols)）
    Columns []string // 索引列，按定义顺序
    Unique  bool     // 是否唯一索引
    Primary bool     // 是否主键索引
}
```

**显式优先**：`StructFieldInfo` 的每个字段都允许调用方预先指定，`Generate` 只对留空的字段做自动推导。这使得特殊列（如 JSON 列想映射为自定义结构体类型）可以完全手工控制。

**指针字段的语义**：`Nullable` 与 `Default` 用指针而非值类型，以便区分「未知 / 无默认值」与零值——
`Nullable` 为 `nil` 时 `ddl` 片段**省略** NULL 标记（而不是假定 `NOT NULL` 而误标可空列）；
`Default` 为 `nil` 表示无默认值，非 `nil` 一律追加 DEFAULT 段（含空串，判定只看指针不看值）。
手工构造时建议显式填写这两个字段（如 `Nullable: &nullable`、`Default: &defaultVal`）。

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
文件说明头（部分生成声明；新建文件写入，存量文件缺失时在文件最顶补写、已含则不重复）

原文件头（build tags、文件级注释，存量文件按原文保留；新建文件无）
package 行（存量文件尊重原声明；新建文件取 OutputDir 目录名）

imports（原有 import + 缺失的生成代码所需 import）

// Entity 生成代码（结构体注释 + 结构体 + ToDO 方法）
// Entity 自定义方法（用户代码，保留）

// DO 生成代码（结构体注释 + 结构体 + ToEntity 方法）
// DO 自定义方法（用户代码，保留）

// 其他用户代码（保留）
```

#### 文件说明头

新建文件在首行写入说明头；存量文件再生成时若头部缺失则于文件最顶补写（已含则不重复）：

```go
// 本文件由 zcmodel 部分生成：Entity/DO 结构体及 ToDO/ToEntity 方法在再次调用
// Generate 时会被替换，其余用户代码会被完整保留。表结构变更请重新调用
// zcmodel.Generate，勿手改生成区。
```

三行分别声明文件性质（部分生成）、分区规则（生成区被替换 / 用户区被保留）与行为指引（表结构变更走再生成）。
说明头与 `package` 声明之间以空行分隔，避免被 godoc 识别为 package 文档注释；它位于用户 build tags（`//go:build`）之前是合法的——Go 规定 build 约束之前仅允许空行与其他行注释。

**刻意不使用** Go 官方的生成标记 `// Code generated ... DO NOT EDIT.`：官方标记的社区共识是「整个文件是生成物、手改会丢失」，与本模块「用户代码是文件的一等公民、再生成时被完整保留」的契约恰好相反；且匹配官方正则（`^// Code generated .* DO NOT EDIT\.$`）会使 staticcheck、golangci-lint 等工具**整体跳过该文件**，而本文件包含用户代码，不应被豁免静态检查。

### Entity 与 DO 结构体

- tag 顺序固定为：**json → {ColumnTagName} → primary_key → ddl → description**，各 tag 数据为空时跳过：
  - `json` tag 仅在 `JsonTagValue` 非空时生成；
  - `{ColumnTagName}` tag 仅在 `ColumnTagName` 非空时生成（避免产生空 tag 名 `:"colname"`）；
  - `primary_key:"true"` 仅在 `Column.PrimaryKey` 为 true 时生成（联合主键多列均标记）；主键在 DDL 中是表级约束，不并入 `ddl` 片段，另由 doc 注释的索引块呈现；
  - `ddl` tag 仅在 `Column.Type` 非空时生成（规则见下方「ddl 片段」）；
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
//   - KEY idx_user_status (user_id, status)
type UserOrderEntity struct {
	ID        int64     `json:"id" db:"id" primary_key:"true" ddl:"bigint unsigned NOT NULL AUTO_INCREMENT" description:"主键"`
	OrderNo   string    `json:"orderNo" db:"order_no" ddl:"varchar(32) NOT NULL" description:"订单号"`
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
//   - KEY idx_user_status (user_id, status)
type UserOrderDO struct {
	ID        any `json:"id" db:"id" primary_key:"true" ddl:"bigint unsigned NOT NULL AUTO_INCREMENT" description:"主键"`
	OrderNo   any `json:"orderNo" db:"order_no" ddl:"varchar(32) NOT NULL" description:"订单号"`
	Amount    any `json:"amount" db:"amount" ddl:"decimal(10,2) NOT NULL DEFAULT 0.00" description:"订单金额（保留两位小数）"`
	Status    any `json:"status" db:"status" ddl:"enum('pending','paid','refunded') NOT NULL DEFAULT pending" description:"订单状态"`
	Remark    any `json:"remark" db:"remark" ddl:"varchar(255) NULL" description:"备注（可为空）"`
	CreatedAt any `json:"createdAt" db:"created_at" ddl:"datetime NOT NULL DEFAULT CURRENT_TIMESTAMP" description:"创建时间"`
}
```

#### ddl 片段

`ddl` 是**重组的类 DDL 文本**（目标是让人与 AI 准确理解该列，而非可直接执行的 DDL），按
`{Type} [{NULL|NOT NULL}] [DEFAULT 值] [EXTRA 白名单项]` 组装：

| 段 | 取值规则 |
|---|---|
| `Type` | `Column.Type` 方言原样，不归一化（保留 `unsigned`、`enum` 值域、长度/精度等） |
| NULL 标记 | `Nullable` 为 `*bool`：`true` → `NULL`、`false` → `NOT NULL`、`nil`（未知）→ **整段省略**。已知值一律显式书写，不模拟各方言 `SHOW CREATE` 的省略习惯；未知时省略而非假定 `NOT NULL`，避免误标可空列 |
| DEFAULT | `Default` 非 `nil` 时追加（判定只看指针不看值）；值为空串时渲染为 `DEFAULT ''`（MySQL 元数据对空串默认值给出裸空串，补一对单引号还原为合法的字符串字面量）；其余值方言原样：MySQL 裸值（`pending`）、PostgreSQL 表达式（`'pending'::character varying`、`nextval(...)`）、SQLite 带引号字面量 |
| EXTRA | 仅 MySQL：`Column.Extra` 透传 information_schema 的 `EXTRA`，按白名单过滤并以大写形态输出，顺序固定为 `AUTO_INCREMENT` → `ON UPDATE CURRENT_TIMESTAMP` → `VIRTUAL GENERATED` → `STORED GENERATED`。`DEFAULT_GENERATED` 属噪音（默认值已由 DEFAULT 段呈现）被过滤；`VIRTUAL/STORED GENERATED` 必须保留——该列不可写，片段是 DO 侧唯一能提示这一点的位置（EXTRA 不含生成表达式本身，完整表达式仍需回查 DDL） |

**已知局限**：

- 元数据是**生成时刻的快照**，表结构变更后未重新生成即过期；`ddl` 只呈现单个列定义片段，不含索引与约束（索引见索引块，约束请回查建表语句）；
- 各段的值形态因方言而异（裸值 / 带引号字面量 / 带 cast 表达式），这是「方言原样」决策的自然结果，不抹平方言与版本差异；
- MySQL 版本间差异（如 8.0.13+ 才支持表达式默认值）未逐一验证（测试基准为 MySQL 8.4）；
- SQLite 的 `INTEGER PRIMARY KEY` 在 `table_info` 中 `notnull=0`，会渲染为 `INTEGER NULL`——该列实为 rowid 别名、不可能为 NULL，属元数据固有局限，不做特判修正。

#### 索引块

`Input.Indexes` 非空时，Entity 与 DO 的 doc 注释中追加索引清单（upsert 是写操作，DO 侧对读者同样关键）：

```go
// UserOrderDO test_db.user_order 订单表，do结构体，常用于数据库写入操作。
//
// 索引:
//   - PRIMARY KEY (id)
//   - UNIQUE KEY uk_email (email)
//   - UNIQUE KEY uk_order_no (order_no)
//   - KEY idx_user_status (user_id, status)
type UserOrderDO struct {
```

- **排序固定**：PRIMARY → UNIQUE（索引名字典序）→ 普通（索引名字典序），输出稳定利于 git diff 与 golden 断言；
- **渲染格式**：主键行 `PRIMARY KEY (cols)`，不显示方言内部物理索引名（如 PG 的 `user_order_pkey`、SQLite 的 `sqlite_autoindex_*`）；唯一与普通索引为 `UNIQUE KEY name (cols)` / `KEY name (cols)`，列按定义顺序以逗号加空格连接；
- **清单前缀**：`//` + 三个空格 + `- `，即 gofmt 对 doc 注释清单的规范形态，产物与落盘前 gofmt 输出一致；
- **表达式列**：列位可能是表达式文本或占位符（如 `#expr`，由元数据来源决定），生成端不校验列名合法性——索引块是提示性元数据，严格校验会误报表达式索引；
- **净化**：索引名与列文本中的换行/回车替换为空格，防止注释行断裂；
- `Input.Indexes` 为空时整块省略（数据驱动，无数据自然不生成）。

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
2. **识别并移除旧的生成代码**（内部由 `isGeneratedTypeSpec` / `isGeneratedMethod` 判定）：
   - 名为 `{EntityName}` / `{DOName}` 的 type 声明（混合 type 声明块按 Spec 粒度过滤：按源码偏移剔除生成的类型，同块中的用户类型及其注释、块内游离注释均原样保留）；
   - Entity 上的 `ToDO` 方法、DO 上的 `ToEntity` 方法（含值接收者版本，避免同名方法共存导致编译失败）；
3. **分类保留用户代码**：
   - Entity 上的自定义方法（指针/值接收者均识别）→ 紧随 Entity 生成代码之后；
   - DO 上的自定义方法（指针/值接收者均识别）→ 紧随 DO 生成代码之后；
   - 其他声明（import 单独收集）→ 放在文件末尾；
4. **补齐 import**：生成代码所需的包（如 `time`）若文件中缺失，自动补充并合并进原第一个 import 块（避免形成两个 import 块）；存量文件以**别名导入**（含 `_`、`.` 导入）所需包时不视为已存在，仍会补充默认导入（生成代码引用默认包名，两种导入可合法共存）；
5. 按固定布局重写整个文件；
6. **落盘保障**：全部处理成功后，对完整内容执行 `go/format.Source` 语法自校验，再经同目录临时文件 + rename 原子替换目标文件（见下方“落盘安全三重防线”）。

特殊情形：文件不存在或内容为空白时，按新建处理（包名取输出目录名，无法推导时回退 `main`）。存量文件再生成时：package 声明之前的原文（build tags、文件级注释）与原 package 行按原文前置保留，包名不强制改为目录推导值；若 package 声明之前的头部区域没有说明块首行（整行精确匹配），则在文件最顶补写「说明头 + 空行」（见「文件说明头」）。

### 落盘安全三重防线

1. **落盘前语法自校验**：`writeGeneratedFile` 在写出前对完整文件内容执行 `go/format.Source`（内部含 `go/parser` 解析），任何语法非法的产物都直接报错、不落盘；
2. **原子写入**：`writeFileAtomic` 在同目录创建临时文件写入后 rename 覆盖目标（Windows 上 rename 可直接覆盖已存在文件），进程中断/磁盘满不留半文件，且保留目标文件原权限位；
3. **存量文件解析失败不覆盖**：已有文件无法被 `go/parser` 解析时直接报错返回，绝不覆盖用户代码。

### 注释净化（sanitizeTagValue）

列注释写入 `description` tag 前经 `strconv.Quote` 转义并将反引号替换为单引号，保证：

- 反引号不会提前终止 tag 的反引号字符串（语法错误）；
- 换行、回车等控制字符转为 `\n`、`\r` 转义序列，`reflect.StructTag.Lookup` 可完整还原原值；
- 双引号转义为 `\"`，不会被误认为 tag 值的分隔符。

`ddl` 片段的 tag 值同样经此净化（enum 值域的单引号在 tag 值中合法、原样保留；双引号与反斜杠被转义）。

此外，进入 **doc 注释**的表注释、索引名与索引列文本经换行净化（`\n`/`\r` → 空格）：注释无法转义，含换行的文本会被撑断成裸行而导致生成代码语法错误（落盘自校验会报错，但根因难定位），故在组装注释时统一净化。

## 使用示例

### 手工构造 Input 生成

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

### 配合 zcdb Schema 从真实数据库生成

`NewDBDao` 必须传五个参数：第四参数为列映射标签名（空串使用 `db`），第五参数为默认 SQL 短业务标识（此处传 `""` 保持无注释基线）。默认注释仅影响 Builder 的最终编译 SQL，DAO 原始 SQL 和 Schema 元数据查询不会自动追加，表/列注释读取及模型生成行为不变。第五参数不是表/列说明，不得透传外部输入、请求体或秘密；覆盖、清空及 Clone 行为见 [zcdb 查询构造](../../zcdb/docs/query-builder.md)的 Comment 小节。

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

一键生成的产物即「表的完整面貌」：每列的 `ddl` 片段（类型 / 可空 / 默认值 / MySQL `EXTRA`）、主键列的 `primary_key` tag，以及 Entity 与 DO 的 doc 注释中的索引块。索引列的特殊形态（MySQL 前缀索引 `col(n)`、表达式索引 `#expr`、PG 的 INCLUDE 列不输出、SQLite 合成主键行）见 [zcdb Schema 元数据查询](../../zcdb/docs/schema.md)的 Indexes 小节。

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
- 自定义方法（如 `func (e *UserOrderEntity) Validate() error`，指针与值接收者均支持）**原样保留**，且自动归位到对应结构体的生成代码之后；
- 若新表结构引入了新的依赖包（如新增 datetime 列），`time` import 会被自动补上。

## 注意事项

1. **输出目录名即包名（仅新建文件）**：新建生成文件时，`OutputDir` 的最后一级目录名被用作 `package` 名，请保证它是合法的 Go 包名（如 `model`），无法推导时回退为 `main`；存量文件再生成时尊重原 package 声明，不受目录名影响。
2. **表名经主动校验**：输出文件名为 `{TableName}.go`，不做大小写转换；`Generate` 会主动校验表名——拒绝空名、`.` / `..`、路径分隔符与 Windows 非法文件名字符，且首字符必须为 ASCII 字母或下划线（保证推导出的结构体名是合法 Go 标识符），非法时返回明确错误（而非依赖操作系统报错）。结构体名则始终经 `toPascalCase` 推导。
3. **未知列类型兜底为 string**：映射表未覆盖的列类型（如 PG 的自定义类型）默认映射为 `string`，避免生成非法代码；需要精确类型时请显式指定 `StructFieldInfo.Type` 与 `Import`。
4. **MySQL 的 BOOL 实际是 TINYINT(1)**：从真实 MySQL 读到的 BOOL/BOOLEAN 列，其存储类型为 `tinyint`，映射结果为 `int` 而非 `bool`。
5. **SQLite 不支持字段注释**：SQLite 元数据中没有列注释，通过 zcdb Schema 读取时 `Comment` 恒为空，因此生成的 `description` tag 也恒为空；若手工构造 SQLite 方言的列并填写 `Comment`，仍会正常生成该 tag。
6. **存量文件必须语法正确**：增量再生成依赖 `go/parser` 解析现有文件，若用户代码存在语法错误，`Generate` 报错返回且不会覆盖文件。
7. **ToEntity 的类型断言语义**：DO 字段为 `any`，断言失败（值为 nil 或类型不符）时该字段被跳过，Entity 保留原值；不会返回错误。
8. **json tag 可选**：`JsonTagValueCase` 传空串则不生成任何 json tag；单列也可通过显式指定 `StructFieldInfo.JsonTagValue` 覆盖全局风格。
9. **生成代码按名称识别**：增量再生成按名称匹配 Entity/DO 类型与 ToDO/ToEntity 方法，请勿在同一文件中定义与它们同名的其他类型，否则再生成时会被一并移除。
10. **列名须能推导出合法标识符**：数字开头的列名（如 `2fa_code`）会报“不是合法的 Go 标识符”错误，请通过 `StructFieldInfo.Name` 显式指定合法字段名；中文列名是合法 Go 标识符，可正常生成。
11. **`Nullable` 未知时刻意省略 NULL 标记**：手工构造 `Input` 且未设置 `Nullable` 时，`ddl` 片段只输出类型（如 `ddl:"varchar(255)"`）——缺省渲染 `NOT NULL` 会把可空列误标，故用 `*bool` 的 `nil` 表达「未知」并整段省略；需要完整的 NULL/NOT NULL 标记请显式设置（桥接 zcdb Schema 时该值恒已知）。
12. **元数据是生成时刻的快照**：`ddl` tag、`primary_key` tag 与索引块均来自生成时传入的元数据，表结构或索引变更后必须重新调用 `Generate` 才会刷新；本模块不做运行时校验，也不会回查数据库。
13. **MySQL 生成列的呈现**：生成列在 `ddl` 片段中以 `VIRTUAL GENERATED` / `STORED GENERATED` 结尾（提示该列**不可写**，DO 侧尤其重要），但片段不含生成表达式——需要表达式本身时请回查建表语句。
14. **请勿改动文件说明头的首行**：再生成时按首行判定是否需要补写说明头，改动首行会导致下次再生成时重复补写一次（说明块其余行可自由修改，不影响判定）。
