# 执行层：连接路由、事务与回调

`DBDao` 是执行层：它拿着编译好的 SQL，决定用哪条连接执行。这份文档解释连接池、读写分离、事务传播与慢 SQL 回调的实现方式。

## 创建顺序

```
PoolConfig{DriverName, DSN, SlaveDSNs, 池参数, ConnectTimeout, SlaveStrategy}
        │ NewPool：sql.Open → SetMaxOpenConns/SetMaxIdleConns/SetConnMaxLifetime → Ping（带超时）
        ▼
      Pool（master *sql.DB + slaves []*sql.DB + SlaveStrategy）
        │ NewDBDao(pool, dialect, onSQL, tagName, comment)
        ▼
     DBDao（pool + grammar + tagName + comment + onSQL）
```

- `NewPool` 打开主库并 Ping；每个从库 DSN 也各自 `sql.Open` + Ping，任一失败则关闭已建连接并返回 `ErrPoolConnect`（底层错误用 `%w` 包装）。
- 默认值：`MaxOpenConns=50`、`MaxIdleConns=50`、`ConnMaxLifetimeSecond=600`、`ConnectTimeout=5s`、`SlaveStrategy=RandomStrategy`；`<=0` 取默认值。
- `dialect` 决定 Grammar：`mysql`；`postgresql`/`postgres`/`pgsql`；`sqlite`/`sqlite3`。空值 `ErrDialectRequired`，未知 `ErrUnknownDialect`，pool 为 nil `ErrPoolRequired`。
- `tagName` 空串使用 `db`；`comment` 在 DAO 创建时规范化并保存，之后不可变。

## 路由规则

`DBDao` 的每个执行入口都遵循同一套判定：

| 条件 | 目标连接 |
|---|---|
| `ctx` 携带本 Pool 的事务 | 事务连接（`*sql.Tx`） |
| `ctx` 携带其它 Pool 的事务 | 返回 `ErrTxOwnerMismatch`，不执行 |
| `Exec`（写） | 主库 |
| `Query`（读） | 从库；无配置从库或策略返回 nil 时降级主库 |
| `QueryPrimary` / Builder 带 `Primary()` / Builder 带锁 | 主库 |
| Schema 查询 | 读（`Query`） |

Builder 的 `query` 内部入口只判断「锁子句非空或 `usePrimary`」；事务检测在 `DBDao` 层。因此 `Primary()` 只影响路由、不改变 SQL。

`PickReadDB` 的从库选择由 `SlaveStrategy` 决定：内置 `RandomStrategy`（默认）与 `RoundRobinStrategy`；`Pick` 返回 nil 时降级主库。`AddSlave` 可在运行期热加载，失败不影响已建连接。

## 事务

```go
func example() {
	err := db.Transaction(ctx, func(ctx context.Context) error {
		if _, err := db.Builder().Table("users").Where("id", "=", 1).
			Update(ctx, User{Name: "alice"}); err != nil {
			return err // 返回 error → 回滚
		}
		return nil // 返回 nil → 提交
	})
}
```

实现要点：

- `Transaction` 用 `pool.master.BeginTx` 开启事务，把 `{tx, pool}` 放进 `ctx`。
- 回调返回 nil → `Commit`；返回 error → `Rollback`，回滚失败时返回 `ErrTxRollback`（同时包装回滚错误与回调原始错误，两者都可用 `errors.Is` 匹配）。
- 回调 panic → `defer` 兜底回滚后继续向上 panic。
- **事务归属绑定 Pool**：同 Pool 的不同 DAO 可以复用该事务；把 ctx 传给另一 Pool 的 DAO 会返回 `ErrTxOwnerMismatch`，不会误用原事务，也不会另开独立事务。
- 嵌套调用 `Transaction` 时，若 ctx 已有同 Pool 事务则直接执行回调，不开启新事务。
- 事务内所有 Builder 执行（读和写）都走事务连接，因此能看到未提交的写入。

## 慢 SQL 回调

`NewDBDao` 的第三个参数 `SlowSQLCallback` 签名为 `(ctx, grammar, elapsed, sqlStr, args)`：

- 只在回调非 nil 时计时（`time.Now` 不开销在热路径）；
- 在语句执行后调用，拿到的是**实际执行的 SQL**（Builder 产物已包含最终注释）与原始绑定参数；
- 回调 panic 被 `recover` 隔离，不影响主流程；
- 一次方法调用可能触发多次回调：`Paginate`（COUNT + 数据查询）、`CursorBy`（每批）、SQLite `Truncate`（主语句 + 辅助语句）各算各的；
- 阈值判断由回调内部决定，zcdb 不内置「slow」概念。

## rows 的生命周期

| 入口 | 谁关闭 rows |
|---|---|
| `Find`/`First`/`Value`/`Count`/`Exists`/聚合/`Pluck` 系列/`Paginate` | 方法内部关闭 |
| `Cursor` | 迭代结束或 `break` 时关闭；迭代期持有连接 |
| `CursorBy` | 每批结束关闭；不长时间占用连接 |
| `db.Query` 返回的 rows | 调用方负责 `Close` |
| `ScanStruct` | 不关闭；`ScanStructClose` 关闭 |

`Cursor` 在 `yield` 返回 false（`break`）时会 return，`defer rows.Close()` 生效；`CursorBy` 每批单独 `Close`，错误路径也会关闭。

## Pool 生命周期

- `Pool.Close()` 幂等：首次关闭主库与全部从库，重复调用返回 nil；
- 关闭后 `AddSlave` 返回 `ErrPoolClosed`；`PickReadDB`/`PickWriteDB` 仍返回已关闭的连接（签名所限不做快速失败），后续查询报 `sql: database is closed`；
- `Ping(ctx)` 依次 Ping 主库与全部从库，任一失败返回该错误；
- `db.Close()` 委托给池；推荐在进程退出流程（如 zcquit 的高级别清理 handler）调用一次。

## 相关阅读

- 事务任务指南：[how-to/transactions](../how-to/transactions.md)
- 连接池参数与错误：[reference/errors](../reference/errors.md)
- 路由与锁的 SQL 形态：[reference/builder-order-group](../reference/builder-order-group.md)
