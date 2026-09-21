# Schema 元数据查询

`DBDao.Schema()` 返回 `SchemaInspector` 接口，用于查询当前数据库的表列表、字段信息与索引信息（按 DAO 的 Grammar 类型自动选择 MySQL/PostgreSQL/SQLite 实现），常用于代码生成、运维巡检等场景。

Schema 元数据查询**不自动追加 SQL 注释**，即使 DAO 第五参数配置了默认短业务标识也不追加；默认注释仅影响 Builder 的最终编译 SQL。`TableInfo.Comment` / `ColumnInfo.Comment` 是数据库中的表/列说明，与 `Builder.Comment` 的 SQL 尾部业务标识不同，模型生成行为不受影响。慢 SQL 回调仍可观察 Schema 实际执行的原始 SQL。

```go
inspector, err := db.Schema()
if err != nil {
	panic(err)
}
```

也可直接用 `zcdb.NewSchemaInspector(dao)` 创建，非内置方言的 Grammar 会返回错误。

## Tables：查询所有用户表

返回当前数据库（MySQL 为当前库、PostgreSQL 为 public schema）中所有基础表的名称与注释：

```go
tables, err := inspector.Tables(ctx)
for _, t := range tables {
	fmt.Println(t.Name, t.Comment)
}
```

底层执行的 SQL（按方言）：

```sql
-- MySQL
SELECT TABLE_NAME, IFNULL(TABLE_COMMENT, '')
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE'
ORDER BY TABLE_NAME

-- PostgreSQL
SELECT c.relname, COALESCE(obj_description(c.oid, 'pg_class'), '')
FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname = 'public'
ORDER BY c.relname

-- SQLite
SELECT "name", '' FROM "sqlite_master"
WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%'
ORDER BY "name"
```

`TableInfo` 字段：

| 字段 | 说明 |
|---|---|
| `Name` | 表名 |
| `Comment` | 表注释（SQLite 无表注释概念，始终为空） |

## Columns：查询表的字段信息

返回指定表的所有字段元数据，按字段定义顺序排列：

```go
columns, err := inspector.Columns(ctx, "users")
for _, c := range columns {
	fmt.Printf("%s %s nullable=%v default=%v comment=%s primary=%v extra=%s\n",
		c.Name, c.Type, c.Nullable, c.Default, c.Comment, c.PrimaryKey, c.Extra)
}
```

底层执行的 SQL（按方言）：

```sql
-- MySQL
SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE, COLUMN_DEFAULT,
       IFNULL(COLUMN_KEY, ''), IFNULL(EXTRA, '')
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION

-- PostgreSQL
SELECT a.attname, format_type(a.atttypid, a.atttypmod),
       COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull,
       pg_get_expr(d.adbin, d.adrelid),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_index ix
           WHERE ix.indrelid = c.oid AND ix.indisprimary AND a.attnum = ANY(ix.indkey)
       ) AS is_primary
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
WHERE c.relname = $1 AND n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum

-- SQLite
PRAGMA table_info("users")
```

`ColumnInfo` 字段：

| 字段 | 说明 |
|---|---|
| `Name` | 字段名 |
| `Type` | 字段类型（如 `varchar(255)`、`integer`） |
| `Comment` | 字段注释（SQLite 始终为空） |
| `Nullable` | 是否允许 NULL。注意 SQLite 的 `INTEGER PRIMARY KEY` 是 rowid 别名，`PRAGMA` 中 `notnull=0`，此处为 `true`（该列实际不可能为 NULL，属元数据固有局限） |
| `Default` | 默认值，`*string`，nil 表示无默认值 |
| `PrimaryKey` | 是否主键列（联合主键的各列均为 `true`） |
| `Extra` | MySQL `information_schema` 的 `EXTRA` 列原样（如 `auto_increment`、`DEFAULT_GENERATED on update CURRENT_TIMESTAMP`、`VIRTUAL GENERATED`、`STORED GENERATED`）；PostgreSQL/SQLite 恒为空 |

## Default 默认值的方言格式

`Default` 为**方言原生格式**，不做归一化，跨库比较时需自行处理：

| 方言 | 格式 | 示例 |
|---|---|---|
| MySQL | 裸值 | `active` |
| PostgreSQL | 表达式 | `'active'::character varying`、`nextval('users_id_seq'::regclass)` |
| SQLite | 字面量 | 字符串带引号 `'x'`，数值为 `0` / `1.5` |

注意：PostgreSQL 的 `SERIAL` 自增语义只体现在 `Default` 的 `nextval(...)` 表达式中（PG 无 MySQL 的 `EXTRA` 元数据列）；MySQL 的自增则体现在 `Extra` 的 `auto_increment` 上。

## Indexes：查询表的索引信息

返回指定表的索引元数据（主键、唯一、普通索引），`Columns` 按索引定义顺序聚合：

```go
indexes, err := inspector.Indexes(ctx, "users")
for _, idx := range indexes {
	fmt.Printf("%s unique=%v primary=%v columns=%v\n", idx.Name, idx.Unique, idx.Primary, idx.Columns)
}
```

底层执行的 SQL（按方言）：

```sql
-- MySQL
SELECT INDEX_NAME, COLUMN_NAME, SUB_PART, NON_UNIQUE
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY INDEX_NAME, SEQ_IN_INDEX

-- PostgreSQL（仅 public schema；要求 PG ≥ 11：indnkeyatts 自 PG 11 引入，仓库测试基准为 PG 15）
SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname, k.ord
FROM pg_catalog.pg_index ix
JOIN pg_catalog.pg_class c ON c.oid = ix.indrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord)
LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
WHERE n.nspname = 'public' AND c.relname = $1 AND k.ord <= ix.indnkeyatts
ORDER BY i.relname, k.ord

-- SQLite（两段式：先列出索引，再逐个取列）
PRAGMA index_list("users")    -- seq, name, unique, origin, partial
PRAGMA index_info("idx_name") -- seqno, cid, name
```

`IndexInfo` 字段：

| 字段 | 说明 |
|---|---|
| `Name` | 索引名；MySQL 主键索引为 `PRIMARY`，SQLite 的合成主键行同为 `PRIMARY`（该行不显示物理索引名），PostgreSQL 为实际索引名（如 `users_pkey`） |
| `Columns` | 索引列，按定义顺序 |
| `Unique` | 是否唯一索引 |
| `Primary` | 是否主键索引 |

索引列的呈现约定：

| 情形 | 呈现 | 说明 |
|---|---|---|
| MySQL 前缀索引 | `email(10)` | `SUB_PART` 非 NULL 时附加前缀长度 |
| 表达式（函数）索引 | `#expr` | 列名无法从基础元数据取得（MySQL 的 `COLUMN_NAME` 为 NULL、PostgreSQL 的 `attnum` 为 0、SQLite 的 `index_info.name` 为 NULL），v1 不查询方言特有的表达式列 |
| PostgreSQL INCLUDE 列 | 不输出 | 只输出索引键列（`k.ord <= ix.indnkeyatts`）；INCLUDE 列不参与索引键 |
| SQLite rowid 表主键 | 合成行 | `INTEGER PRIMARY KEY`（含 `AUTOINCREMENT`）是 rowid 别名、无二级索引，主键行由 `table_info` 的 `pk` 序合成 |
| SQLite `origin='pk'` 自动索引 | 跳过 | TEXT 主键、联合主键、`WITHOUT ROWID` 表触发的 `sqlite_autoindex_*` 信息已由合成行覆盖，避免重复 |
| SQLite `origin='u'` 自动索引 | 保留 | UNIQUE 约束触发的 `sqlite_autoindex_*` 无用户命名可得，原样呈现其自动名 |
| SQLite 部分索引 | v1 忽略谓词 | `partial=1` 的谓词不在基础元数据中，该索引按普通索引呈现 |

不存在的表与 `Columns` 行为一致：返回空切片与 nil 错误，不报错也不 panic，调用方以空切片自行判断。

## 完整示例

```go
inspector, err := db.Schema()
if err != nil {
	return err
}

// 遍历所有表及其字段与索引，输出类似建表清单的信息
tables, err := inspector.Tables(ctx)
if err != nil {
	return err
}
for _, t := range tables {
	fmt.Printf("表 %s（%s）:\n", t.Name, t.Comment)
	columns, err := inspector.Columns(ctx, t.Name)
	if err != nil {
		return err
	}
	for _, c := range columns {
		null := "NOT NULL"
		if c.Nullable {
			null = "NULL"
		}
		key := ""
		if c.PrimaryKey {
			key = " PRIMARY KEY"
		}
		fmt.Printf("  %s %s %s%s\n", c.Name, c.Type, null, key)
	}
	indexes, err := inspector.Indexes(ctx, t.Name)
	if err != nil {
		return err
	}
	for _, idx := range indexes {
		kind := "KEY"
		if idx.Primary {
			kind = "PRIMARY KEY"
		} else if idx.Unique {
			kind = "UNIQUE KEY"
		}
		fmt.Printf("  %s %s (%s)\n", kind, idx.Name, strings.Join(idx.Columns, ", "))
	}
}
```
