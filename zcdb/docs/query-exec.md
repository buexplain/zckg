# 查询执行

本文介绍 Builder 的终端查询方法：`Find/First/Value`、`Count/Exists`、聚合、`Pluck`、`Paginate`、游标迭代（`Cursor/CursorBy`）以及原始 Rows 扫描工具（`ScanStruct/ScanStructClose`）。

所有读操作默认路由到从库（见[连接文档](connection.md)）；带锁查询（`LockForUpdate`/`SharedLock`）与 `Primary()` 标记强制走主库——前者因锁在从库不生效，后者用于写后读场景（从库可能因复制延迟无数据）。以下示例统一给出 MySQL 形态 SQL。

## Find / First / Value

三者都是"执行 SELECT 并扫描结果"的方法，区别在于结果基数：

| 方法 | dest 类型 | 无结果时的行为 |
|---|---|---|
| `Find` | `*[]struct` 或 `*[]*struct` | 空切片、err 为 nil |
| `First` | `*struct` | 返回 `sql.ErrNoRows` |
| `Value` | 基本类型指针（`*string`、`*int64`…）或二级指针 | 返回 `sql.ErrNoRows` |

`First` 与 `Value` 内部按需克隆并强制附加 `LIMIT 1`（limit 已为 1 时跳过克隆），不修改原对象。

```go
// Find：多条记录
var users []User
err := db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &users)
// SQL:  SELECT * FROM `users` WHERE `status` = ?
// args: [active]

// First：单条记录，未命中返回 sql.ErrNoRows
var user User
err = db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)
// SQL:  SELECT * FROM `users` WHERE `id` = ? LIMIT 1
// args: [1]

// Value：单个标量值
var name string
err = db.Builder().Table("users").Select("name").Where("id", "=", 1).Value(ctx, &name)
// SQL:  SELECT `name` FROM `users` WHERE `id` = ? LIMIT 1
// args: [1]
```

`Value` 的 dest 支持二级指针用于区分 NULL 与空字符串：

```go
var remark *string
err = db.Builder().Table("users").Select("remark").Where("id", "=", 1).Value(ctx, &remark)
// err == nil 且 remark == nil → 该列值为 NULL
// err == nil 且 remark != nil → 正常字符串
```

## Count / Exists

```go
count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
// SQL:  SELECT COUNT(*) FROM `users` WHERE `status` = ?
// args: [active]

exists, err := db.Builder().Table("users").Where("email", "=", "a@t.com").Exists(ctx)
// SQL:  SELECT 1 FROM `users` WHERE `email` = ? LIMIT 1
// args: [a@t.com]
```

- `Count`：包含 UNION / GROUP BY / DISTINCT 时自动包裹子查询计数，保留分组/去重语义（见[编译文档](compile.md)的 ToCount）；
- `Exists`：走 `SELECT 1 ... LIMIT 1`，命中首行即返回，大表存在性检查显著快于 `Count`。

## Max / Min / Sum / Avg

聚合方法返回 `float64`，生成 `SELECT AGG(col) AS aggregate FROM ...`：

```go
maxAge, err := db.Builder().Table("users").Max(ctx, "age")
// SQL: SELECT MAX(`age`) AS `aggregate` FROM `users`

total, err := db.Builder().Table("orders").Where("status", "=", "paid").Sum(ctx, "amount")
// SQL:  SELECT SUM(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
// args: [paid]
```

空集语义（SQL 聚合函数对空集返回 NULL 的归一化处理）：

| 方法 | 空表/无匹配行时 |
|---|---|
| `Max` / `Min` | 返回 `(0, sql.ErrNoRows)` |
| `Sum` / `Avg` | 返回 `(0, nil)` |

## Pluck

`Pluck` 将查询结果的列值提取到切片或 map，有三种模式：

**模式一：切片提取单列**（dest 为切片指针，columns 只能传一列）

```go
var names []string
err := db.Builder().Table("users").Where("vip", "=", 1).Pluck(ctx, &names, "name")
// SQL: SELECT `name` FROM `users` WHERE `vip` = ?
// args: [1]
```

**模式二：map 键值对**（dest 为 map 指针，columns 传两列：第一列为值、第二列为键）

```go
var m map[int64]string
err := db.Builder().Table("users").Pluck(ctx, &m, "name", "id") // id => name
// SQL: SELECT `name`, `id` FROM `users`
```

**模式三：map 结构体（keyBy）**（dest 为 `map[K]struct`/`map[K]*struct`，columns 只传键列，整行按 db 标签扫描进结构体）

```go
var m map[int64]User
err := db.Builder().Table("users").Pluck(ctx, &m, "id") // id => User 整行
// SQL: SELECT `id`, `name`, `age` FROM `users`   // SELECT 列 = 结构体字段列（键列已在字段中则不重复）
```

注意事项：

- Pluck 会**覆盖** Builder 当前的 SELECT 列；
- NULL 值扫描为零值；map 模式重复键后者覆盖前者，键列为 NULL 时使用零值键；
- keyBy 模式下键列若不在结构体字段中，会追加为 SELECT 的最后一列单独扫描。

## Paginate

分页查询并自动统计总数，返回 `(totalCount, err)`：

```go
var users []User
total, err := db.Builder().Table("users").
	Where("status", "=", "active").
	OrderBy("id", "ASC").
	ForPage(2, 20). // 第 2 页、每页 20 条
	Paginate(ctx, &users)
// COUNT SQL: SELECT COUNT(*) FROM `users` WHERE `status` = ?           args: [active]
// 数据 SQL:  SELECT * FROM `users` WHERE `status` = ? ORDER BY `id` ASC LIMIT 20 OFFSET 20
```

内部行为：

- 先**克隆** Builder 并清除 orders/limit/offset/columns 后执行 COUNT（原 Builder 不受影响）；
- `totalCount == 0` 时不再执行数据查询，直接返回；
- 分页范围需预先通过 `ForPage` 或 `Limit+Offset` 设置。

## Cursor（流式迭代）

`Cursor` 返回 Go 1.23 的 `iter.Seq[error]` 迭代器：一次查询、通过 `*sql.Rows` 逐行扫描，适合中小表全量遍历。迭代期间持有数据库连接，循环 break 时自动释放。

```go
var user User
for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &user) {
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(user.Name) // 每次迭代覆盖写入同一个 user
}
// SQL: SELECT * FROM `users` ORDER BY `id` ASC（一次执行，逐行扫描）
```

dest 要求：结构体指针（如 `*User`），字段按 db 标签匹配列名，未匹配的列被忽略。

## CursorBy（游标分批迭代）

`CursorBy` 通过 `WHERE cursorColumn > lastValue` 分批查询，每批独立执行、不长时间占用连接，适合大表批量处理：

```go
var user User
for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id") {
	if err != nil {
		log.Fatal(err)
	}
	process(user)
}
// 首批 SQL: SELECT * FROM `users` ORDER BY `id` ASC LIMIT 101
// 后续批次: SELECT * FROM `users` WHERE `id` > ? ORDER BY `id` ASC LIMIT 101   args: [上批最后一行的 id]
```

参数与规则：

- `cursorColumn` 必须是**有序且唯一**的列（通常为主键），结构体必须包含该列对应的字段；
- `chunkSize` 为 0 时直接返回（不执行任何查询），小于 0 时使用默认值 100；
- 每批实际取 `chunkSize + 1` 条（SQL 中为 `LIMIT chunkSize+1`），多取的一条用于探测是否还有下一页：探测行不产出给调用方，下一批从本批最后一行的游标值继续取回该行，不丢数据；这避免了数据量恰为 chunkSize 整数倍时多执行一次返回 0 行的空查询；
- **忽略**已设置的 ORDER BY，强制按游标列排序；
- 变参 `desc` 为 true 时倒序分批（条件变为 `<`、排序 `DESC`）：

```go
for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id", true) {
	// 首批 SQL: SELECT * FROM `users` ORDER BY `id` DESC LIMIT 101
	// 后续批次: SELECT * FROM `users` WHERE `id` < ? ORDER BY `id` DESC LIMIT 101
}
```

错误终止：游标列值为 NULL 时报 `ErrCursorColumnNull`（否则条件 `col > NULL` 恒假会无限重复同一批）；结构体找不到游标列字段时报 `ErrCursorFieldNotFound`；游标列字段不可用（如 nil 嵌入指针结构体）时报 `ErrCursorFieldUnavailable`。

## ScanStruct / ScanStructClose

配合 [dao.Query 原始 SQL](connection.md#原始-sql) 使用的行扫描工具，按 db 标签（或 snake_case 字段名）将 `*sql.Rows` 扫描到结构体，支持嵌入结构体字段展开：

```go
rows, err := db.Query(ctx, "SELECT id, name, age FROM users WHERE age > ?", 18)
if err != nil {
	return err
}
defer rows.Close() // ScanStruct 不关闭 rows，调用方负责

var users []User
if err := zcdb.ScanStruct(rows, &users); err != nil {
	return err
}

// 扫描单行：dest 传 *struct，未找到返回 sql.ErrNoRows
var user User
if err := zcdb.ScanStruct(rows, &user); err != nil {
	return err
}
```

`ScanStructClose` 是自动关闭 rows 的便捷包装：

```go
rows, err := db.Query(ctx, "SELECT id, name FROM users WHERE id = ?", 1)
if err != nil {
	return err
}
var user User
err = zcdb.ScanStructClose(rows, &user) // 扫描完成后自动 rows.Close()，无需 defer
```

| dest 类型 | 行为 |
|---|---|
| `*struct` | 扫描第一行，未找到返回 `sql.ErrNoRows` |
| `*[]struct` / `*[]*struct` | 扫描所有行，无结果时为空切片 |

扫描转换说明：未匹配的列被忽略；NULL 扫描为零值（指针字段置 nil）；数值文本（驱动以字符串/[]byte 返回的数值列）按目标字段类型位宽解析，**溢出时报错**（如 `"300"` 扫入 int8），不静默截断；时间列扫入 string 字段时格式化为 RFC3339。

### 错误语义

ScanStruct 系列可能返回以下哨兵错误（均可用 `errors.Is` 匹配）：

| 错误 | 含义 |
|------|------|
| `ErrNotPointer` | dest 不是指针 |
| `ErrScanDest` | dest 是指针但指向的类型非法（必须是 `*struct` 或 `*[]struct` / `*[]*struct`） |
| `ErrInvalidStruct` | 切片元素不是结构体/结构体指针 |
| `ErrScanConvert` | 列值转换为目标字段类型失败（数值溢出、JSON 反序列化失败、类型不可转换等） |
| `sql.ErrNoRows` | `*struct` 模式下查询结果为空 |

示例：

```go
if err := zcdb.ScanStruct(rows, &user); err != nil {
    if errors.Is(err, zcdb.ErrScanConvert) {
        log.Printf("数据转换失败: %v", err)
    }
    if errors.Is(err, sql.ErrNoRows) {
        log.Println("未找到记录")
    }
}
```
