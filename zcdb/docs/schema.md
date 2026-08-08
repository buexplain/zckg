# Schema 元数据查询

`DBDao.Schema()` 返回 `SchemaInspector` 接口，用于查询当前数据库的表列表与字段信息（按 DAO 的 Grammar 类型自动选择 MySQL/PostgreSQL/SQLite 实现），常用于代码生成、运维巡检等场景。

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
	fmt.Printf("%s %s nullable=%v default=%v comment=%s\n",
		c.Name, c.Type, c.Nullable, c.Default, c.Comment)
}
```

底层执行的 SQL（按方言）：

```sql
-- MySQL
SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE, COLUMN_DEFAULT
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
ORDER BY ORDINAL_POSITION

-- PostgreSQL
SELECT a.attname, format_type(a.atttypid, a.atttypmod),
       COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull,
       pg_get_expr(d.adbin, d.adrelid)
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
| `Nullable` | 是否允许 NULL |
| `Default` | 默认值，`*string`，nil 表示无默认值 |

## Default 默认值的方言格式

`Default` 为**方言原生格式**，不做归一化，跨库比较时需自行处理：

| 方言 | 格式 | 示例 |
|---|---|---|
| MySQL | 裸值 | `active` |
| PostgreSQL | 表达式 | `'active'::character varying`、`nextval('users_id_seq'::regclass)` |
| SQLite | 字面量 | 字符串带引号 `'x'`，数值为 `0` / `1.5` |

## 完整示例

```go
inspector, err := db.Schema()
if err != nil {
	return err
}

// 遍历所有表及其字段，输出类似建表清单的信息
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
		fmt.Printf("  %s %s %s\n", c.Name, c.Type, null)
	}
}
```
