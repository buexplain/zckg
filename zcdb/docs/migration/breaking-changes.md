# 破坏性变更与迁移

本文汇总 zcdb 当前代码相对历史版本的破坏性变更，按「旧写法 → 新写法」给出对照、影响与按版本升级的检查清单。

## 结论先行

- `NewDBDao` 已从四参数变为五参数（新增 `comment`），旧调用在编译期直接失败。
- `SlowSQLCallback` 签名的第二个参数新增 `grammar`，旧回调实现需要补参数。
- `Pluck` 已拆分为 `Pluck`（切片）/ `PluckMap`（标量 map）/ `KeyBy`（结构体 map）；旧 `Pluck(map)` 的 **map 键值列顺序与现在相反**。
- `ErrPluckColumns` 已移除，替换为 `ErrPluckDest` / `ErrPluckMapDest` / `ErrKeyByDest`。
- 写语句携带 `ORDER BY` / `LIMIT` 时，方言或语句形态不支持会在编译期返回 `ErrWriteOrderLimitUnsupported`，不再静默忽略。
- `NewDBDao` 的第五参数启用后，Builder 编译出的 SQL 末尾会自动追加默认注释。

## 版本矩阵

各变更对应的版本：

| 变更 | 旧版本 | 引入版本 |
|---|---|---|
| `NewDBDao` 第五参数 `comment` | v1.1.1 及更早（四参数） | v1.2.0 |
| 默认 SQL 注释 | 无 | v1.2.0 |
| `SlowSQLCallback` 增加 `grammar` 参数 | v1.5.1 及更早 | v1.6.0 |
| 写语句 `ORDER BY` / `LIMIT` 编译期拒绝 | v1.6.1 及更早 | v1.7.0 |
| `Pluck` 拆分与错误哨兵调整 | v1.7.0 及更早 | 当前代码（v1.7.0 之后，尚未打 tag） |

> 版本边界以仓库 tag 为准；「当前代码」指本文对应的开发分支状态，实际发版后请把最后一行替换为对应版本号。示例统一包在 `func example()` 中，仅用于语法检查（`db`、`pool`、`ctx` 等是外部变量）。

## NewDBDao：四参数 → 五参数

`NewDBDao(pool, dialect, onSQL, tagName, comment)` 在末尾新增 `comment string`：它只作为 Builder 的默认短业务标识，不参与连接、路由或方言推导。

```go
func example() {
	// 旧（v1.1.1 及更早）：四参数
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	_, _ = db, err
}
```

```go
func example() {
	// 新（v1.2.0+）：末尾补第五参数
	// 传空串 = 保持原有的无注释基线
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "", "")
	_, _ = db, err

	// 需要默认短业务标识时传固定值（不要传外部输入）
	db, err = zcdb.NewDBDao(pool, "mysql", nil, "", "app:user-service")
	_, _ = db, err
}
```

影响：

- 所有旧调用都因参数个数不足而无法编译，属于「必然被发现」的破坏性变更；迁移时统一在末尾补 `""` 即可保留原有 SQL 文本。
- 通过函数值间接调用的代码（如 `var newDAO = zcdb.NewDBDao`）不需要改类型，但实际调用点仍要补参数。
- 第五参数在 DAO 创建时规范化并固定，之后不可变更；`db.Builder()` 创建的每个 Builder 都复制该值。
- 该参数只影响 Builder 最终编译出的 SQL；DAO 的 `Exec` / `Query` / `QueryPrimary`、Schema 查询与直接调用 `Grammar` 都不会自动追加。

## SlowSQLCallback：增加 grammar 参数

回调签名从 `(ctx, elapsed, sqlStr, args)` 变为 `(ctx, grammar, elapsed, sqlStr, args)`，`grammar` 就是 DAO 持有的编译器（与 `db.Grammar()` 返回同一实例）。

```go
func example() {
	// 旧（v1.5.1 及更早）
	var onSQL func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any)
	db, err := zcdb.NewDBDao(pool, "mysql", onSQL, "", "")
	_, _, _ = db, err, onSQL
}
```

```go
func example() {
	// 新（v1.6.0+）：在 ctx 之后补 grammar
	var onSQL zcdb.SlowSQLCallback = func(ctx context.Context, grammar zcdb.Grammar, elapsed time.Duration, sqlStr string, args []any) {
		if elapsed > 200*time.Millisecond {
			slog.Default().Info("slow sql", "dialect", fmt.Sprintf("%T", grammar), "sql", sqlStr, "args", args)
		}
	}
	db, err := zcdb.NewDBDao(pool, "mysql", onSQL, "", "")
	_, _ = db, err
}
```

影响：

- 旧回调实现缺少参数，无法赋值给新签名，编译期报错；匿名函数字面量只要补齐 `grammar Grammar` 即可。
- `NewDBDao` 的第三个参数在 v1.6.0 起使用具名类型 `SlowSQLCallback`。参数列表一致的无名函数值仍可赋值，但显式声明为旧签名的变量必须改。
- `grammar` 与 DAO 共用实例、只读使用，可用于判断方言或自行编译；不要在回调里修改它。
- 回调为 nil 时不计时（零开销）；回调内 panic 会被 recover 隔离，不影响主流程。

## Pluck：拆分为 Pluck / PluckMap / KeyBy

旧 `Pluck(ctx, dest, columns ...string)` 按目标容器分三种模式；新 API 按容器拆成三个方法，并把 map 的列参数顺序统一为「键在前、值在后」。

| 目标容器 | v1.7.0 及更早 | 当前代码 |
|---|---|---|
| 切片 `[]T` | `Pluck(ctx, &s, "col")` | `Pluck(ctx, &s, "col")`（不变） |
| 标量 map `map[K]V` | `Pluck(ctx, &m, "value", "key")`（值列在前） | `PluckMap(ctx, &m, "key", "value")`（键列在前） |
| 结构体 map `map[K]Struct` | `Pluck(ctx, &m, "key")` | `KeyBy(ctx, &m, "key")` |

```go
func example() {
	// 旧（v1.7.0 及更早）
	var names []string
	err := db.Builder().Table("users").Pluck(ctx, &names, "name")

	var nameByID map[int64]string
	err = db.Builder().Table("users").Pluck(ctx, &nameByID, "name", "id") // 值 name、键 id

	var userByID map[int64]User
	err = db.Builder().Table("users").Pluck(ctx, &userByID, "id")

	_, _ = err, names
}
```

```go
func example() {
	// 新（当前代码）
	var names []string
	err := db.Builder().Table("users").Pluck(ctx, &names, "name")

	var nameByID map[int64]string
	err = db.Builder().Table("users").PluckMap(ctx, &nameByID, "id", "name") // 键 id、值 name

	var userByID map[int64]User
	err = db.Builder().Table("users").KeyBy(ctx, &userByID, "id")

	_, _ = err, names
}
```

**列序变化是这次拆分最容易出错的地方。** 旧 map 模式编译为 `SELECT value, key`，扫描顺序是「先值后键」；新的 `PluckMap(keyColumn, valueColumn)` 编译为 `SELECT key, value`，扫描顺序是「先键后值」。

```go
func example() {
	var m map[string]string

	// 旧：code 作键、name 作值
	err := db.Builder().Table("lookup").Pluck(ctx, &m, "name", "code")

	// 错误迁移：只改方法名、没换参数顺序 —— 编译通过，但键是 name、值是 code
	err = db.Builder().Table("lookup").PluckMap(ctx, &m, "name", "code")

	// 正确迁移：键列在前
	err = db.Builder().Table("lookup").PluckMap(ctx, &m, "code", "name")

	_, _ = err, m
}
```

当键和值都是 `string` 时，顺序写反不会报错，只会得到一张键值颠倒的 map；当键是数值、值列是文本时，通常会在扫描阶段报类型转换错误。不要依赖运行期报错来发现这个错误。

其它影响：

- 新的 `Pluck` 只接受切片目标；把 map 传给 `Pluck` 会返回 `ErrPluckDest`（消息会提示改用 `PluckMap` 或 `KeyBy`）。
- 旧 `Pluck` 的列数错误（切片传多列、map 传错列数）由 `ErrPluckColumns` 在运行期返回；新签名在编译期就约束了列数：切片 1 列、`PluckMap` 2 列、`KeyBy` 1 列。
- 三者仍都会**覆盖** Builder 当前的 SELECT 列；`Pluck` 仍是追加到现有切片，`PluckMap` / `KeyBy` 仍在目标 map 为 nil 时自动初始化。
- 键值类型限制更明确：`PluckMap` 的键和值都不能是结构体/结构体指针，`KeyBy` 的值必须是结构体/结构体指针、键不能是结构体/结构体指针。

## 错误哨兵：ErrPluckColumns 移除

`ErrPluckColumns` 已被删除，目标容器错误拆成三个哨兵：

| 旧哨兵 | 覆盖范围 | 新哨兵 | 覆盖范围 |
|---|---|---|---|
| `ErrPluckDest` | 切片或 map 目标非法 | `ErrPluckDest` | 仅 `Pluck`：非 nil 切片指针（map 目标改报本错误并提示替代方法） |
| `ErrPluckColumns` | 列数与目标容器不匹配 | `ErrPluckMapDest` / `ErrKeyByDest` | 分别对应 `PluckMap` / `KeyBy` 的目标校验；列数已改由方法签名约束 |

```go
func example() {
	// 旧：切片传两列，运行期返回 ErrPluckColumns
	var names []string
	if err := db.Builder().Table("users").Pluck(ctx, &names, "name", "email"); errors.Is(err, zcdb.ErrPluckColumns) {
		return
	}
}
```

```go
func example() {
	// 新：按目标容器匹配对应哨兵
	var names []string
	err := db.Builder().Table("users").Pluck(ctx, &names, "name")
	switch {
	case errors.Is(err, zcdb.ErrPluckDest):
		// Pluck 目标必须是非 nil 的切片指针
	case errors.Is(err, zcdb.ErrPluckMapDest):
		// PluckMap 目标必须是非 nil 的标量 map 指针
	case errors.Is(err, zcdb.ErrKeyByDest):
		// KeyBy 目标必须是非 nil 的结构体值 map 指针
	}
}
```

影响：

- 代码中任何 `errors.Is(err, zcdb.ErrPluckColumns)` 都会编译失败，必须删除或改到新哨兵。由于列数错误已前移到编译期，大多数此类分支可以直接删除。
- `ErrPluckDest` 的错误消息已收窄为「切片」语义；不要用错误字符串做判断，用 `errors.Is`。
- 目标容器类型不合法仍会在运行期返回错误；键列名写错等 SQL 层问题仍由数据库返回。

## 写操作 ORDER BY / LIMIT：从静默忽略到编译期拒绝

`Update` / `Increment` / `Decrement` / `Delete` / `DeleteJoin` 会读取 Builder 上的 `OrderBy` 与 `Limit`。从 v1.7.0 起，方言或语句形态不支持时在编译期返回 `ErrWriteOrderLimitUnsupported`：

| 方言 / 形态 | v1.6.1 及更早 | v1.7.0 及以后 |
|---|---|---|
| MySQL 单表 UPDATE / DELETE / Increment / Decrement | 编译进 SQL 并执行 | 不变，仍支持 |
| MySQL 多表 UPDATE（带 JOIN） | 编译进 SQL，执行时被引擎以 `Error 1221` 拒绝 | 编译期返回 `ErrWriteOrderLimitUnsupported` |
| MySQL `DeleteJoin` | 子句未进入 SQL，被静默忽略 | 编译期返回 `ErrWriteOrderLimitUnsupported` |
| PostgreSQL / SQLite 的 UPDATE / DELETE / DeleteJoin / Increment / Decrement | 子句未进入 SQL，被静默忽略（写范围扩大到全部命中行） | 编译期返回 `ErrWriteOrderLimitUnsupported` |
| Insert 族、`Truncate` | 不读取这两个状态，静默忽略 | 不变 |

判定细节：`Limit(n <= 0)` 不输出 LIMIT，不计入；`Offset` 不进入任何写语句，也不在校验范围。

```go
func example() {
	// 旧（v1.6.1 及更早）：PostgreSQL/SQLite 下 ORDER BY/LIMIT 被静默丢弃，
	// 实际更新的是全部 status = 'pending' 的行，而不是前 100 行。
	_, err := db.Builder().Table("users").
		Where("status", "=", "pending").
		OrderBy("id", "ASC").Limit(100).
		Update(ctx, User{Status: "archived"})
	_ = err
}
```

```go
func example() {
	// 新：先取主键，再按主键写（三方言通用；需要原子性时放进 Transaction）
	var ids []int64
	if err := db.Builder().Table("users").
		Where("status", "=", "pending").
		OrderBy("id", "ASC").Limit(100).
		Pluck(ctx, &ids, "id"); err != nil {
		return
	}

	keys := make([]any, len(ids))
	for i, id := range ids {
		keys[i] = id
	}
	// ids 为空时 WhereIn 编译为 0 = 1，不会误写全表
	if _, err := db.Builder().Table("users").
		WhereIn("id", keys).
		Update(ctx, User{Status: "archived"}); err != nil {
		return
	}
}
```

迁移方式：

- PostgreSQL / SQLite：改成「先查主键再写」的两步；或删掉 `OrderBy` / `Limit`，用 WHERE 条件表达真实范围。PostgreSQL 也可写成 `WHERE id IN (SELECT ... ORDER BY ... LIMIT n)` 的单语句，但 MySQL 对同表子查询有 `Error 1093` 限制，跨方言代码不建议这么做。
- MySQL 多表 UPDATE 与 `DeleteJoin`：删除这两个子句，或改成「先取主键 + `WhereIn`」。
- MySQL 单表写：可以保留；注意省略 `OrderBy` 时由引擎决定改/删哪几行，结果不确定。

影响：以前 PG/SQLite 上「带 LIMIT 的写」会成功但实际扩大范围，属于**数据正确性风险**；升级后会变成显式错误，必须逐处确认原意。MySQL 多表场景则从「执行期错误」提前到「编译期错误」，便于测试发现。

## 默认注释：从无到有

v1.2.0 起 `NewDBDao` 接受第五参数作为默认短业务标识。传空串时行为与旧版一致；传非空值时，所有经 Builder 编译的 SQL 末尾追加 ` /* comment */`。

```go
func example() {
	// 旧（v1.1.1 及更早）：没有默认注释
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	sql, _, _ := db.Builder().Table("users").Where("id", "=", 1).ToSelect()
	// SQL: SELECT * FROM `users` WHERE `id` = ?
	_, _, _ = db, err, sql
}
```

```go
func example() {
	// 新（v1.2.0+）：启用默认注释
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "", "app:user-service")
	sql, _, _ := db.Builder().Table("users").Where("id", "=", 1).ToSelect()
	// SQL: SELECT * FROM `users` WHERE `id` = ? /* app:user-service */
	_, _, _ = db, err, sql

	// 单个 Builder 显式清空；清空后不回退 DAO 默认值
	sql, _, _ = db.Builder().Table("users").Comment("").ToSelect()
	// SQL: SELECT * FROM `users`
	_ = sql
}
```

影响：

- SQL 文本变化会影响：精确 SQL 断言、慢 SQL 日志匹配/聚合、审计留痕、依赖 SQL 文本的缓存键。升级时如果不想改变基线，统一传 `""`。
- 注释在 DAO 创建时规范化：`*`、`/`、`\` 与全部 Unicode 控制字符替换为空格，去首尾空白，超过 255 rune 截断；非法 UTF-8 转为 U+FFFD。规范化有损且不脱敏，**不得传外部输入、请求体、密码或令牌**。
- 只影响 Builder 的编译出口；DAO 原生 `Exec` / `Query` / `QueryPrimary`、Schema 查询、直接调用 `Grammar` 都不会自动追加。SQLite `Truncate` 的辅助语句也不会自动追加。
- `Comment(text)` 可覆盖或清空；`Clone` 保留当前注释状态（包括显式清空）。

## 迁移对照总表

| # | 变更 | 旧写法 | 新写法 | 失败形态 |
|---|---|---|---|---|
| 1 | `NewDBDao` 四参数 → 五参数 | `NewDBDao(pool, "mysql", nil, "")` | `NewDBDao(pool, "mysql", nil, "", "")` | 编译错误 |
| 2 | `SlowSQLCallback` 增加 grammar | `func(ctx, elapsed, sqlStr, args)` | `func(ctx, grammar, elapsed, sqlStr, args)` | 编译错误 |
| 3 | `Pluck` 切片签名收紧 | `Pluck(ctx, &s, "col")`（`columns ...string`） | `Pluck(ctx, &s, "col")`（`column string`） | 单列调用源码不变；多列调用编译错误 |
| 4 | map 标量提取 | `Pluck(ctx, &m, "value", "key")` | `PluckMap(ctx, &m, "key", "value")` | 未迁移则编译错误；只改名不换序则运行期报错或键值颠倒 |
| 5 | map 结构体提取 | `Pluck(ctx, &m, "key")` | `KeyBy(ctx, &m, "key")` | 旧写法能编译，但新 `Pluck` 对 map 目标返回 `ErrPluckDest` |
| 6 | `ErrPluckColumns` 移除 | `errors.Is(err, zcdb.ErrPluckColumns)` | `ErrPluckDest` / `ErrPluckMapDest` / `ErrKeyByDest` | 编译错误 |
| 7 | 默认注释 | 无注释 | 第五参数传 `""` 保持基线，或传固定短标识 | 无编译错误；SQL 文本变化 |
| 8 | 写语句 ORDER BY/LIMIT（PG/SQLite） | 静默忽略、扩大写范围 | 先取主键再写，或删掉子句 | `ErrWriteOrderLimitUnsupported` |
| 9 | 写语句 ORDER BY/LIMIT（MySQL 多表/DeleteJoin） | 执行期 1221 / 静默忽略 | 去掉子句或改「先取主键 + WhereIn」 | `ErrWriteOrderLimitUnsupported` |

## 按版本升级的检查清单

### 从 v1.1.1 及更早升级到 v1.2.0+

- [ ] 搜索所有 `NewDBDao(` 调用，末尾补第五参数；不需要默认注释时统一传 `""`。
- [ ] 决定是否启用默认注释：只使用固定、低基数的服务/模块标识，禁止拼接外部输入。
- [ ] 更新精确 SQL 断言、慢 SQL 日志解析与监控聚合规则（启用注释时 SQL 末尾会多出后缀）。
- [ ] `go build ./...` 确认没有遗漏的调用点。

### 从 v1.5.1 及更早升级到 v1.6.0+

- [ ] 搜索 `SlowSQLCallback` 与所有传给 `NewDBDao` 第三参数的回调，在 `ctx` 之后补 `grammar zcdb.Grammar`。
- [ ] 显式声明回调类型的变量改为 `zcdb.SlowSQLCallback`（或与新签名一致的无名函数类型）。
- [ ] 回调内如果按方言分支，改用 `grammar` 判断，而不是自己再推导方言。
- [ ] 不要在回调内修改 `grammar`；它与 DAO 共用同一实例，只读使用。

### 从 v1.6.1 及更早升级到 v1.7.0+

- [ ] 搜索写链上的 `OrderBy` / `Limit`，重点检查 `Update` / `Increment` / `Decrement` / `Delete` / `DeleteJoin`。
- [ ] PostgreSQL / SQLite：删除子句，或改为「先取主键 + `WhereIn`」两步；需要原子性时放进 `Transaction`。
- [ ] MySQL：单表写可保留；多表 UPDATE 与 `DeleteJoin` 必须去掉子句或改写。
- [ ] 审计历史上「带 LIMIT 的写」是否曾经扩大范围（尤其 PG/SQLite 的批量修正脚本）。
- [ ] 用三方言测试覆盖改写后的写路径（SQLite 内存库即可跑；MySQL/PostgreSQL 用集成测试环境）。

### 从 v1.7.0 升级到当前代码（Pluck 拆分）

- [ ] 搜索所有 `.Pluck(` 调用，区分目标容器：切片保持不变，map 标量改 `PluckMap`，map 结构体改 `KeyBy`。
- [ ] 对 `PluckMap` 逐处核对参数顺序：**键列在前、值列在后**，尤其是键和值同为 `string` 的场景。
- [ ] 删除所有 `zcdb.ErrPluckColumns` 引用，按容器改用三个新哨兵。
- [ ] 检查是否有「把 map 传给 `Pluck`」的旧代码：现在返回 `ErrPluckDest`。
- [ ] 检查复用同一 Builder 的调用点：`Pluck` / `PluckMap` / `KeyBy` 都会覆盖 SELECT 列，必要时先 `Clone()`。
- [ ] 跑 `go test ./zcdb/... -count=1`，并确认新增的 map 列序用例真实覆盖了键值方向。

### 通用验收

- [ ] `go build ./...`、`go vet ./...` 通过。
- [ ] `go test ./zcdb/... -count=1` 通过；集成测试没有因数据库不可达被意外跳过（SQLite 必须真实执行）。
- [ ] 搜索是否还有旧签名/旧哨兵/旧 `Pluck(map)` 的残留：`NewDBDao(`、`SlowSQLCallback`、`ErrPluckColumns`、`Pluck(ctx, &` 后跟 map 变量的调用。
- [ ] 更新项目内的 SQL 快照、文档示例与监控看板，确认注释与写语句子句的最终文本。

## 相关阅读

- `Pluck` / `PluckMap` / `KeyBy` 的完整语义：[builder-query](../reference/builder-query.md)
- 写操作保护与 ORDER BY/LIMIT 的方言边界：[safety](../concepts/safety.md)
- DAO 创建、慢 SQL 回调与默认注释：[getting-started](../getting-started.md)
- 错误哨兵全集与匹配方式：[errors](../reference/errors.md)
- 写操作执行语义：[builder-exec](../reference/builder-exec.md)
