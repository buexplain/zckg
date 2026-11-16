# 错误参考

zcdb 的可导出错误都是 `errors.New` 生成的**哨兵值**（`errors.go` 共 45 个，可用 AST 枚举核对），没有自定义错误类型，因此判定方式只有两种：哨兵用 `errors.Is`，未命中哨兵的错误按驱动/数据库错误处理。

这份文档按产生位置分组列出全部哨兵：触发条件、返回入口，以及该错误是裸返回还是被 `%w` 包装。文末说明「驱动错误原样透传」与「非哨兵错误」的边界。

## 怎么读这份参考

- **`errors.Is` 语义**：全部 45 个哨兵都可用 `errors.Is(err, zcdb.ErrXxx)` 精确匹配——裸返回时 `err == zcdb.ErrXxx` 也成立，但推荐统一用 `errors.Is`，这样将来加上下文包装不会破坏调用方。
- **包装形态**列写作「裸返回」表示错误原样返回、消息就是哨兵文本；写作 `%w` 表示外层带上下文前缀，哨兵在错误链中。被 `%w` 包装的哨兵**同时**保留底层错误（驱动错误、回调错误、`rbErr` 等）的可匹配性。
- **顺序**：同一个入口可能先校验参数、再校验保护、最后编译。表中「返回入口」列出真正产生错误的那个环节；执行类方法通常先做 Builder 错误/环检测，再走安全保护，最后才进 `ToXxx` 编译（因此「无 WHERE」会先于「写语句不支持 ORDER BY/LIMIT」返回）。
- **不提供 `errors.As` 目标**：zcdb 没有自定义错误结构体，`errors.As` 只可能用于底层驱动错误类型。

## Pool

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrDriverNameRequired` | `PoolConfig.DriverName` 为空 | `zcdb.NewPool` | 裸返回（`zcdb: DriverName is required`） |
| `ErrDSNRequired` | `PoolConfig.DSN` 为空（主库 DSN 必填） | `zcdb.NewPool` | 裸返回（`zcdb: DSN is required`） |
| `ErrPoolConnect` | 主库 `sql.Open` 或 Ping 失败；从库 `sql.Open` 或 Ping 失败 | `zcdb.NewPool`（主库与从库初始化）、`Pool.AddSlave` | `%w` 包装：`zcdb: pool connect failed: open master: <driver>` / `ping master` / `open slave` / `ping slave`；`NewPool` 加从库失败时再套一层 `zcdb: open slave failed: %w`。底层驱动错误仍在链上 |
| `ErrPoolClosed` | 连接池已 `Close` 后调用 `AddSlave`（含 Ping 期间被并发 `Close` 的锁内复检） | `Pool.AddSlave` | 裸返回（`zcdb: pool is closed`）。`Pool.Close` 幂等，重复调用返回 `nil` |

`Pool.Ping` 与 `Pool.Close` 返回的是驱动错误（或第一个 `Close` 错误），不包装哨兵。

## DAO 与事务

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrDialectRequired` | `dialect` 为空串 | `zcdb.NewDBDao` | 裸返回（`zcdb: dialect is required`）。该检查先于 `pool` 校验 |
| `ErrUnknownDialect` | `dialect` 不在别名表内（`mysql`、`postgresql`/`postgres`/`pgsql`、`sqlite`/`sqlite3`） | `zcdb.NewDBDao` | `%w` 包装：`zcdb: unknown dialect: oracle` |
| `ErrPoolRequired` | `pool` 为 `nil`（dialect 已合法） | `zcdb.NewDBDao` | 裸返回（`zcdb: pool is required`） |
| `ErrTxOwnerMismatch` | `ctx` 携带的事务属于另一个 `Pool` | `DBDao.Exec`、`DBDao.Query`、`DBDao.QueryPrimary`、`DBDao.Transaction`（以及经它们执行的 Builder 终端方法） | 裸返回。SQL 不执行、慢 SQL 回调不触发；不会在另一个池上另开事务 |
| `ErrTxBegin` | `BeginTx` 失败 | `DBDao.Transaction` | `%w` 包装：`zcdb: begin transaction failed: <err>` |
| `ErrTxCommit` | `Commit` 失败（事务已结束，`database/sql` 负责自动回滚） | `DBDao.Transaction` | `%w` 包装：`zcdb: commit failed: <err>` |
| `ErrTxRollback` | 回调返回 error 且随后的 `Rollback` 也失败 | `DBDao.Transaction` | `%w` 包装两个错误：`zcdb: rollback failed: <rbErr>, original error: <回调错误>`，三者都可用 `errors.Is` 匹配。回滚成功时直接返回回调的原始错误 |

## Builder 编译

这些错误在 `ToXxx` 编译入口（或构造期写入 `b.err`、由编译入口统一返回）产生。调用它们的终端方法会原样上报。

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrEmptyTable` | 未设置 `Table`/`TableSub` 就编译 | `ToSelect`、`ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToInsertUsing`、`ToInsertOrIgnoreUsing`、`ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToTruncate`、`ToIncrement`、`ToDecrement`、`ToCount`、`ToExists`、`ToAggregate` 及其终端方法 | 裸返回。例外：`ToCount`/`ToExists`/`ToAggregate` 允许「只有 UNION、没有主表」 |
| `ErrInvalidStruct` | 数据不是结构体/结构体指针/它们的切片（含 `nil`、`nil` 指针、切片元素非结构体）；`InsertGetId` 传入非空切片 | `ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToUpdate`、`InsertGetId` | 裸返回；`ScanStruct` 的「切片元素非结构体」分支为 `%w` 包装（`...: slice element must be struct or *struct, got ...`） |
| `ErrNoFields` | 结构体没有可用导出字段，或首行的字段全部是 nil 的 `any`/指针/不可用的嵌入字段 | `ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToUpdate`、`KeyBy`（目标结构体无可用字段） | 裸返回 |
| `ErrEmptyData` | 传入空切片 | `ToInsert`、`ToInsertOrIgnore`、`ToUpsert`（`Insert` 族经它们） | 裸返回（`zcdb: data is empty`） |
| `ErrInvalidOperator` | 运算符不在白名单（`=`、`!=`、`<>`、`<`、`>`、`<=`、`>=`、`LIKE`、`NOT LIKE`、`IS`、`IS NOT`），或三参形式的值位置不是字符串运算符 | `Where`/`OrWhere`、`WhereColumn`、`WhereSub`/`OrWhereSub`、`Join`/`LeftJoin`/`RightJoin`/`CrossJoinOn`、`JoinBuilder.On`/`OrOn`/`Where`/`OrWhere`/`CrossJoinOn`、`Having`/`OrHaving` | 裸返回；构造期写入 `b.err`，在编译入口返回 |
| `ErrInvalidSubQuery` | `WhereExists`/`OrWhereExists`/`WhereNotExists`/`OrWhereNotExists` 的 `sub` 既不是 `*Builder` 也不是 `func(*Builder)`；`JoinBuilder.WhereExists` 在 Grammar 缺失时也返回它 | 上述 `Where*Exists` 方法、`JoinBuilder.WhereExists` | 裸返回；构造期累积，编译入口返回 |
| `ErrInvalidWhereInValues` | `JoinBuilder.WhereIn`/`WhereNotIn` 的 `values` 既不是 `[]any` 也不是 `*Builder` | 仅 `JoinBuilder` 的这两个方法 | 裸返回；构造期累积，编译入口返回 |
| `ErrCyclicQuery` | 子查询图存在环（`TableSub`/`Union`/`JoinSub` 等指向自身或互引） | 所有 `ToXxx` 的 `validateAcyclic`；`Clone` 返回携带该错误的浅副本 | 裸返回 |
| `ErrUpsertUniqueByRequired` | PostgreSQL/SQLite 的 `ToUpsert` 未提供 `uniqueBy` | `ToUpsert`（`Upsert` 经它） | 裸返回 |
| `ErrInsertUsingColumnMismatch` | 子查询 SELECT 列数可静态判定，且与目标列数不一致（含 `SelectSub`；`*` 或未显式指定列时交给数据库） | `ToInsertUsing`、`ToInsertOrIgnoreUsing` | `%w` 包装：`...: insert columns=2, subquery select columns=3` |
| `ErrInsertGetIdUnsupported` | PostgreSQL 方言调用 `InsertGetId`（数据校验与编译已通过，执行前拒绝） | `InsertGetId` | `%w` 包装：`... (lib/pq does not support LastInsertId); use Insert or raw SQL with RETURNING instead` |
| `ErrPgUnionLockNotSupported` | PostgreSQL 下同时设置锁与 UNION | `ToSelect` 及其终端方法（Find/First/Value/Count/Exists/Paginate/聚合/Pluck 族/Cursor/CursorBy） | 裸返回 |
| `ErrSQLiteLockNotSupported` | SQLite 下设置任意锁（`LockForUpdate`/`SharedLock`） | 同上 | 裸返回。校验在 `ToSelect`，直接调 `Grammar.CompileSelect` 会绕过并静默丢弃锁子句 |
| `ErrWriteOrderLimitUnsupported` | 写语句携带 `ORDER BY` 或 `Limit > 0`，但方言/形态不支持：PostgreSQL、SQLite 的全部写语句；MySQL 的多表 `UPDATE`（带 JOIN）与 `DeleteJoin` | `ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToIncrement`、`ToDecrement` 及其终端方法 | `%w` 包装，消息带出方言/形态：`...: postgres dialect has no ORDER BY/LIMIT on write statements`、`...: sqlite ...`、`...: MySQL multi-table UPDATE/DELETE JOIN does not support ORDER BY/LIMIT` |
| `ErrDeleteWithJoin` | 普通 `ToDelete`/`Delete` 的 Builder 带 JOIN（`Force()` 也不放行） | `ToDelete`、`Delete` | 裸返回 |
| `ErrDeleteJoinNoJoin` | `ToDeleteJoin`/`DeleteJoin` 没有任何 JOIN | `ToDeleteJoin`、`DeleteJoin` | 裸返回。该检查先于「无有效 WHERE」保护 |
| `ErrInvalidAggregate` | `ToAggregate` 的函数不是 `MAX`/`MIN`/`SUM`/`AVG` | `ToAggregate` | 裸返回。`Max`/`Min`/`Sum`/`Avg` 传固定函数名，不会触发 |
| `ErrAggregateGrouped` | 标量聚合（`ToAggregate`/`Max`/`Min`/`Sum`/`Avg`）带 GROUP BY 或 HAVING | `ToAggregate`、`Max`、`Min`、`Sum`、`Avg` | 裸返回；应改用 `Select` + `Find` 接收分组结果 |
| `ErrIncrementColumns` | `Increment`/`Decrement` 的 `extra` 长度为奇数，或其中列名位置不是 `string`；`ToIncrement`/`ToDecrement` 的 `columns` 为空或与 `amounts` 不等长 | `Increment`、`Decrement`（先于保护判定）、`ToIncrement`、`ToDecrement` | 裸返回 |
| `ErrRawPlaceholderMismatch` | `CompileRaw` 的占位符与参数数量/编号不符：`?` 风格数量不等；`$N` 越界、最大编号不等于参数个数、或没有 `$N` 却传了参数 | `Grammar.CompileRaw`（三个内置 Grammar），例如 `db.Grammar().CompileRaw(...)` | `%w` 包装，消息带出两侧实际数量或越界编号 |
| `ErrRawValueNotInlinable` | `CompileRaw` 的某个参数无法内联成 SQL 字面量：类型不支持（如非 nil 指针、结构体、channel）、`NaN`/`Inf` 无字面量、`driver.Valuer` 的 `Value()` 报错 | `Grammar.CompileRaw` | `%w` 包装：`...: arg 2: unsupported type ...` |

## 终端执行

包含执行前的保护判定与结果扫描。写终端方法的检查顺序是：Builder 错误/环检测 → 保护判定 → `ToXxx` 编译 → 执行。

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrUpdateWithoutWhere` | `Update`/`Increment`/`Decrement` 没有有效 WHERE/JOIN，且未 `Force()`；空 `WhereNotIn`、空嵌套组等不算有效限定 | `Update`、`Increment`、`Decrement`（执行前） | 裸返回。优先于写语句 ORDER BY/LIMIT 的编译检查 |
| `ErrDeleteWithoutWhere` | `Delete` 没有有效 WHERE；`DeleteJoin` 既无有效 WHERE 也无带条件的 JOIN，且未 `Force()` | `Delete`、`DeleteJoin`（执行前） | 裸返回。`Delete` 带 JOIN 时先返回 `ErrDeleteWithJoin` |
| `ErrPluckDest` | `Pluck` 的 `dest` 不是非 nil 的切片指针（map 目标应用 `PluckMap`/`KeyBy`） | `Pluck` | 裸返回 |
| `ErrPluckMapDest` | `PluckMap` 的 `dest` 不是非 nil 的 map 指针，或键/值类型是结构体/结构体指针 | `PluckMap` | 裸返回 |
| `ErrKeyByDest` | `KeyBy` 的 `dest` 不是非 nil 的 map 指针，或值类型不是结构体/结构体指针，或键类型是结构体/结构体指针 | `KeyBy` | 裸返回 |
| `ErrScanDest` | `ScanStruct` 的 `dest` 是指针，但指向的类型既不是结构体也不是切片 | `ScanStruct`、`ScanStructClose`，以及经它们扫描的 `Find`/`First`/`Paginate` | `%w` 包装：`..., got *int` |
| `ErrScanConvert` | 列值转成目标字段失败：文本数值解析失败或溢出、JSON 反序列化失败、目标类型不可转换 | `ScanStruct`/`ScanStructClose`、`Find`/`First`/`Paginate`、`Pluck`/`PluckMap`/`KeyBy`、`Cursor`/`CursorBy` | 全部 `%w` 包装并带上下文（`cannot convert ... to int8`、`cannot unmarshal JSON ...`）；JSON 场景还会保留内层 `encoding/json` 错误 |

## 游标

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrNotPointer` | `Cursor`/`CursorBy` 的 `dest` 不是指针；`ScanStruct` 的 `dest` 不是指针 | `Cursor`、`CursorBy`（裸返回）；`ScanStruct`/`ScanStructClose`/`Find`/`First`（`%w` 包装：`..., got %T`） | 游标侧裸返回，扫描侧包装 |
| `ErrNotStruct` | `Cursor`/`CursorBy` 的 `dest` 是指针，但指向的不是结构体 | `Cursor`、`CursorBy` | 裸返回。非法 `dest` 在查询之前产出，不浪费一次数据库往返 |
| `ErrCursorFieldNotFound` | `CursorBy` 在 `dest` 结构体中找不到 `cursorColumn` 对应字段（列映射、字段名、`table.column` 短名三种匹配都失败） | `CursorBy` | 裸返回 |
| `ErrCursorFieldUnavailable` | `CursorBy` 的游标列字段不可用（如 nil 嵌入指针结构体） | `CursorBy` | 裸返回 |
| `ErrCursorColumnNull` | `CursorBy` 扫描出的游标列值为 NULL | `CursorBy` | 裸返回。必须终止，否则 `游标列 > NULL` 恒假会无限重复同一批 |

`CursorBy` 的 `chunkSize == 0` 时直接结束序列，不执行任何查询，也不产出错误。

## Schema

| 变量 | 触发条件 | 返回入口 | 包装形态 |
|---|---|---|---|
| `ErrSchemaGrammarNotSupported` | DAO 的 Grammar 不是 `*MySQLGrammar`/`*PostgresGrammar`/`*SQLiteGrammar`（含 nil） | `DBDao.Schema`、`zcdb.NewSchemaInspector` | `%w` 包装：`...: *zcdb.MySQLGrammar` 形式的实际类型；失败时返回 nil inspector |

Schema 查询本身的失败（权限不足、连接已关闭、SQL 错误）是数据库/驱动错误，原样返回，不转成哨兵。

## 非哨兵错误与驱动透传

只有 `errors.go` 的 45 个哨兵与 `database/sql` 的 `sql.ErrNoRows` 是本文档承诺可判定的错误；其余错误按下表处理：

| 类别 | 来源与表现 | 判定方式 |
|---|---|---|
| `sql.ErrNoRows` | `First`/`Value` 未命中；`Max`/`Min` 空集；`ScanStruct`（`*struct` 模式）未命中 | `errors.Is(err, sql.ErrNoRows)`。它不是 zcdb 的哨兵，也不会被包装成哨兵 |
| 驱动错误（原样透传） | `DBDao.Exec`/`Query`/`QueryPrimary` 直接返回驱动错误；`Insert`/`Update`/`Delete`/`Upsert`/`InsertUsing`/`Truncate` 等写终端把 `Exec` 的错误原样返回；`Pool.Ping`/`Pool.Close` 返回原始错误 | 按驱动类型/错误码判断；不要按文本匹配 |
| 驱动错误（被包装） | `NewPool`/`AddSlave` 的驱动错误在 `ErrPoolConnect` 链上；`Transaction` 的驱动错误在 `ErrTxBegin`/`ErrTxCommit`/`ErrTxRollback` 链上 | `errors.Is` 命中哨兵后，继续 `errors.Is`/`errors.As` 找驱动错误 |
| 扫描包装错误 | `zcdb: scan row failed: %w`、`zcdb: pluck scan row failed: %w`、`zcdb: keyBy scan row failed: %w`、`zcdb: cursor scan failed: %w` | 外层不是哨兵；用 `errors.Is` 找 `ErrScanConvert` 或驱动错误 |
| 方言 SQL 错误 | 数据库拒绝语法/语义（如 MySQL Error 1221、PostgreSQL 42601、SQLite 旧版本不支持 RIGHT JOIN） | 框架不识别、不翻译；按数据库错误处理 |
| 上下文错误 | `ctx` 取消/超时、连接已关闭（`sql: database is closed`） | 原样透传，`errors.Is(err, context.Canceled)` 等按标准库判断 |

### 边界结论

- **哨兵只描述 zcdb 自己的判断**：参数非法、方言不支持、保护拒绝、扫描转换失败。数据库为什么拒绝一条它不认识的 SQL，不属于哨兵范围。
- **包装不改变语义**：`%w` 只是附加上下文；只要文档写了「包装」，调用方就应假定存在前缀，用 `errors.Is` 而不是字符串比较。
- **不要依赖错误文本**：同一条哨兵在不同入口的消息前缀不同（尤其 `ErrWriteOrderLimitUnsupported` 会带出方言/形态），文本仅供日志阅读。

## 相关阅读

- 各入口的判定顺序与语义：[builder-compile](builder-compile.md)、[builder-exec](builder-exec.md)、[builder-query](builder-query.md)、[builder-cursor](builder-cursor.md)
- 保护判定与写语句限制：[safety](../concepts/safety.md)
- 方言相关的拒绝条件：[dialect-matrix](dialect-matrix.md)
- 连接与事务：[execution-routing](../concepts/execution-routing.md)
- 行扫描的转换规则：[scanner](scanner.md)
