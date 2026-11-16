# 字段、tag 与 ddl

> 返回 [zcmodel 文档地图](zcmodel.md)。本文覆盖字段 tag 的转义、`ddl` 片段、索引块与 doc 注释净化——即“生成出来的一行字段/一段注释为什么长这样”。

## tag 组装与转义

`buildStruct` 按固定顺序组装每个字段的 tag：**json → {ColumnTagName} → primary_key → ddl → description**（见 [生成代码契约](generated-code.md)），值由 `formatStructTag` 统一处理：

- 全部 tag 值（`json`、列映射 tag 的列名、`ddl`、`description`）写入前均经 `strconv.Quote` 转义；
- 常规 tag 使用反引号原始字符串；**任一 tag 键或值含反引号**时，整个 tag 改用双引号解释型字符串字面量，反引号与列名原值完整保留，不会提前终止 tag；
- 换行、回车等控制字符转为 `\n`、`\r` 转义序列，双引号转义为 `\"`，`reflect.StructTag.Lookup` 可完整还原原值；
- `ddl` 片段的 tag 值同样经此转义（enum 值域的单引号在 tag 值中合法、原样保留；双引号与反斜杠被转义）。

含反引号列名（如 PostgreSQL/SQLite 的 ``a`b``）生成的整个字段 tag 会变成解释型字符串，列映射 tag 仍等于真实列名，不会导致 zcdb 映射失配。

> 实现：`build.go` `formatStructTag`、`buildStruct`；测试：`TestFormatStructTag`、`TestFormatStructTag_ReflectRestore`、`TestBuildStruct_ColumnTagValueWithSpecialChars`、`TestBuildStruct_JsonTagValueWithBacktick`。

## ddl 片段

`ddl` 是**重组的类 DDL 文本**（目标是让人与 AI 准确理解该列，而非可直接执行的 DDL），按 `{Type} [{NULL|NOT NULL}] [DEFAULT 值] [EXTRA 白名单项]` 组装：

| 段 | 取值规则 |
|---|---|
| `Type` | `Column.Type` 方言原样，不归一化（保留 `unsigned`、`enum` 值域、长度/精度等） |
| NULL 标记 | `Nullable` 为 `*bool`：`true` → `NULL`、`false` → `NOT NULL`、`nil`（未知）→ **整段省略**。已知值一律显式书写，不模拟各方言 `SHOW CREATE` 的省略习惯；未知时省略而非假定 `NOT NULL`，避免误标可空列 |
| DEFAULT | `Default` 非 `nil` 时追加（判定只看指针不看值）；值为空串时渲染为 `DEFAULT ''`（MySQL 元数据对空串默认值给出裸空串，补一对单引号还原为合法的字符串字面量）；其余值方言原样：MySQL 裸值（`pending`）、PostgreSQL 表达式（`'pending'::character varying`、`nextval(...)`）、SQLite 带引号字面量 |
| EXTRA | 仅 MySQL：`Column.Extra` 透传 information_schema 的 `EXTRA`，按白名单过滤并以大写形态输出，顺序固定为 `AUTO_INCREMENT` → `ON UPDATE CURRENT_TIMESTAMP` → `VIRTUAL GENERATED` → `STORED GENERATED`。`ON UPDATE CURRENT_TIMESTAMP` 保留紧随其后的 `(n)` 精度（MySQL 对 `datetime(3)` 等列给出 `on update CURRENT_TIMESTAMP(3)`，渲染为 `ON UPDATE CURRENT_TIMESTAMP(3)`；括号内非纯数字时不追加）。`DEFAULT_GENERATED` 属噪音（默认值已由 DEFAULT 段呈现）被过滤；`VIRTUAL/STORED GENERATED` 必须保留——该列不可写，片段是 DO 侧唯一能提示这一点的位置（EXTRA 不含生成表达式本身，完整表达式仍需回查 DDL） |

**已知局限**：

- 元数据是**生成时刻的快照**，表结构变更后未重新生成即过期；`ddl` 只呈现单个列定义片段，不含索引与约束（索引见索引块，约束请回查建表语句）；
- 各段的值形态因方言而异（裸值 / 带引号字面量 / 带 cast 表达式），这是「方言原样」决策的自然结果，不抹平方言与版本差异；
- **MySQL 裸值默认值的数字形态有歧义**：`varchar(10) DEFAULT '0'` 在元数据里是裸值 `0`，片段渲染为 `DEFAULT 0`（读起来像数值默认值）；列类型紧邻其前可消除歧义，如需精确语义请回查建表语句；
- **显式 `DEFAULT NULL` 的呈现因方言而异**：MySQL 元数据与「无默认值」不可区分，片段不含 DEFAULT 段；PostgreSQL 给出表达式 `NULL::character varying`、SQLite 给出字面量 `NULL`，二者都会如实渲染出 DEFAULT 段（判定只看指针，不做特殊抹平）；
- MySQL 版本间差异（如 8.0.13+ 才支持表达式默认值）未逐一验证（测试基准为 MySQL 8.4）；
- SQLite 的 `INTEGER PRIMARY KEY` 在 `table_info` 中 `notnull=0`，会渲染为 `INTEGER NULL`——该列实为 rowid 别名、不可能为 NULL，属元数据固有局限，不做特判修正。

> 实现：`build.go` `assembleDDLFragment`、`formatDDLDefault`、`ddLExtraParts`、`precisionSuffix`；测试：`TestAssembleDDLFragment_NullableTriState`、`TestAssembleDDLFragment_Default`、`TestAssembleDDLFragment_ExtraWhiteList`、`TestAssembleDDLFragment_TypePreservedVerbatim`。

## 索引块

索引是**表级信息、没有自然的结构体字段挂载点**，因此不放 tag，而渲染为 struct 的 doc 注释（与各字段 tag 契约无关）：

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
	// 字段定义略
}
```

- **排序固定**：PRIMARY → UNIQUE（索引名字典序）→ 普通（索引名字典序），输出稳定利于 git diff 与 golden 断言；
- **渲染格式**：主键行 `PRIMARY KEY (cols)`，不显示方言内部物理索引名（如 PG 的 `user_order_pkey`、SQLite 的 `sqlite_autoindex_*`）；唯一与普通索引为 `UNIQUE KEY name (cols)` / `KEY name (cols)`，列按定义顺序以逗号加空格连接；
- **清单前缀**：`//` + 三个空格 + `- `，即 gofmt 对 doc 注释清单的规范形态，产物与落盘前 gofmt 输出一致；
- **表达式列**：列位可能是表达式文本或占位符（如 `#expr`，由元数据来源决定），生成端不校验列名合法性——索引块是提示性元数据，严格校验会误报表达式索引；
- **净化**：索引名与列文本中的换行/回车替换为空格，防止注释行断裂；连续两个单引号或反引号之间插入零宽空格（U+200B），防止 gofmt 将其改写为弯引号（见下方「doc 注释净化」）；
- `Input.Indexes` 为空时整块省略（数据驱动，无数据自然不生成）。

> 实现：`build.go` `buildStructDocComment`、`buildIndexCommentBlock`；测试：`TestBuildIndexCommentBlock_Ordering`、`TestBuildIndexCommentBlock_Format`、`TestBuildIndexCommentBlock_Empty`、`TestBuildStructDocComment`、`TestBuildStruct_CompositePrimaryKey`。

## doc 注释净化

进入 **doc 注释**的表注释、索引名与索引列文本经换行净化（`\n`/`\r` → 空格）：注释无法转义，含换行的文本会被撑断成裸行而导致生成代码语法错误（落盘自校验会报错，但根因难定位），故在组装注释时统一净化。

同时，gofmt 会把 doc 注释中连续两个单引号或连续两个反引号规范化为弯引号（如 enum 空串值域、SQL 字面量中的成对引号），因此净化时在相邻两个引号之间插入零宽空格（U+200B），使注释在 gofmt 前后保持一致、读起来与原文相同（注释中会多出不可见字符，复制回 SQL 时需留意）。

> 实现：`build.go` `sanitizeCommentLine`、`buildStructDocComment`；测试：`TestSanitizeCommentLine`、`TestBuildStruct_TableCommentNewlineRegression`、`TestBuildStruct_DocCommentQuotesSurviveGofmt`。

## 相关

- 列类型与命名：[类型映射与命名转换](types-and-naming.md)
- 产物整体形态：[生成代码契约](generated-code.md)
- 生成流水线：[生成流水线](pipeline.md)
