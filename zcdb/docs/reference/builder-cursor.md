# 游标迭代（Cursor / CursorBy）

`Cursor` 与 `CursorBy` 返回 Go 1.23 的 `iter.Seq[error]`：**一次迭代产出一次结果**——成功取到一行就产出 `nil`，出错就产出一个非 nil 错误并结束序列。它们是「大结果集不一次性装进内存」的读取方式，区别在分批策略：

| 方法 | 查询次数 | 适用场景 | 连接占用 |
|---|---|---|---|
| `Cursor` | 1 次 | 中小表全量遍历、需要单条 SQL 的精确排序 | 迭代期间持有 `*sql.Rows`（一条连接），break 释放 |
| `CursorBy` | 每批 1 次 | 大表批量处理、按游标列稳定推进 | 每批独立查询，批间不占用连接 |

行扫描的列映射、NULL 与类型转换规则与 `ScanStruct` 完全一致，见 [scanner](scanner.md)；终端查询方法见 [builder-query](builder-query.md)。

示例统一为 MySQL 形态 SQL、DAO 第五参数为 `""` 的无注释基线；示例包在 `func example()` 中，仅用于语法检查（`db`、`ctx`、`User` 是外部占位）。

## 迭代协议

```text
func (b *Builder) Cursor(ctx context.Context, dest any) iter.Seq[error]
func (b *Builder) CursorBy(ctx context.Context, dest any, chunkSize int, cursorColumn string, desc ...bool) iter.Seq[error]
```

- 惰性执行：调用这两个方法只构造序列，不编译、不查询；`for range` 开始迭代时才真正执行。
- 每成功扫描一行产出 `nil`，循环体负责处理 `dest` 里的当前行；出错时产出一个非 nil 错误，**序列随即结束**，之后不会再产出任何值。
- 错误只产出一次：编译错误、dest 校验错误、查询错误、扫描错误、行集迭代错误都走同一通道。
- 循环体必须检查 `err`；出现错误后继续 range 只会立即结束。
- `break`/`return` 会让 `yield` 返回 false，生成器立刻返回并执行内部 `defer rows.Close()`，连接随之释放。不 break 地读完整个结果集也会释放。

```go
func example() {
	var user User
	for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &user) {
		if err != nil {
			slog.Default().Error("cursor", "err", err)
			break
		}
		fmt.Println(user.Id, user.Name) // 每次迭代覆盖写入同一个 user
	}
}
```

## Cursor

**语义**：编译 `ToSelect` 执行**一次**查询，通过 `*sql.Rows` 逐行扫描进同一个 `dest`，属于服务端游标式读取（驱动是否缓冲由驱动决定，本库不额外分页）。

**dest 要求**：结构体指针（`*User`）。非指针产出 `ErrNotPointer`，指向非结构体（如 `*int`）产出 `ErrNotStruct`。校验在查询**之前**完成，非法 dest 不会浪费一次数据库往返。字段按列映射标签（默认 `db`，可由 `NewDBDao` 的 `tagName` 参数自定义）匹配列名，未匹配的列被忽略。

```go
func example() {
	var user User
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").OrderBy("id", "ASC").Cursor(ctx, &user) {
		if err != nil {
			slog.Default().Error("cursor", "err", err)
			break
		}
		count++
		if count == 2 {
			break // 只取前 2 行：break 会关闭 rows 并释放连接
		}
	}
	// SQL: SELECT `id`, `name` FROM `users` ORDER BY `id` ASC（一次执行，逐行扫描）
}
```

**SQL/结果语义**：Builder 状态原样编译——`Select`、`WHERE`、`JOIN`、`ORDER BY`、`LIMIT`、`UNION`、锁、注释都保留（不像 `CursorBy` 那样改写排序与分页）。`Cursor` 不修改原 Builder。

**边界与错误**：

- 每次迭代覆盖写同一个 `dest`：需要留存某一行时请拷贝结构体值（`saved := user`），不要保存 `&user`——那只会拿到最后一次迭代的内容。
- NULL 扫描为零值（非指针字段）或 nil（指针字段）；nil 嵌入指针使某列不可用时该列被忽略，不报错、不 panic。
- 行扫描失败产出 `zcdb: cursor scan failed: %w`，错误链中保留 `ErrScanConvert`（可用 `errors.Is` 匹配）。
- 迭代期间行集出错（连接中断、`ctx` 取消）在循环结束后经 `rows.Err()` 产出，不会被静默当成「正常读完」。
- 编译错误（未设表、SQLite 锁、PG UNION + 锁等）在第一次迭代就产出，不会有任何数据库往返。

## CursorBy

**语义**：基于游标列的键集分页（keyset pagination），通过 `WHERE 游标列 > 上批最后值` 逐批推进，每批独立执行、不长时间占用连接。适用于大表批量处理。

**游标列要求**：`cursorColumn` 必须是**有序且唯一**的列（通常为主键）；不唯一会导致批边界漏行或重复。`dest` 结构体必须包含该列对应的字段，查找顺序为：

1. 列名映射（`db` 标签值或字段名的 snake_case）；
2. 顶层字段名精确匹配（大小写敏感）；
3. 带表限定的 `table.column` 先取点号后的列名，再回到第 1 步映射。

`dest` 的其它要求同 `Cursor`（结构体指针，`ErrNotPointer`/`ErrNotStruct`）。`chunkSize` 为 0 时在执行任何校验之前就直接结束序列——非法 `dest` 也不会产出错误。

**chunkSize 语义**：

| `chunkSize` | 行为 |
|---|---|
| `0` | 直接返回，不执行任何查询，也不做 dest/游标列校验 |
| `< 0` | 视为默认值 `100` |
| `> 0` | 每批**实际取 `chunkSize + 1` 行**（SQL 为 `LIMIT chunkSize+1`） |

```go
func example() {
	var user User
	for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id") {
		if err != nil {
			slog.Default().Error("cursorBy", "err", err)
			break
		}
		fmt.Println(user.Id, user.Name)
	}
	// 首批 SQL: SELECT * FROM `users` ORDER BY `id` ASC LIMIT 101
	// 后续批次: SELECT * FROM `users` WHERE `id` > ? ORDER BY `id` ASC LIMIT 101
	// args:    [上批最后一行的 id]
}
```

**SQL/结果语义**：

- 每批通过 `Clone()` 派生查询，原 Builder 不被修改：`limit` 被覆盖为 `chunkSize+1`，`orders` 被清空后只按游标列排序，已有 `WHERE`/`JOIN`/`SELECT`/注释保留；游标条件以 `AND 游标列 > ?` 追加。
- **多取一条做探测**：第 `chunkSize+1` 行说明还有下一页，它不扫描、不产出给调用方、不更新游标值；下一批从本批最后一行的游标值继续取，把这一行重新取回——不丢数据、不重复。这样数据量恰为 `chunkSize` 整数倍时也不会多执行一次返回 0 行的空查询（例如 6 行、`chunkSize=3` 恰好 2 次查询）。
- **忽略**已设置的 ORDER BY（`orders` 被清空），强制按游标列单键排序；`desc` 变体把比较符换成 `<`、排序换成 `DESC`。`desc` 只读取第一个变参，多传无意义。
- 注释与回调：`Clone` 保留当前注释（包括显式清空），每批 SQL **各在最终编译出口追加一次**，慢 SQL 回调逐批观测到各自的完整 SQL 与原绑定参数；`Cursor` 是单条 SQL 追加一次，不是逐行追加。原 Builder 与 DAO 默认注释都不被修改。

```go
func example() {
	var user User
	for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id", true) {
		if err != nil {
			slog.Default().Error("cursorBy", "err", err)
			break
		}
		fmt.Println(user.Id)
	}
	// 首批 SQL: SELECT * FROM `users` ORDER BY `id` DESC LIMIT 101
	// 后续批次: SELECT * FROM `users` WHERE `id` < ? ORDER BY `id` DESC LIMIT 101
}
```

**边界与错误**：三类游标专有错误（均可用 `errors.Is` 匹配）由生成器产出后序列立即结束——已经成功产出的行不会被撤回，错误之前的行已交付给调用方。`ErrCursorFieldNotFound` 与 `ErrCursorFieldUnavailable` 在扫描首行之前或首行判定，`ErrCursorColumnNull` 可能出现在任意一行。

| 错误 | 触发条件 | 为什么必须终止 |
|---|---|---|
| `ErrCursorColumnNull` | 扫描出的游标列值为 NULL | `游标列 > NULL` 永不为真，继续会无限重复同一批 |
| `ErrCursorFieldNotFound` | `dest` 结构体找不到游标列对应的字段 | 没有回传游标值的通道 |
| `ErrCursorFieldUnavailable` | 游标列字段不可用（如 nil 嵌入指针） | 取值会 panic 或被静默当成 NULL |

- 其它错误：`ErrNotPointer`/`ErrNotStruct`（dest 非法）、编译错误、查询错误、`zcdb: cursor scan failed: %w`（行扫描失败）、批内行集错误。
- 批内出错（连接中断、`ctx` 取消）会先取 `rows.Err()` 再上报，不会被当成「本批已读完」而静默截断；下一批的查询层报错是第二道上报通道。
- 游标列值取自 `dest` 字段**扫描后**的值（已按字段类型转换过），不是驱动返回的原始值；因此下一批的绑定参数类型是结构体字段类型。
- 只覆盖 `limit` 与 `orders`，**不重置 `offset`**：Builder 上已设置 `Offset` 时每批都会带同一个 OFFSET，分批迭代应避免混用。
- 只支持单列游标；复合游标（如 `(created_at, id)`）需要自行用 `WhereRaw` 分页，不要交给 `CursorBy`。
- 每批都是独立查询，批与批之间数据可能变化：并发插入/删除会产生「快照不一致」，不保证跨批的强一致视图。

## 相关阅读

- 结构体映射与类型转换：[scanner](scanner.md)、[mapping](../concepts/mapping.md)
- 原 Builder 的克隆与状态：[builder-state](../concepts/builder-state.md)
- 连接选择与锁路由：[execution-routing](../concepts/execution-routing.md)
- 单次查询的终端方法：[builder-query](builder-query.md)
- 分页与大批量处理的取舍：[pagination](../how-to/pagination.md)
- 方法总览：[terminal-methods](terminal-methods.md)
