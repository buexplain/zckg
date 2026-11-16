# Schema 元数据（SchemaInspector）

`SchemaInspector` 是 zcdb 的元数据只读入口：查询当前数据库的表、字段与索引。它不经过 Builder，也不参与链式查询编译，常用于代码生成（如 zcmodel）、运维巡检与建表对照。

这份文档说明三个查询方法的语义、三方言实际执行的 SQL、`TableInfo`/`ColumnInfo`/`IndexInfo` 各字段的来源，以及「元数据本身不完整」造成的方言局限。

## 入口与接口

```text
func (d *DBDao) Schema() (SchemaInspector, error)
func NewSchemaInspector(dao *DBDao) (SchemaInspector, error)

type SchemaInspector interface {
    Tables(ctx context.Context) ([]TableInfo, error)
    Columns(ctx context.Context, table string) ([]ColumnInfo, error)
    Indexes(ctx context.Context, table string) ([]IndexInfo, error)
}
```

- `DBDao.Schema()` 等价于 `NewSchemaInspector(dao)`；后者按 DAO 持有的 Grammar **具体类型**分派到 `MySQLSchemaInspector`、`PostgresSchemaInspector`、`SQLiteSchemaInspector`。
- Grammar 不是这三个内置类型（包括 nil）时返回 `ErrSchemaGrammarNotSupported`，错误消息用 `%T` 带出实际类型，不返回半成品 inspector。
- 元数据查询走 `dao.Query`：`ctx` 未携带事务时按读路由选连接（见 [execution-routing](../concepts/execution-routing.md)），携带本 Pool 的事务时走事务连接；`ctx` 属于其它 Pool 时返回 `ErrTxOwnerMismatch`。
- **不追加 DAO 默认注释**：即使 `NewDBDao` 第五参数配置了短业务标识，Schema 查询也不会追加；慢 SQL 回调仍能观察到实际执行的原始 SQL。
- 参数绑定按方言：MySQL 用 `?`、PostgreSQL 用 `$1`、SQLite 的 `PRAGMA` 没有占位符（表名经 `Grammar.WrapTable` 包裹后拼接，见 [dialect-matrix](dialect-matrix.md)）。

```go
func example() {
	inspector, err := db.Schema()
	if err != nil {
		// 非内置 Grammar：errors.Is(err, zcdb.ErrSchemaGrammarNotSupported)
		panic(err)
	}

	tables, err := inspector.Tables(ctx)
	if err != nil {
		panic(err)
	}
	for _, table := range tables {
		fmt.Println(table.Name, table.Comment)
	}
}
```

## Tables：用户表清单

返回当前数据库中的**基础表**（不含视图），按表名升序。三方言的可见范围不同，这是最容易踩的差异。

```sql
-- MySQL：当前库（DATABASE()）的基础表
SELECT TABLE_NAME, IFNULL(TABLE_COMMENT, '')
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'
ORDER BY TABLE_NAME

-- PostgreSQL：仅 public schema 的普通表（relkind = 'r'）
SELECT c.relname, COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname = 'public'
ORDER BY c.relname

-- SQLite：main 库中非 sqlite_ 前缀的表
SELECT "name", '' FROM "sqlite_master"
WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%'
ORDER BY "name"
```

| 方言 | 注意 |
|---|---|
| MySQL | 只返回 `BASE TABLE`，视图、临时表不在结果中；表注释来自 `TABLE_COMMENT`，已用 `IFNULL` 兜底 |
| PostgreSQL | 只扫 `public`；分区父表（`relkind = 'p'`）、视图、物化视图、外部表都不返回 |
| SQLite | 只扫 `main`；`sqlite_` 前缀的内部表被过滤；`Comment` 恒为空（SQLite 没有表注释概念） |

`TableInfo` 字段：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Name` | `string` | 表名 |
| `Comment` | `string` | 表注释；PostgreSQL 来自 `obj_description`，MySQL 来自 `TABLE_COMMENT`，SQLite 恒为空串 |

## Columns：字段信息

返回指定表的全部字段，按**定义顺序**排列（MySQL `ORDINAL_POSITION`、PostgreSQL `attnum`、SQLite `cid`）。表不存在时不报错，见下文「空结果语义」。

```sql
-- MySQL
SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE,
       COLUMN_DEFAULT, IFNULL(COLUMN_KEY, ''), IFNULL(EXTRA, '')
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION

-- PostgreSQL（仅 public；attisdropped 过滤已删除列）
SELECT a.attname, format_type(a.atttypid, a.atttypmod),
       COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull,
       pg_get_expr(d.adbin, d.adrelid),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_index ix
           WHERE ix.indrelid = c.oid AND ix.indisprimary AND a.attnum = ANY(ix.indkey)
       )
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
WHERE c.relname = $1 AND n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum

-- SQLite（表名已经 WrapTable 包裹）
PRAGMA table_info("users")
```

| 方言 | `Nullable` 来源 | `PrimaryKey` 来源 | `Extra` |
|---|---|---|---|
| MySQL | `IS_NULLABLE = 'YES'` | `COLUMN_KEY = 'PRI'` | `EXTRA` 列原样 |
| PostgreSQL | `NOT attnotnull` | `EXISTS (pg_index ... indisprimary AND attnum = ANY(indkey))` | 恒为空 |
| SQLite | `notnull = 0` | `pk > 0` | 恒为空 |

`ColumnInfo` 字段：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Name` | `string` | 字段名 |
| `Type` | `string` | 方言原生类型文本（如 `varchar(64)`、`character varying(64)`、`INTEGER`） |
| `Comment` | `string` | 字段注释；MySQL 来自 `COLUMN_COMMENT`，PostgreSQL 来自 `col_description`，SQLite 恒为空 |
| `Nullable` | `bool` | 是否允许 NULL。SQLite 的 `INTEGER PRIMARY KEY` 报 `true`，见「三方言局限」 |
| `Default` | `*string` | 默认值；`nil` 表示没有默认值。文本是**方言原生格式**，不做归一化 |
| `PrimaryKey` | `bool` | 是否主键列；联合主键的每一列都是 `true` |
| `Extra` | `string` | MySQL `EXTRA` 原样；PostgreSQL/SQLite 恒为空 |

### Default 的方言格式

`Default` 直接透出数据库返回的文本，跨库比较前必须按方言处理：

| 方言 | 格式 | 实测示例（MySQL 8.4 / PostgreSQL 15 / SQLite 3.53） |
|---|---|---|
| MySQL | 裸值 | `active`、`0`、`1.50`、`CURRENT_TIMESTAMP`、`json_object()` |
| PostgreSQL | 表达式 | `'active'::character varying`、`0`、`1.50`、`CURRENT_TIMESTAMP`、`nextval('users_id_seq'::regclass)`、`'{}'::jsonb` |
| SQLite | 字面量 | `'x'`（字符串带单引号）、`0`、`1.5`（数值以文本呈现） |

自增与生成列的呈现方式并不统一：

| 情形 | `Default` | `Extra` |
|---|---|---|
| MySQL `AUTO_INCREMENT` | `nil`（`COLUMN_DEFAULT` 为 NULL） | `auto_increment` |
| MySQL 表达式默认值 | 表达式文本，如 `json_object()` | `DEFAULT_GENERATED` |
| MySQL `ON UPDATE CURRENT_TIMESTAMP` | `CURRENT_TIMESTAMP` | `DEFAULT_GENERATED on update CURRENT_TIMESTAMP` |
| MySQL 生成列 | `nil`（`COLUMN_DEFAULT` 为 NULL） | `STORED GENERATED` / `VIRTUAL GENERATED` |
| PostgreSQL `SERIAL`/`BIGSERIAL` | `nextval(...)` 表达式 | 恒为空 |
| SQLite `INTEGER PRIMARY KEY AUTOINCREMENT` | `nil` | 恒为空（`AUTOINCREMENT` 不在 `table_info` 中） |

**不要假设「默认值一定能从 `Default` 读到」**：MySQL 的自增与生成列、SQLite 的 `AUTOINCREMENT` 都没有默认值文本，语义只体现在 `Extra`（MySQL）或建表语句里。

## Indexes：索引信息

返回指定表的索引（主键、唯一、普通索引），`Columns` 按索引定义顺序聚合。三方言的实现路径差异最大。

```sql
-- MySQL：STATISTICS 按索引名 + 列序聚合，前缀索引取 SUB_PART
SELECT INDEX_NAME, COLUMN_NAME, SUB_PART, NON_UNIQUE
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX

-- PostgreSQL：indnkeyatts 过滤 INCLUDE 列，unnest(indkey) WITH ORDINALITY 给出列序
SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname, k.ord
FROM pg_catalog.pg_index ix
JOIN pg_catalog.pg_class c ON c.oid = ix.indrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord)
LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
WHERE n.nspname = 'public' AND c.relname = $1 AND k.ord <= ix.indnkeyatts
ORDER BY i.relname, k.ord

-- SQLite：两段式，主键行由 table_info 的 pk 序合成，其余索引逐条取列
PRAGMA table_info("users")     -- 合成主键行
PRAGMA index_list("users")     -- seq, name, unique, origin, partial
PRAGMA index_info("idx_name")  -- seqno, cid, name
```

`IndexInfo` 字段：

| 字段 | 类型 | 含义 |
|---|---|---|
| `Name` | `string` | 索引名；MySQL 主键固定为 `PRIMARY`，SQLite 的合成主键行同为 `PRIMARY`，PostgreSQL 用实际索引名（如 `users_pkey`） |
| `Columns` | `[]string` | 索引列，按定义顺序；表达式列以 `#expr` 占位，MySQL 前缀索引为 `col(n)` |
| `Unique` | `bool` | 是否唯一索引（PostgreSQL 主键索引同时为 `true`） |
| `Primary` | `bool` | 是否主键索引 |

索引列的呈现约定：

| 情形 | 呈现 | 来源与说明 |
|---|---|---|
| MySQL 前缀索引 | `email(10)` | `SUB_PART` 非 NULL 时追加 `(n)` |
| 表达式（函数）索引 | `#expr` | 三方言的列名都取不到（MySQL `COLUMN_NAME` 为 NULL、PostgreSQL 表达式列 `attname` 为 NULL、SQLite `index_info.name` 为 NULL） |
| PostgreSQL INCLUDE 列 | 不输出 | 只输出索引键列（`k.ord <= ix.indnkeyatts`）；INCLUDE 列不参与索引键 |
| SQLite rowid 表主键 | 合成行 | `INTEGER PRIMARY KEY`（含 `AUTOINCREMENT`）是 rowid 别名，`index_list` 中没有对应行，只能由 `table_info` 的 `pk` 序合成 |
| SQLite `origin = 'pk'` 自动索引 | 跳过 | TEXT 主键、联合主键、`WITHOUT ROWID` 表的 `sqlite_autoindex_*` 信息已由合成行覆盖，避免重复 |
| SQLite `origin = 'u'` 自动索引 | 保留 | UNIQUE 约束触发的自动索引没有用户命名，原样呈现自动名 |
| SQLite 部分索引 | 按普通索引呈现 | `partial = 1` 的谓词不在基础元数据中，v1 不输出 |

顺序：MySQL 按索引名再按列序，PostgreSQL 按索引名再按列序；SQLite 先输出合成主键行（如有），其余按 `index_list` 的返回顺序。无索引且无主键的表返回空切片。

SQLite 的表名与索引名都要拼进 `PRAGMA` 语句：两者都经 `Grammar.WrapTable` 包裹（双引号、内部双引号加倍），因此包含引号的标识符不会破坏语句结构；但包裹只防注入，不校验对象是否存在。

## 三方言局限

以下都是**元数据本身的缺口**，不是查询 bug；需要这些信息时请回查建表语句或方言专有视图。

| 局限 | 方言 | 表现 |
|---|---|---|
| 表/字段注释恒空 | SQLite | 没有注释概念，`Comment` 始终是空串 |
| 作用域受限 | MySQL / PostgreSQL / SQLite | MySQL 只看当前库；PostgreSQL 只看 `public`；SQLite 只看 `main` |
| 表名按字面值精确匹配 | PostgreSQL | `c.relname = $1` 精确比较：传 `Users` 查不到 `users`（未加引号的标识符在建表时已折叠为小写）；查不到时返回空结果而非报错 |
| INCLUDE 列缺失 | PostgreSQL | 只列索引键列；`INCLUDE ("status")` 的 `status` 不出现，容易被误判为「索引定义不同」 |
| 版本下限 | PostgreSQL | 查询用到 `indnkeyatts`，要求 PG ≥ 11（仓库测试基准 PG 15） |
| 表达式文本缺失 | 三方言 | 统一 `#expr`；MySQL 的 `EXPRESSION` 列自 8.0.13 才有，v1 不查询；PostgreSQL/SQLite 也不取表达式 |
| 部分索引谓词缺失 | PostgreSQL / SQLite | 索引按普通索引输出，谓词丢失；MySQL 无部分索引能力 |
| 降序标注缺失 | 三方言 | `DESC` 不体现在 `Columns` 中 |
| 生成列表达式缺失 | MySQL | `Extra` 只标注 `STORED/VIRTUAL GENERATED`，不含 `GENERATION_EXPRESSION` |
| `Nullable` 失真 | SQLite | `INTEGER PRIMARY KEY` 是 rowid 别名，`PRAGMA` 中 `notnull = 0`，因此 `Nullable` 为 `true`；该列实际不可能为 NULL，属元数据固有局限 |
| 主键命名不统一 | 三方言 | MySQL 固定 `PRIMARY`；SQLite 合成行固定 `PRIMARY`；PostgreSQL 用实际名（如 `users_pkey`），按 `Primary` 标记而非名字定位 |
| 自增/生成列默认值缺失 | MySQL / SQLite | 自增与生成列的 `Default` 为 `nil`，语义只在 `Extra`（MySQL）或建表语句中 |

### 空结果语义

`Columns` 与 `Indexes` 对**不存在的表**返回空切片与 `nil` 错误，不报错、不 panic：

- `Columns` 空结果可以判定为「表不存在或没有可见字段」；
- `Indexes` 空结果**无法区分**「表不存在」与「表存在但没有索引」，需要判断表是否存在时先查 `Tables`；
- MySQL/PostgreSQL 依赖 `information_schema`/`pg_catalog` 的查询权限；权限不足由数据库报错，错误原样返回（不包装成哨兵）；SQLite 的 `PRAGMA` 对未知表返回空结果集。

## 完整示例

```go
func example() {
	inspector, err := db.Schema()
	if err != nil {
		panic(err)
	}

	tables, err := inspector.Tables(ctx)
	if err != nil {
		panic(err)
	}
	for _, table := range tables {
		fmt.Printf("表 %s（%s）\n", table.Name, table.Comment)

		columns, err := inspector.Columns(ctx, table.Name)
		if err != nil {
			panic(err)
		}
		for _, column := range columns {
			null := "NOT NULL"
			if column.Nullable {
				null = "NULL"
			}
			fmt.Printf("  %s %s %s primary=%v extra=%q\n",
				column.Name, column.Type, null, column.PrimaryKey, column.Extra)
		}

		indexes, err := inspector.Indexes(ctx, table.Name)
		if err != nil {
			panic(err)
		}
		for _, index := range indexes {
			fmt.Printf("  index %s columns=%v unique=%v primary=%v\n",
				index.Name, index.Columns, index.Unique, index.Primary)
		}
	}
}
```

## 相关阅读

- 方言差异总表：[dialect-matrix](dialect-matrix.md)
- 连接、事务与读路由：[execution-routing](../concepts/execution-routing.md)
- 元数据如何用于模型生成：[mapping](../concepts/mapping.md)
- 任务式指引：[schema-inspection](../how-to/schema-inspection.md)
- 错误清单：[errors](errors.md)
