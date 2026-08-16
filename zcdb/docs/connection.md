# 连接、读写分离与事务

本文介绍 `Pool`（连接池）、`DBDao`（数据访问对象）的创建与使用，以及事务、原始 SQL 执行、慢 SQL 回调。

## 创建连接池

`NewPool` 接收 `PoolConfig`，内部完成 `sql.Open → 池参数配置 → Ping` 验证，主库 Ping 失败时直接返回错误。

```go
pool, err := zcdb.NewPool(zcdb.PoolConfig{
	DriverName:            "mysql",   // 驱动名，传给 sql.Open
	DSN:                   "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
	SlaveDSNs:             []string{ // 从库列表，为空则不分读写
		"user:pass@tcp(127.0.0.1:3307)/test?parseTime=true",
	},
	MaxOpenConns:          50,  // 最大打开连接数（主库与每个从库独立应用），默认 50
	MaxIdleConns:          50,  // 最大空闲连接数，默认 50
	ConnMaxLifetimeSecond: 600, // 连接最大存活秒数，默认 600
	ConnectTimeout:        5 * time.Second, // 主库/从库 Ping 验证超时，默认 5s
	SlaveStrategy:         &zcdb.RandomStrategy{}, // 从库选择策略，默认 RandomStrategy
})
```

说明：

- `DriverName` 与 `DSN` 必填，缺失分别返回错误 `zcdb: DriverName is required` / `zcdb: DSN is required`；
- 每个从库 DSN 都会创建独立的 `*sql.DB` 并 Ping 验证，任一失败则整体回滚关闭；
- 主库与从库的 Ping 验证均受 `ConnectTimeout` 限时（默认 5s，`<=0` 取默认值），防止不可达主机静默丢包时启动/热加载长时间挂起；
- 从库选择策略内置两种：`RandomStrategy`（随机，默认）与 `RoundRobinStrategy`（轮询）；也可实现 `SlaveStrategy` 接口自定义（`Pick` 返回 nil 时自动降级到主库）。

DSN 可手写，也可由 `zcconfig.DBConfig` 按驱动自动组装（`GetMasterDSN` / `GetSlaveDSN`，见 zcconfig 文档）：

```go
testDB := zcconfig.Config("database.test_db", zcconfig.DBConfig{})
pool, err := zcdb.NewPool(zcdb.PoolConfig{
    DriverName: testDB.Driver,
    DSN:        testDB.GetMasterDSN(),
    SlaveDSNs:  testDB.GetSlaveDSN(),
})
```

运行期可热加载从库（并发安全）：

```go
err := pool.AddSlave("user:pass@tcp(127.0.0.1:3308)/test?parseTime=true")
```

### 连接池生命周期约定

- **Close 幂等**：首次调用执行实际清理（关闭主库与全部从库），重复调用直接返回 nil；
- **Close 后不得再使用池**：`AddSlave` 在池关闭后返回 `zcdb: pool is closed` 错误；`PickReadDB`/`PickWriteDB` 仍会返回已关闭的连接（返回值签名所限不做快速失败），后续在其上执行的查询将报 `sql: database is closed`，调用方应自行保证 Close 后不再发起请求；
- 推荐的关闭时机：进程退出流程（如 zcquit 的高级别清理 handler）中调用一次 `pool.Close()` 或 `db.Close()`（DAO 的 Close 委托给池）。

## 创建 DAO

`DBDao` 是面向用户的唯一入口，由方言名推导 SQL 编译器：

```go
db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
// dialect 取值：
//   "mysql"                        → MySQLGrammar
//   "postgresql"/"postgres"/"pgsql" → PostgresGrammar
//   "sqlite"/"sqlite3"             → SQLiteGrammar
defer db.Close()
```

第三个参数为慢 SQL 回调（见下文），第四个参数为列映射标签名（见下节，传空串使用默认 `db` 标签）。`dialect` 为空返回 `ErrDialectRequired`，未知方言返回 `ErrUnknownDialect`，`pool` 为 nil 返回 `ErrPoolRequired`。

`db.Pool()` 返回 DAO 持有的底层连接池，供调用方直接管理生命周期（`AddSlave`/`Ping`/`Close`）或获取 `*sql.DB`；`db.Close()` 即委托给该池的 `Close`。

## 自定义列映射标签

结构体与数据库列的映射默认读取 `db` 标签，可在创建 DAO 时通过 `NewDBDao` 的最后一个参数改为任意标签名（如与其它 ORM 共用结构体时复用其标签），初始化后不可变更：

```go
db, err := zcdb.NewDBDao(pool, "mysql", nil, "zc") // 该 DAO 下的结构体映射读取 zc 标签

type User struct {
	Name string `zc:"user_name"` // 映射到 user_name 列
	Age  int    // 无标签，自动转 snake_case → age
}
```

说明：

- 对该 DAO 下所有 Builder 的 Insert/Update/Pluck/Find/First/Paginate/Cursor/CursorBy 等结构体映射生效；
- 无自定义标签的字段仍回退 snake_case 字段名，标签值为 `-` 的字段仍被跳过；
- 标签名在 DAO 创建时确定且不可变更；不同标签名的 DAO 互不影响，各自独立缓存结构体映射；
- 直接使用包级 `ScanStruct`/`ScanStructClose` 扫描原始 SQL 结果时，通过变参指定标签名：

```go
rows, _ := db.Query(ctx, "SELECT user_name, age FROM users")
defer rows.Close()
var users []User
_ = zcdb.ScanStruct(rows, &users, "zc") // 第三个参数指定标签名，缺省为 "db"
```

## 读写分离路由

读写路由对用户完全透明，规则如下：

| 操作 | 目标连接 |
|---|---|
| SELECT（Find/First/Count/Cursor 等） | 从库（按策略选择；无从库时降级主库） |
| SELECT + 锁（LockForUpdate/SharedLock） | **强制主库**（锁在从库不生效） |
| SELECT + `Primary()` | **强制主库**（不加锁，写后读场景：从库可能因复制延迟无数据） |
| INSERT/UPDATE/DELETE/TRUNCATE | 主库 |
| 事务内的一切操作 | 事务连接（主库开启） |

## 事务

`Transaction` 通过 context 传播事务，回调返回 nil 提交、返回 error 回滚、panic 时 defer 兜底回滚：

```go
err := db.Transaction(ctx, func(ctx context.Context) error {
	// 事务内的所有 Builder 执行自动走事务连接
	if _, err := db.Builder().Table("users").
		Where("id", "=", 1).
		Update(ctx, User{Name: "alice"}); err != nil {
		return err // 返回 error → 整体回滚
	}

	// 事务内的读查询也走事务连接（而非从库）
	var user User
	if err := db.Builder().Table("users").
		Where("id", "=", 1).
		First(ctx, &user); err != nil {
		return err
	}
	return nil // 返回 nil → 提交
})
// SQL（事务内依次执行）:
//   UPDATE `users` SET `name` = ? WHERE `id` = ?    args: [alice 1]
//   SELECT * FROM `users` WHERE `id` = ? LIMIT 1    args: [1]
```

嵌套调用时检测到 ctx 中已有事务，直接复用（不会开启新事务），因此业务函数可以自由组合：

```go
err := db.Transaction(ctx, func(ctx context.Context) error {
	// orderService.Create 内部可能也调用了 db.Transaction，会直接复用当前事务
	return orderService.Create(ctx, order)
})
```

## 原始 SQL

不适合用 Builder 表达的语句可直接走 DAO：

```go
// Exec：写语句（走写库或事务连接）
result, err := db.Exec(ctx, "DELETE FROM logs WHERE created_at < ?", cutoff)

// Query：读语句（走从库或事务连接），调用方负责 Close rows
rows, err := db.Query(ctx, "SELECT id, name FROM users WHERE age > ?", 18)
if err != nil {
	return err
}
defer rows.Close()
var users []User
if err := zcdb.ScanStruct(rows, &users); err != nil { // 按 db 标签扫描到结构体切片
	return err
}

// QueryPrimary：强制走写（主库）连接，用于手写带锁查询
rows, err = db.QueryPrimary(ctx, "SELECT * FROM users WHERE id = ? FOR UPDATE", 1)
```

Builder 链式查询需要强制主库时（如订单写入后立刻回读，从库可能尚未同步），用 `Primary()` 标记，不加锁、不改变编译 SQL：

```go
var order Order
err = db.Builder().Table("orders").Where("id", "=", id).Primary().First(ctx, &order)
```

`ScanStruct` 不关闭 rows（调用方负责），`ScanStructClose` 扫描完成后自动关闭，详见[查询执行文档](query-exec.md)。

## 慢 SQL 回调

创建 DAO 时传入回调即可记录每条 SQL 的耗时：

```go
db, err := zcdb.NewDBDao(pool, "mysql",
	func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		if elapsed > 200*time.Millisecond {
			log.Printf("slow sql (%s): %s %v", elapsed, sqlStr, args)
		}
	}, "")
```

`NewDBDao` 的回调参数语义与 Builder 链式构造无关，作用于所有经 DAO 执行的 SQL。`slowSQLMillis` 概念由回调内部自行判断；回调为 nil 时不计时（零开销）。

## 连接探活

```go
err := pool.Ping(ctx) // 依次 Ping 主库与所有从库，任一失败即返回错误
```
