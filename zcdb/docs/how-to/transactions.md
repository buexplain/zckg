# 事务：提交、回滚、嵌套与跨 Pool 边界

zcdb 的事务只有一个入口：`DBDao.Transaction`。它用 `context` 传播事务句柄，因此**事务边界由 ctx 决定**——回调里用了哪个 ctx，语句就落在哪条连接上。这份指南覆盖提交/回滚/panic 语义、嵌套复用、跨 Pool 拒绝、事务内读与锁，以及最常见的误用。

## 结论先行

- `Transaction(ctx, fn)`：`fn` 返回 `nil` → 提交；返回 `error` → 回滚；`panic` → 先兜底回滚，再继续向上 panic。
- 回调内**必须使用它传入的 `ctx`**；用外层 ctx 的语句不在事务里，回滚带不走。
- 同一 `Pool` 上嵌套调用 `Transaction` 会**复用当前事务**，不会开启新事务，也没有保存点（savepoint）。
- 把事务 ctx 传给**另一个 Pool** 的 DAO，任何执行入口都返回 `ErrTxOwnerMismatch`，SQL 不执行、回调不触发；zcdb 不提供跨库原子事务。
- 事务内的读与写都走同一个事务连接（由主库开启），`Primary()` 不改变路由也不改 SQL；锁子句会编译进 SQL，但只在事务内才有互斥意义。
- 开启/提交/回滚失败分别返回 `ErrTxBegin`/`ErrTxCommit`/`ErrTxRollback`，被包装的底层错误仍可用 `errors.Is` 匹配。

## Transaction 的契约

签名：

```go
func (d *DBDao) Transaction(ctx context.Context, fn func(ctx context.Context) error) error
```

| 阶段/回调结果 | 事务动作 | `Transaction` 返回值 |
|---|---|---|
| `BeginTx` 失败 | 不执行回调 | `ErrTxBegin`（包装底层错误） |
| ctx 携带其它 Pool 的事务 | 不执行回调、不开启新事务 | `ErrTxOwnerMismatch` |
| `fn` 返回 `nil` | `Commit` | `nil`；提交失败则 `ErrTxCommit` |
| `fn` 返回 `error` | `Rollback` | 回调的原始 error；回滚失败则 `ErrTxRollback`（同时包装回滚错误与原始 error） |
| `fn` panic | `defer` 中兜底 `Rollback` | panic 继续向上传播（zcdb 不 recover） |

补充语义：

- 事务始终在 `pool.master.BeginTx(ctx, nil)` 上开启，使用驱动默认隔离级别，只读/只写选项不可配置。
- `Commit` 失败时，`database/sql` 已经结束并自动回滚该事务；`Transaction` 不会再调用 `Rollback`（避免返回误导性的 `ErrTxDone`），直接返回 `ErrTxCommit`。
- 回滚失败时返回的错误形如 `zcdb: rollback failed: <rbErr>, original error: <fnErr>`，两个底层错误都可用 `errors.Is` 匹配。
- 回调返回的原始 error 在回滚成功时**原样返回**，不会被额外包装。

```go
func example() {
	err := db.Transaction(ctx, func(txCtx context.Context) error {
		if _, err := db.Builder().Table("accounts").Where("id", "=", 1).
			Decrement(txCtx, "balance", 100); err != nil {
			return err
		}
		if _, err := db.Builder().Table("accounts").Where("id", "=", 2).
			Increment(txCtx, "balance", 100); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return
	}
}
```

事务内依次执行（MySQL 形态）：

```sql
UPDATE `accounts` SET `balance` = `balance` - ? WHERE `id` = ?   -- args: [100 1]
UPDATE `accounts` SET `balance` = `balance` + ? WHERE `id` = ?   -- args: [100 2]
```

两条语句属于同一事务：回调返回 `nil` 时两边一起生效；任一语句失败、或回调返回 `error`，两边余额都回到事务前。这也意味着事务内不要先做不可回滚的外部副作用（发消息、扣库存服务等），除非你有补偿逻辑。

判断失败阶段时用 `errors.Is`：

```go
func example() {
	err := db.Transaction(ctx, func(txCtx context.Context) error {
		return nil
	})
	switch {
	case errors.Is(err, zcdb.ErrTxBegin):
		// 连接池已关闭、网络不可达等，回调未执行
	case errors.Is(err, zcdb.ErrTxCommit):
		// 提交失败；database/sql 已自动回滚
	case errors.Is(err, zcdb.ErrTxRollback):
		// 回调返回 error 且回滚也失败；原始 error 仍可 errors.Is
	case errors.Is(err, zcdb.ErrTxOwnerMismatch):
		// ctx 里的事务属于另一个 Pool
	}
}
```

## ctx 传播：事务句柄只存在于回调的 ctx 里

`Transaction` 开启事务后，把 `{tx, pool}` 放进一个派生 ctx 传给回调。之后只有携带这个值的 ctx 才会命中事务连接。因此：

- 回调的每个数据库调用都必须传 `txCtx`：`db.Builder()...Find(txCtx, ...)`、`db.Exec(txCtx, ...)`、`db.Query(txCtx, ...)` 都一样。
- 把 `txCtx` 传给同一 Pool 的任何 DAO、任何业务函数都有效；不要中途换成 `context.Background()` 或原始 `ctx`。
- `txCtx` 继承外层 ctx 的取消、截止时间与值；外层 ctx 被取消时，`database/sql` 会回滚事务，后续语句失败。

反例：

```go
func example() {
	_ = db.Transaction(ctx, func(txCtx context.Context) error {
		// 错误：用了外层 ctx，这条语句在事务之外执行
		_, err := db.Builder().Table("accounts").Where("id", "=", 1).
			Decrement(ctx, "balance", 100)
		return err
	})
}
```

正确写法是把所有 `ctx` 换成回调参数 `txCtx`。在 `MaxOpenConns=1` 的池上，这个错误还可能表现为「事务持有着唯一连接，外层 ctx 的语句一直等连接」——通常以超时或长时间等待收场，而不是立刻报错。

```go
func example() {
	err := db.Transaction(ctx, func(txCtx context.Context) error {
		if _, err := db.Builder().Table("accounts").Where("id", "=", 1).
			Decrement(txCtx, "balance", 100); err != nil {
			return err
		}
		// 同一个 txCtx 可以继续传给同一 Pool 的其他 DAO/业务函数
		return db.Transaction(txCtx, func(innerCtx context.Context) error {
			_, err := db.Builder().Table("accounts").Where("id", "=", 2).
				Increment(innerCtx, "balance", 100)
			return err
		})
	})
	if err != nil {
		return
	}
}
```

## 同 Pool 嵌套：复用当前事务，不是保存点

`Transaction` 在开启新事务前先检查 ctx：

- 没有事务 → `BeginTx`，正常开启；
- 已有**同一 Pool** 的事务 → 不开启新事务，直接执行回调；
- 已有**其它 Pool** 的事务 → 返回 `ErrTxOwnerMismatch`，不执行回调。

因此同 Pool 的嵌套是「扁平」的：内层没有独立的提交点，也没有保存点。内层返回 nil 只代表它自己没报错，真正的提交仍由最外层决定；内层返回 error 时，必须由外层决定是否继续向上返回——外层如果吞掉错误并返回 nil，整个事务（包括内层已执行的语句）会一起提交。

```go
func example() {
	create := func(ctx context.Context, id int64) error {
		return db.Transaction(ctx, func(ctx context.Context) error {
			_, err := db.Builder().Table("orders").Insert(ctx, Order{Id: id})
			return err
		})
	}

	err := db.Transaction(ctx, func(txCtx context.Context) error {
		if err := create(txCtx, 1); err != nil {
			return err // 内层错误 → 外层回滚整个事务
		}
		if err := create(txCtx, 2); err != nil {
			return err
		}
		return nil // 最外层返回 nil 才真正 Commit
	})
	if err != nil {
		return
	}
}
```

同一 `Pool` 上创建的不同 `DBDao` 也能复用：事务归属比较的是 `*Pool` 指针，不是 DAO 实例。把同一个 `txCtx` 交给 `dbA`、`dbB`（两者由同一个 pool 创建）都命中同一事务。

**不要这样**：外层忽略内层错误并返回 nil。

```go
func example() {
	_ = db.Transaction(ctx, func(txCtx context.Context) error {
		_ = db.Transaction(txCtx, func(context.Context) error {
			return errors.New("内层失败")
		})
		return nil // 整个事务提交，内层失败被吞掉
	})
}
```

## 跨 Pool：ErrTxOwnerMismatch

事务 context 里同时记录了事务对象与它所属的 `Pool`。DAO 在执行前用 `txFor` 校验：ctx 里有事务、但 `pool` 不是自己持有的那个，直接返回 `ErrTxOwnerMismatch`。

拒绝发生在执行之前，以下入口全部生效：

| 入口 | 行为 |
|---|---|
| `db.Exec` / `db.Query` / `db.QueryPrimary` | 返回 `ErrTxOwnerMismatch`，SQL 不执行，慢 SQL 回调不触发 |
| Builder 的终端方法（`Find`/`Insert`/`Update`/`Paginate`/`Cursor` 等） | 编译出 SQL 后执行前被拒绝，返回 `ErrTxOwnerMismatch` |
| `db.Transaction` | 不执行回调，不开启第二个独立事务 |

```go
func example() {
	err := dbA.Transaction(ctx, func(txCtx context.Context) error {
		err := dbB.Transaction(txCtx, func(context.Context) error {
			// 不会执行
			return nil
		})
		if errors.Is(err, zcdb.ErrTxOwnerMismatch) {
			// 正确做法：先结束 dbA 的事务，再在 dbB 上另开一个事务；
			// zcdb 不提供跨库原子事务，也不存在「把两个库纳入同一事务」的开关。
		}
		return err
	})
	if err != nil {
		return
	}
}
```

`ErrTxOwnerMismatch` 只表示「ctx 里的事务不属于当前 Pool」，不代表连接坏了。同一个 `txCtx` 继续给所属 Pool 的 DAO 使用仍然有效。

## 事务内读、Primary 与锁

路由判定顺序是：**先看 ctx 事务，再看锁/Primary 标记**。因此事务内所有 Builder 执行（包括读）都走事务连接；`Primary()` 与锁标记不会把语句切到另一条连接。

| 入口 | 无事务 | 本 Pool 事务 | 其它 Pool 事务 |
|---|---|---|---|
| Builder 普通读 | 从库（无从库降级主库） | 事务连接 | `ErrTxOwnerMismatch` |
| Builder + `Primary()` | 主库 | 事务连接（`Primary` 无额外效果） | `ErrTxOwnerMismatch` |
| Builder + `LockForUpdate`/`SharedLock` | 主库 | 事务连接，锁子句编译进 SQL | `ErrTxOwnerMismatch` |
| `Exec`（原始写） | 主库 | 事务连接 | `ErrTxOwnerMismatch` |
| `Query` / `QueryPrimary`（原始读） | 从库 / 主库 | 事务连接（两者相同） | `ErrTxOwnerMismatch` |

```go
func example() {
	err := db.Transaction(ctx, func(txCtx context.Context) error {
		// 锁查询：锁子句编译进 SQL，并走事务连接（事务由主库开启）
		var account Account
		if err := db.Builder().Table("accounts").Where("id", "=", 1).
			LockForUpdate().First(txCtx, &account); err != nil {
			return err
		}

		// 普通读：同样走事务连接，因此能看到本事务尚未提交的写入
		var latest Order
		if err := db.Builder().Table("orders").Where("id", "=", orderID).
			Primary().First(txCtx, &latest); err != nil {
			return err
		}

		_, err := db.Builder().Table("accounts").Where("id", "=", 1).
			Update(txCtx, struct {
				Balance int `db:"balance"`
			}{Balance: account.Balance - 100})
		return err
	})
	if err != nil {
		return
	}
}
```

SQL（MySQL 形态）：

```sql
SELECT * FROM `accounts` WHERE `id` = ? LIMIT 1 FOR UPDATE   -- args: [1]
SELECT * FROM `orders` WHERE `id` = ? LIMIT 1                 -- args: [orderID]，Primary 不改 SQL
UPDATE `accounts` SET `balance` = ? WHERE `id` = ?            -- args: [新余额 1]
```

锁的注意事项：

- `LockForUpdate` 编译为 `FOR UPDATE`；`SharedLock` 在 MySQL 为 `LOCK IN SHARE MODE`、PostgreSQL 为 `FOR SHARE`。
- SQLite 不支持锁子句，编译即报 `ErrSQLiteLockNotSupported`；SQLite 的并发控制靠数据库/事务模式本身。
- 无事务时锁查询虽然强制走主库，但 autocommit 下语句结束锁就释放，**起不到互斥作用**；要互斥必须放进 `Transaction`。
- 事务内的 `Primary()` 不改变 SQL、不改变连接（已经在事务连接上），保留它不会出错，主要用于同一段业务代码既可能在事务内、也可能在事务外执行的场景。

## panic 语义

`Transaction` 在回调执行前注册了 `defer`：只要事务尚未提交/回滚完成，defer 就调用 `Rollback`。因此回调 panic 时：

1. defer 执行 `Rollback`（回滚失败的错误被忽略）；
2. panic 继续向上传播，由调用方决定是否 `recover`。

```go
func example() {
	defer func() {
		if r := recover(); r != nil {
			// 到这里时事务已回滚；可以记录日志或转换为错误返回
		}
	}()

	_ = db.Transaction(ctx, func(txCtx context.Context) error {
		if _, err := db.Builder().Table("accounts").Where("id", "=", 1).
			Increment(txCtx, "balance", 999); err != nil {
			return err
		}
		panic("boom") // 先回滚，再把 panic 抛给调用方
	})
}
```

**不要**在回调内部把 panic recover 掉然后返回 nil：`Transaction` 看不到 panic，会按「回调成功」提交。

```go
func example() {
	_ = db.Transaction(ctx, func(txCtx context.Context) error {
		defer func() {
			_ = recover() // 不要这样：panic 被吞掉后函数返回 nil，事务会提交
		}()
		panic("boom")
	})
}
```

## 常见错误

| 错误 | 现象 | 正确做法 |
|---|---|---|
| 回调里用外层 `ctx` | 语句在事务外执行，回滚带不走；单连接池还可能一直等连接 | 所有调用统一传 `txCtx` |
| 忽略语句错误并 `return nil` | 部分写入被提交 | 每个错误都 `return err` |
| 期待内层 `Transaction` 是保存点 | 内层「提交」不会落库，内层失败也不会自动局部回滚 | 把内层错误返回给外层；需要局部回滚只能拆事务或手写保存点 SQL |
| 外层吞掉内层错误 | 整体提交，包含内层的失败语句 | 透传错误，或显式决定并记录 |
| 跨 Pool 传 `txCtx` | `ErrTxOwnerMismatch`，SQL/回调都不执行 | 结束当前事务后，在目标 Pool 另开事务 |
| 事务里做 HTTP/文件/长时间等待 | 连接与锁被长期占用，吞吐下降、易超时 | 把外部 IO 移出事务，或在事务外先准备好数据 |
| 事务里用 `Cursor` 长时间遍历 | 事务连接被占住，走池的其他操作可能等待（`MaxOpenConns` 较小时尤其明显） | 先在事务内取完数据再处理；批量遍历放到事务外，用 `CursorBy` |
| 在 SQLite 上用锁 | `ErrSQLiteLockNotSupported` | 去掉锁方法，依赖 SQLite 自身事务语义 |
| 回调内 recover 后返回 nil | 本应失败的事务被提交 | 让 panic 继续传播，或在 recover 后返回 error |
| 忽略 `Transaction` 的返回值 | 提交/回滚失败被静默 | 检查返回值，用 `errors.Is` 区分 `ErrTxCommit` 等 |
| 事务未结束就 `db.Close()`/`pool.Close()` | 事务被中断，后续语句报连接已关闭 | 先结束事务，再关闭池 |

## 延伸阅读

- 连接路由与事务实现：[concepts/execution-routing.md](../concepts/execution-routing.md)
- Builder 在事务中的克隆/状态行为：[concepts/builder-state.md](../concepts/builder-state.md)
- 锁与分页的组合：[how-to/pagination.md](pagination.md)
- `Transaction` 与连接池的 API 细节：[reference/builder-exec.md](../reference/builder-exec.md)、[reference/errors.md](../reference/errors.md)
- 从零跑通：[getting-started.md](../getting-started.md)
- 文档总地图：[README.md](../README.md)
