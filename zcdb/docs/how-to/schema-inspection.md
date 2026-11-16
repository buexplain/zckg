# 任务：用 SchemaInspector 做巡检与代码生成

目标：不手写 `information_schema` / `PRAGMA` 查询，用 `db.Schema()` 拿到表、列、索引元数据，用于生成模型草稿、上线前巡检或迁移比对。

## 结论先行

- `db.Schema()` 按 DAO 的方言自动选择实现；返回 `SchemaInspector` 接口。
- 三个方法：`Tables(ctx)`、`Columns(ctx, table)`、`Indexes(ctx, table)`。
- 不存在的表返回**空结果 + nil 错误**（不报错、不 panic），调用方用空切片判断。
- 元数据是方言原样：MySQL 的 `EXTRA`、PG 的类型/默认值表达式、SQLite 的 `PRAGMA` 结果，不做跨方言归一化。
- Schema 查询走 DAO 的读连接，不追加 DAO 默认注释；慢 SQL 回调能观察到实际 SQL。

## 最小示例：列出表并检查主键

```go
func example() {
	inspector, err := db.Schema()
	if err != nil {
		return
	}

	tables, err := inspector.Tables(ctx)
	if err != nil {
		return
	}
	for _, t := range tables {
		cols, err := inspector.Columns(ctx, t.Name)
		if err != nil {
			return
		}
		var pk []string
		for _, c := range cols {
			if c.PrimaryKey {
				pk = append(pk, c.Name)
			}
		}
		if len(pk) == 0 {
			// 无主键表：批量更新/删除、游标分页都要谨慎
			fmt.Println("no primary key:", t.Name)
		}
	}
}
```

## 典型任务

### 1. 生成模型结构体草稿

`Columns` 给出 `Name`/`Type`/`Nullable`/`Default`/`PrimaryKey`/`Comment`/`Extra`，足以生成带 `db` 标签的结构体草稿：

```go
func example() {
	cols, err := inspector.Columns(ctx, "users")
	if err != nil {
		return
	}
	for _, c := range cols {
		tag := c.Name
		if c.Comment != "" {
			tag += " // " + c.Comment
		}
		fmt.Printf("%s %s `db:%q`\n", toGoName(c.Name), goTypeFor(c.Type), c.Name)
		_ = tag
	}
}
```

需要完整的 Entity/DO 与互转方法生成时，用同仓库的 `zcmodel`；`zcdb.Schema()` 负责提供元数据。

### 2. 上线前巡检

| 检查项 | 用元数据怎么判断 |
|---|---|
| 无主键表 | `Columns` 中没有 `PrimaryKey` 的列 |
| 可空列过多 | `Nullable == true` 的列占比 |
| 缺注释 | `Comment == ""` |
| 索引缺失/重复 | `Indexes` 的列组合与 `Unique` 标志 |
| 自增列 | MySQL `Extra` 含 `auto_increment`；PG 看 `Default` 的 `nextval(...)` |
| 生成列 | MySQL `Extra` 含 `VIRTUAL/STORED GENERATED` |

### 3. 迁移前后比对

分别连接新旧库，拉取同名表的 `Columns`/`Indexes`，按 `Name` 建索引后逐字段比较 `Type`、`Nullable`、`Default`、`Extra`。注意默认值/类型文本是方言原样，跨版本比较时不要做字符串归一化假设。

## 方言注意点

| 方言 | 注意点 |
|---|---|
| MySQL | `Default` 来自 `COLUMN_DEFAULT`（`NULL` 表示无默认值，`AUTO_INCREMENT` 也是 NULL，自增语义在 `Extra`）；`Comment` 有值 |
| PostgreSQL | 默认值是 `pg_get_expr` 表达式（`nextval(...)`、`'x'::text`）；`Extra` 恒空；索引过滤 `INCLUDE` 列 |
| SQLite | 注释恒空；`INTEGER PRIMARY KEY` 是 rowid 别名，`Nullable` 仍为 `true`（元数据局限）；表达式索引以 `#expr` 占位 |

完整字段与底层 SQL 见 [reference/schema](../reference/schema.md)；方言差异全景见 [dialect-matrix](../reference/dialect-matrix.md)。

## 错误与边界

- `db.Schema()` 在 DAO 的 Grammar 不是内置三方言时返回 `ErrSchemaGrammarNotSupported`（消息带实际类型）。
- 元数据查询失败（连接断开、权限不足）返回驱动错误，可用 `errors.Is` 匹配到底层错误。
- 表名大小写、schema 限定：MySQL 用当前 database，PG 固定 `public`，SQLite 只查 `main`；跨 schema 需求要直接用原始 SQL。
- 不存在的表返回空切片，不要用 `len == 0` 反推错误。

## 相关阅读

- API 与返回值：[reference/schema](../reference/schema.md)
- 错误全表：[reference/errors](../reference/errors.md)
- 连接与路由：[concepts/execution-routing](../concepts/execution-routing.md)
