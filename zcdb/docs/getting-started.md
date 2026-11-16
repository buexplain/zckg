# 从零跑通 zcdb：内存 SQLite 完整教程

这份教程用一个**可直接运行**的程序，把 zcdb 的主干链路一次走完：建池 → 建 DAO → 建表 → 写 → 读 → 改 → 分页 → 游标 → 事务 → 慢 SQL 回调。数据库用纯 Go 的 `modernc.org/sqlite` 内存库，不需要安装任何外部服务。

## 结论先行

- 固定五步：**引入驱动 → `zcdb.NewPool` → `zcdb.NewDBDao` → `db.Builder()` 链式构造 → 终端方法执行**。
- 内存 SQLite 的 `:memory:` 是「每个连接一个独立数据库」，必须 `MaxOpenConns: 1`，否则建表和查询可能落在不同连接上（报 `no such table`）。
- 读写都走 `DBDao`；`ctx` 从入口贯穿到执行层，事务正是通过 `ctx` 传播的。
- `Paginate` 会先执行一次 `COUNT`，再执行数据查询；不需要总数时用 `ForPage + Find`。
- 写入模型不要带零值自增主键列：zcdb 会把零值列也写进 `INSERT`。
- 事务回调里必须使用它传入的 `ctx`，否则语句在事务之外执行。

## 环境准备

要求 Go 1.25 及以上（模块 `go.mod` 当前为 `go 1.25.11`）。新建一个空目录：

```shell
mkdir zcdb-quickstart && cd zcdb-quickstart
go mod init quickstart
go get github.com/buexplain/zckg/zcdb
go get modernc.org/sqlite
```

`zcdb` 本身不绑定驱动，所以必须 blank import 一个 `database/sql` 驱动；本教程用 `modernc.org/sqlite`（注册名 `sqlite`），它不需要 CGO。

## 完整程序

把下面内容保存为 `main.go`，并在文件第一行补上 `package main`（本仓库的文档门禁会统一按 `package p` 做语法解析，因此示例块本身省略 `package` 子句）：

```go
// 完整程序。实际运行时请在文件第一行补：package main
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/buexplain/zckg/zcdb"
	_ "modernc.org/sqlite" // 注册 sqlite 驱动（纯 Go，无需 CGO）
)

// User 是读取模型：包含全部列。
type User struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
	Age  int    `db:"age"`
}

// NewUser 是写入模型：故意不含 Id，避免把零值 0 显式写进自增主键。
type NewUser struct {
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func main() {
	ctx := context.Background()

	// 1. 建池 + DAO。SQLite 的 :memory: 是「每连接一个独立内存库」，
	//    必须把最大连接数限制为 1，否则建表连接与查询连接看到的不是同一个库。
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName:   "sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		panic(err)
	}
	defer pool.Close() // db.Close() 委托给同一个池；这里保留一个即可

	db, err := zcdb.NewDBDao(pool, "sqlite", nil, "", "")
	if err != nil {
		panic(err)
	}

	// 2. 建表：DDL 直接走 DAO.Exec（原始 SQL 原文透传）。
	if _, err := db.Exec(ctx, `CREATE TABLE users (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		age  INTEGER NOT NULL
	)`); err != nil {
		panic(err)
	}
	fmt.Println("建表完成")

	// 3. 批量插入：返回受影响行数。
	affected, err := db.Builder().Table("users").Insert(ctx, []NewUser{
		{Name: "alice", Age: 25},
		{Name: "bob", Age: 30},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("批量插入行数:", affected)

	// 4. 单条插入并拿回自增 ID。
	carolID, err := db.Builder().Table("users").InsertGetId(ctx, NewUser{Name: "carol", Age: 41})
	if err != nil {
		panic(err)
	}
	fmt.Println("carol 的 id:", carolID)

	// 5. Find：多条记录，按 id 稳定排序。
	var adults []User
	if err := db.Builder().Table("users").Where("age", ">", 25).OrderBy("id", "ASC").Find(ctx, &adults); err != nil {
		panic(err)
	}
	fmt.Println("age > 25:", adults)

	// 6. First：单条记录；未命中返回 sql.ErrNoRows。
	var alice User
	if err := db.Builder().Table("users").Where("id", "=", 1).First(ctx, &alice); err != nil {
		panic(err)
	}
	fmt.Println("id=1:", alice)

	// 7. Update 必须带 WHERE；无 WHERE 会被拒绝（ErrUpdateWithoutWhere）。
	updated, err := db.Builder().Table("users").Where("id", "=", 1).Update(ctx, struct {
		Age int `db:"age"`
	}{Age: 26})
	if err != nil {
		panic(err)
	}
	fmt.Println("更新行数:", updated)

	// 8. Paginate：先 COUNT 再取当前页；total 是满足 WHERE 的总行数。
	var page []User
	total, err := db.Builder().Table("users").OrderBy("id", "ASC").ForPage(1, 2).Paginate(ctx, &page)
	if err != nil {
		panic(err)
	}
	fmt.Printf("total=%d page1=%v\n", total, page)

	// 9. Cursor：一次查询、逐行扫描；循环 break 时自动释放连接。
	var u User
	var names []string
	for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &u) {
		if err != nil {
			panic(err)
		}
		names = append(names, u.Name)
	}
	fmt.Println("cursor:", names)

	// 10. 事务：回调返回 nil 提交，返回 error 回滚。
	err = db.Transaction(ctx, func(txCtx context.Context) error {
		if _, err := db.Builder().Table("users").Insert(txCtx, NewUser{Name: "dave", Age: 33}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	rollbackErr := db.Transaction(ctx, func(txCtx context.Context) error {
		if _, err := db.Builder().Table("users").Insert(txCtx, NewUser{Name: "eve", Age: 20}); err != nil {
			return err
		}
		return errors.New("模拟业务失败")
	})
	count, err := db.Builder().Table("users").Count(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Printf("rollback=%v count=%d\n", rollbackErr, count)

	// 11. 慢 SQL 回调：换一个 DAO，回调里按 elapsed 自行判断阈值。
	slowDB, err := zcdb.NewDBDao(pool, "sqlite",
		func(ctx context.Context, grammar zcdb.Grammar, elapsed time.Duration, sqlStr string, args []any) {
			// 真实项目用 elapsed 过滤慢 SQL；这里无条件打印，保证示例输出稳定。
			fmt.Printf("[SQL] %s args=%v\n", sqlStr, args)
		}, "", "")
	if err != nil {
		panic(err)
	}
	n, err := slowDB.Builder().Table("users").Where("age", ">", 30).Count(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("age > 30 的人数:", n)
}
```

## 逐步说明

### 第 1 步：创建连接池与 DAO

`zcdb.NewPool` 接收 `zcdb.PoolConfig`，内部依次执行 `sql.Open → 设置池参数 → Ping`，主库 Ping 失败直接返回错误（`ErrPoolConnect`，底层驱动错误仍可用 `errors.Is` 匹配）。`DriverName` 与 `DSN` 必填。

`zcdb.NewDBDao(pool, dialect, onSQL, tagName, comment)` 是五参数接口：

| 参数 | 本教程取值 | 作用 |
|---|---|---|
| `pool` | 上一步的 `*zcdb.Pool` | 提供主从连接 |
| `dialect` | `"sqlite"` | 决定 Grammar；`sqlite`/`sqlite3` 等价 |
| `onSQL` | `nil` | 慢 SQL 回调，nil 表示不计时 |
| `tagName` | `""` | 列映射标签名，空串使用 `db` |
| `comment` | `""` | DAO 默认 SQL 注释，空串表示不追加 |

**内存库陷阱**：`modernc.org/sqlite` 的 `:memory:` 每个连接各有一个独立数据库。`NewPool` 默认 `MaxOpenConns=50`，一旦并发或连接复用切换，就会出现「建表成功、查询却报 `no such table: users`」。本教程显式设为 1，所有语句串行走同一个连接。需要多连接并发时，改用文件 DSN 或 `file::memory:?cache=shared`（共享缓存模式），并自行评估 SQLite 的并发限制。

**预期结果**：两个构造函数都不返回错误，`NewPool` 内部已完成主库 Ping。`DriverName` 为空返回 `ErrDriverNameRequired`，`DSN` 为空返回 `ErrDSNRequired`，连接失败返回 `ErrPoolConnect`。

### 第 2 步：建表（DAO.Exec）

DDL 不适合用 Builder 表达，直接走 `db.Exec`：

```sql
CREATE TABLE users (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    age  INTEGER NOT NULL
)
```

`Exec` 是原始 SQL 入口：**原文透传**，不追加 DAO 默认注释、不转换占位符（SQLite 用 `?`），有事务时走事务连接，否则走主库。它返回 `sql.Result`，需要行数时调用 `RowsAffected()`。

### 第 3 步：写入（Insert / InsertGetId）

- `Insert(ctx, data)` 支持 `struct`、`*struct`、`[]struct`、`[]*struct`，返回 `(int64, error)`（受影响行数）。
- `InsertGetId(ctx, data)` 只接受单个结构体或其指针，返回自增 ID；PostgreSQL 方言不支持（`ErrInsertGetIdUnsupported`）。
- zcdb 不会跳过零值列：`User{Name: "alice"}` 会把 `Id=0` 一起写进 `INSERT`。因此教程把写入模型拆成不含 `Id` 的 `NewUser`；如果希望某列不参与写入，用指针或 `any` 字段并置 nil（nil 列会被跳过）。

批量插入与单条插入的 SQL（SQLite 形态）：

```sql
-- Insert([]NewUser{{alice,25},{bob,30}})
INSERT INTO "users" ("name", "age") VALUES (?, ?), (?, ?)
-- args: [alice 25 bob 30]

-- InsertGetId(NewUser{carol,41})
INSERT INTO "users" ("name", "age") VALUES (?, ?)
-- args: [carol 41]，LastInsertId() = 3
```

### 第 4 步：查询（Find / First）

| 方法 | dest | 无结果时 |
|---|---|---|
| `Find` | `*[]User` 或 `*[]*User` | 空切片，`err == nil` |
| `First` | `*User` | `sql.ErrNoRows` |

`First` 内部按需克隆 Builder 并补 `LIMIT 1`（limit 已为 1 时复用原对象），不修改原对象；`Find` 不克隆，也不附加 `LIMIT`。分页/多页场景必须自己写 `OrderBy`，否则数据库返回顺序没有保证。

```sql
-- Find
SELECT * FROM "users" WHERE "age" > ? ORDER BY "id" ASC
-- args: [25]

-- First
SELECT * FROM "users" WHERE "id" = ? LIMIT 1
-- args: [1]
```

### 第 5 步：更新（Update 必须带 WHERE）

`Update` 只接受单个结构体或其指针，返回受影响行数。**没有有效 WHERE 时默认拒绝执行**，返回 `ErrUpdateWithoutWhere`；确需全表更新必须显式 `Force()` 或 `WhereRaw("1=1")`。

```sql
UPDATE "users" SET "age" = ? WHERE "id" = ?
-- args: [26 1]
```

只更新部分列时，把不需要更新的字段设为指针 nil 或 `any` nil（nil 列会被跳过）；示例里只声明了 `Age` 字段，所以 `SET` 只有一列。

### 第 6 步：分页（Paginate）

`Paginate(ctx, &dest)` 返回 `(totalCount int, err error)`，内部是两次查询：

1. `Count`：统计满足当前 WHERE 的总行数；
2. 数据查询：按当前 `ORDER BY` 与 `ForPage`/`Limit+Offset` 取当前页。

```sql
-- COUNT
SELECT COUNT(*) FROM "users"

-- 数据（ForPage(1, 2)）
SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2
```

`ForPage(page, perPage)` 从 1 开始计页，等价于 `Limit(perPage).Offset((page-1)*perPage)`；第 1 页的 offset 为 0，不会输出 `OFFSET`。`perPage <= 0` 时不输出 LIMIT/OFFSET，等于不分页。

`Paginate` **不会设置默认页大小**：没有通过 `ForPage` 或 `Limit`/`Offset` 设置分页范围时，它会正常 COUNT，然后把整张表查出来。`total == 0` 时不再执行数据查询，直接返回 `(0, nil)`。

### 第 7 步：游标遍历（Cursor）

`Cursor(ctx, &user)` 返回 Go 1.23 的 `iter.Seq[error]`：**一次查询**，用 `*sql.Rows` 逐行扫描，每次迭代把当前行覆盖写入同一个 `dest`。迭代期间持有数据库连接，循环 `break` 时自动关闭 rows。

```sql
SELECT * FROM "users" ORDER BY "id" ASC
```

适合中小表全量遍历或需要精确排序的场景；大表分批处理改用 `CursorBy`（每批独立查询，不长时间占用连接），见[分页任务指南](how-to/pagination.md)。

### 第 8 步：事务（Transaction）

`db.Transaction(ctx, fn)` 的回调签名是 `func(ctx context.Context) error`：

- 返回 `nil` → `Commit`；
- 返回 `error` → `Rollback`，并把原始 error 返回给调用方；
- `panic` → `defer` 兜底 `Rollback`，然后 panic 继续向上传播。

事务通过 `ctx` 传播：回调里所有 Builder 执行和 `db.Exec`/`db.Query` 都必须使用回调传入的 `txCtx`，才会命中事务连接（包括读查询，因此能看到未提交的写入）。用外层 `ctx` 会落到普通连接上，既不受事务保护，也可能与单连接池互相等待。

本教程先提交一条 `dave`，再让事务返回 `errors.New("模拟业务失败")` 回滚 `eve`，所以最终 `count=4`。事务的嵌套、跨 Pool 边界与锁行为见[事务任务指南](how-to/transactions.md)。

### 第 9 步：慢 SQL 回调

`NewDBDao` 的第三个参数类型是 `zcdb.SlowSQLCallback`：

```go
func onSQL(ctx context.Context, grammar zcdb.Grammar, elapsed time.Duration, sqlStr string, args []any) {
	// 回调体：按 elapsed 判断是否记录，sqlStr/args 是实际执行的 SQL 与绑定参数
}
```

- 只在回调非 nil 时计时；nil 时零开销；
- 在语句执行后调用，拿到**实际执行的 SQL**（Builder 产物包含最终注释）与原始绑定参数；
- 回调 panic 会被 `recover` 隔离，不影响主流程；
- 阈值判断由回调内部根据 `elapsed` 决定，zcdb 不内置 slow 概念；
- 一次方法调用可能触发多次回调：`Paginate` 在 `total > 0` 时 COUNT 与数据查询各一次，`CursorBy` 每批一次。

示例里换了一个带回调的 DAO，执行：

```sql
SELECT COUNT(*) FROM "users" WHERE "age" > ?
-- args: [30]
```

## 运行与预期输出

在补好 `package main` 的目录执行 `go run .`，输出应逐行等于：

```text
建表完成
批量插入行数: 2
carol 的 id: 3
age > 25: [{2 bob 30} {3 carol 41}]
id=1: {1 alice 25}
更新行数: 1
total=3 page1=[{1 alice 26} {2 bob 30}]
cursor: [alice bob carol]
rollback=模拟业务失败 count=4
[SQL] SELECT COUNT(*) FROM "users" WHERE "age" > ? args=[30]
age > 30 的人数: 2
```

输出中的 ID 与顺序是确定的：`:memory:` 每次启动都是空库，插入顺序固定，查询都带了 `ORDER BY id`。

## 常见坑

- **`no such table` / 数据查不到**：`:memory:` 配了多个连接。把 `MaxOpenConns` 设为 1，或改用文件 DSN/共享缓存。
- **`UPDATE` 被拒绝**：忘记 WHERE。补条件，或明确调用 `Force()`（全表操作）。
- **自增 ID 为 0 或主键冲突**：写入结构体带了零值 `Id`。拆分写入模型，或用指针/`any` 字段置 nil。
- **分页结果重复/漏行**：没有 `ORDER BY`，或排序键不唯一。分页排序应带唯一列兜底（如 `ORDER BY created_at DESC, id DESC`）。
- **复用目标切片**：`Find`/`Paginate` 对 `*[]struct` 是**追加**语义，不重置切片；同一个切片反复接结果会越积越多。每次用新变量，或先 `users = users[:0]`。
- **事务里用了外层 ctx**：语句跑在事务外，回滚带不走它们。统一使用回调的 `txCtx`。
- **`Paginate` 结果比预期多**：忘了 `ForPage`/`Limit+Offset`；它不会套默认页大小。
- **`database is locked` / 一直等待**：单连接池里同时开事务又发起池上查询，或游标未关闭就执行下一条语句。SQLite 场景先串行化，游标/rows 用完即关。

## 下一步读什么

| 想继续了解 | 去哪里 |
|---|---|
| Builder 为什么是可变状态、哪些方法会克隆 | [concepts/builder-state.md](concepts/builder-state.md) |
| SQL 如何编译、绑定参数顺序 | [concepts/compile-pipeline.md](concepts/compile-pipeline.md)、[reference/builder-compile.md](reference/builder-compile.md) |
| 结构体与列如何映射、NULL/类型转换 | [concepts/mapping.md](concepts/mapping.md)、[reference/scanner.md](reference/scanner.md) |
| 读写分离、事务与连接路由 | [concepts/execution-routing.md](concepts/execution-routing.md)、[how-to/transactions.md](how-to/transactions.md) |
| 分页、深页与游标选型 | [how-to/pagination.md](how-to/pagination.md)、[reference/builder-cursor.md](reference/builder-cursor.md) |
| 终端方法逐个说明 | [reference/terminal-methods.md](reference/terminal-methods.md)、[reference/builder-query.md](reference/builder-query.md) |
| 无 WHERE 保护、Expression、注释边界 | [concepts/safety.md](concepts/safety.md) |
| 三方言差异 | [reference/dialect-matrix.md](reference/dialect-matrix.md) |
| 错误哨兵速查 | [reference/errors.md](reference/errors.md) |
| 旧版本升级 | [migration/breaking-changes.md](migration/breaking-changes.md) |
| 文档总地图 | [README.md](README.md) |
