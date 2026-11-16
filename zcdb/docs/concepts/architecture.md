# 架构与一次查询的生命周期

这份文档回答一个问题：从 `db.Builder()` 到结构体里出现数据，中间到底发生了什么。理解它之后，大多数「SQL 为什么长这样」「为什么这里必须 Clone」「为什么事务里的读走了主库」都可以从机制推出，而不必记结论。

## 分层与依赖方向

```
Builder ──状态──► Grammar ──SQL──► DBDao ──连接──► Pool ──► database/sql
   ▲                                  │
   └────────── 终端方法（Find/Update/Cursor…）──────────┘
```

- **Builder** 只依赖 `Grammar` 接口与 `*DBDao`（执行终端方法时用）。它不知道 MySQL 还是 PostgreSQL。
- **Grammar** 是纯编译层：输入 Builder 的字段，输出 SQL 字符串。它不执行、不重试、不感知事务。
- **DBDao** 持有 `Pool` 与一个 `Grammar` 实例；它是唯一决定「走哪条连接」的地方。
- **Pool** 只管理 `*sql.DB` 列表：主库、从库、策略、生命周期。

依赖单向：`Builder → Grammar`、`DBDao → {Pool, Grammar}`，没有反向依赖，也没有全局单例。

## 一次读查询的完整路径

以 `db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)` 为例：

1. **`db.Builder()`**：`DBDao.Builder()` 调用 `NewBuilder(d.grammar, d)`，创建空 Builder 并复制 DAO 的默认注释。Grammar 与 DAO 是共享实例（Grammar 无可变状态，见 [compile-pipeline](compile-pipeline.md)）。
2. **链式构造**：`Table` 写 `table` 字段，`Where` 追加 `WhereClause`，方法都返回同一个 `*Builder`。这一步不生成任何 SQL。
3. **终端方法进入编译**：`First` 检查 `limit == 1`；不是则 `Clone()` 出一份副本并把副本的 `limit` 设为 1，避免污染调用方的 Builder。随后副本执行 `ToSelect()`。
4. **编译**：`ToSelect` 依次做：检查累积错误 `b.err` → 检查数据源（`table`/`tableSub`）→ 环检测 → 锁/UNION 组合校验 → 调 `grammar.CompileSelect` → 收集绑定参数 → `appendSQLComment` 追加 DAO 注释。返回 `(sqlStr, args, nil)`。
5. **路由**：`First` 调 `b.query(ctx, sqlStr, args...)`。`query` 判断：`lockClause != ""` 或 `usePrimary` → 走 `QueryPrimary`；否则走 `Query`。`DBDao.Query` 再检查 `ctx` 里是否有本 Pool 的事务：有则用 `*sql.Tx`，否则 `pool.PickReadDB()` 选从库（无则主库）。
6. **执行**：`database/sql` 执行 SQL；DAO 的慢 SQL 回调在语句返回后触发，回调 panic 被 `recover` 隔离。
7. **扫描**：`rows` 交给 `ScanStructClose`：读取列名 → 用 `(结构体类型, 标签名)` 查字段映射缓存 → 逐行把列值写入字段（未匹配列丢弃、NULL 置零值、必要时做类型转换）→ 关闭 rows。

## 一次写查询的差异

写路径与读路径的差别集中在三处：

- **编译入口**：`Insert`/`Update`/`Delete` 等调 `extractInsertData`/`extractUpdateData` 把结构体转成列与值，再进 `ToInsert`/`ToUpdate`；写语句的绑定顺序与 SELECT 不同，方言差异见 [compile-pipeline](compile-pipeline.md)。
- **路由**：`DBDao.Exec` 不经过 `PickReadDB`，有事务走事务连接，否则固定 `PickWriteDB()`（主库）。
- **安全守卫**：`Update`/`Increment`/`Decrement`/`Delete`/`DeleteJoin` 在执行前检查「是否有有效限定条件」，无则返回哨兵错误；判定规则与边界见 [safety](safety.md)。

## 为什么这样分层

- **方言隔离**：Builder 层只写「要什么」，包裹符、占位符、UNION 括号、TRUNCATE 形态都由 Grammar 决定。新增方言 = 实现一个 Grammar，不改 Builder。
- **编译可测试**：`ToXxx` 是纯函数式的「状态 → SQL」，不需要数据库就能断言 SQL 与 args，这是仓库大量单元测试的基础。
- **执行可替换**：拿到 `(sql, args)` 后可以交给 `db.Exec`、日志、审计或别的组件执行；`CompileRaw` 还能把参数内联成无占位符的完整 SQL。
- **连接策略集中**：事务所有权、读写分离、锁强制主库只在 DBDao/Pool 一处判定，Builder 不掺和。

## 失败在哪些层产生

| 层 | 典型错误 | 表现 |
|---|---|---|
| Builder 构造 | `ErrInvalidOperator`、`ErrInvalidSubQuery` | 记录到 `b.err`，编译时统一返回 |
| 编译 | `ErrEmptyTable`、`ErrCyclicQuery`、`ErrSQLiteLockNotSupported`、`ErrWriteOrderLimitUnsupported` | 立即返回哨兵 |
| 结构体映射 | `ErrInvalidStruct`、`ErrNoFields`、`ErrScanConvert` | 在提取/扫描阶段返回 |
| 条件保护 | `ErrUpdateWithoutWhere`、`ErrDeleteWithoutWhere`、`ErrDeleteWithJoin` | 执行前拒绝 |
| 方言语义 | `ErrPgUnionLockNotSupported`、`ErrInsertGetIdUnsupported`、`ErrUpsertUniqueByRequired` | 编译前拒绝 |
| 连接与事务 | `ErrPoolConnect`、`ErrPoolClosed`、`ErrTxOwnerMismatch` 等 | 由 DBDao/Pool 返回 |

哨兵全表与触发条件见 [errors](../reference/errors.md)。

## 相关阅读

- 状态如何累积、哪些方法会改自身：[builder-state](builder-state.md)
- SQL 与绑定顺序：[compile-pipeline](compile-pipeline.md)
- 结构体 ↔ 列：[mapping](mapping.md)
- 连接路由与事务：[execution-routing](execution-routing.md)
- 安全边界：[safety](safety.md)
