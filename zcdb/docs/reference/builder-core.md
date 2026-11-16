# Builder 核心：构造、数据源与通用标记

`Builder` 不是一份不可变的 SQL 描述，而是一个被链式方法持续改写的状态机：`Table`/`Select`/`Where` 改查询状态，`Comment` 改输出标记，`Force`/`Primary` 只改执行标记。所有链式方法都返回同一个 `*Builder`，真正生成 SQL 的是 `ToXxx` 系列编译入口。状态字段的完整清单与复制策略见 [builder-state](../concepts/builder-state.md)，编译顺序与绑定顺序见 [compile-pipeline](../concepts/compile-pipeline.md)。

本文覆盖构造与通用操作：`NewBuilder`、`db.Builder()`、`Table`/`TableSub`、`Comment`、`Force`、`Primary`、`Clone`，以及与 Builder 使用相关的 `DBDao`/`Grammar` 取得方式。SELECT 列见 [builder-select](../reference/builder-select.md)，WHERE 条件见 [builder-where](../reference/builder-where.md)。

示例统一给出 MySQL 形态（反引号、`?` 占位符），并假定 DAO 第五参数为 `""`；未显式调用 `Comment` 时 SQL 无注释。`db`、`ctx` 等是外部占位变量，示例只保证语法可解析，不保证可独立运行。

## 先读结论

1. `db.Builder()` **每次返回新对象**；链式方法改的是接收者本身。要在基线上派生互不影响的查询，必须显式 `Clone()`。
2. 构造阶段不生成 SQL，也不返回错误：非法运算符、非法子查询入参等写入 `Builder.err`，由后续 `ToXxx` 统一短路返回。
3. 数据源 `Table` 与 `TableSub` 互斥，后调用者生效；没有数据源时 `ToSelect`/`ToInsert`/`ToUpdate`/`ToDelete` 等返回 `ErrEmptyTable`。
4. `Comment` 是**输出标记**（追加到最终 SQL 末尾），`Force`/`Primary` 是**执行标记**（不改 SQL）。三者都会被 `Clone` 保留。
5. 只有 `ToXxx` 会做「错误短路 → 数据源校验 → 环检测 → 方言形态校验 → 编译 → 追加注释」；直接调用 `Grammar` 不会做这些。

## 构造入口：NewBuilder 与 DBDao.Builder

### NewBuilder

```text
func NewBuilder(grammar Grammar, dao *DBDao) *Builder
```

**语义**：创建一份独立的空 Builder，是包外唯一可用的构造器（`Builder` 的字段全部不导出，不能用结构体字面量构造）。`dao` 非 nil 时复制 DAO 的默认 SQL 注释；`dao` 为 nil 时得到「只能编译、没有默认注释」的构造器。

**SQL**：

```go
func example() {
	g := zcdb.NewMySQLGrammar()
	b := zcdb.NewBuilder(g, nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err := b.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `id` = ?
	// args: [1]
	_, _, _ = sqlStr, args, err
}
```

**边界与错误**：

- `dao == nil` 时只能使用 `ToXxx` 编译；调用执行类终端方法（`Find`/`Update`/`Delete` 等）会因解引用 nil DAO 直接 panic，而不是返回受控错误。
- `grammar` 必须非 nil；三方言可用 `zcdb.NewMySQLGrammar()`、`zcdb.NewPostgresGrammar()`、`zcdb.NewSQLiteGrammar()` 构造。
- `dao` 非 nil 时，`NewBuilder` 只复制**注释**；标签名等配置仍通过 `Builder.dao` 读取，见 [mapping](../concepts/mapping.md)。

**与其他方法的区别**：`db.Builder()` 等价于 `NewBuilder(db.Grammar(), db)`，额外保证 DAO 与其默认注释来自同一个工厂；需要脱离 DAO 纯编译时用 `NewBuilder(grammar, nil)`。

### db.Builder()

```text
func (d *DBDao) Builder() *Builder
```

**语义**：创建一份新的 Builder，绑定 DAO 持有的 `Grammar` 与 `*DBDao`，并复制 `NewDBDao` 第五参数规范化后的默认 SQL 注释。每次调用都是独立对象，互不共享查询状态。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").Where("id", "=", 1).ToSelect()
	// DAO 默认注释为 "app:user-service" 时：
	// SQL: SELECT * FROM `users` WHERE `id` = ? /* app:user-service */

	sqlStr, _, _ = db.Builder().Table("logs").Comment("").ToSelect()
	// SQL: SELECT * FROM `logs`（清空后不回退 DAO 默认值）
	_, _ = sqlStr, db
}
```

**边界与错误**：

- 默认注释在 DAO 构造时已规范化并固定，之后无法修改；`Builder` 之间、`Builder` 与 DAO 之间互不影响。`NewDBDao` 的第四参数是列映射标签名（与注释无关，见 [mapping](../concepts/mapping.md)），第五参数才是默认 SQL 注释。
- `Builder` 没有锁，链式方法会写字段；**不要跨请求/跨 goroutine 共享同一个 Builder**，每个请求用 `db.Builder()` 新建，或 `Clone()` 出私有副本。
- `db.Builder()` 本身不会校验参数，也不返回 error。

**与其他方法的区别**：与 `NewBuilder` 的唯一差别是「DAO 与默认注释来自 DAO 工厂」；执行类终端方法必须使用这种带 DAO 的 Builder（或把 DAO 传给 `NewBuilder`）。

### 取得 Grammar：db.Grammar() 与方言构造器

```text
func (d *DBDao) Grammar() Grammar
func NewMySQLGrammar() *MySQLGrammar
func NewPostgresGrammar() *PostgresGrammar
func NewSQLiteGrammar() *SQLiteGrammar
```

**语义**：`Grammar` 是方言编译器，负责标识符包裹、占位符形态与语句结构。DAO 的 Grammar 由 `NewDBDao` 的 `dialect` 推导（`mysql`；`postgresql`/`postgres`/`pgsql`；`sqlite`/`sqlite3`），`db.Grammar()` 返回该共享实例；无 DAO 时用三个 `NewXxxGrammar()` 构造器。

**SQL**：

```go
func example() {
	// 无 DAO 的纯编译：显式给方言
	sqlStr, args, err := zcdb.NewBuilder(zcdb.NewPostgresGrammar(), nil).
		Table("users").Where("id", "=", 1).ToSelect()
	// SQL: SELECT * FROM "users" WHERE "id" = $1
	// args: [1]

	// 直接调用 Grammar 做方言编译（不追加默认注释，也不做 Builder 的前置校验）
	rawSQL, err := db.Grammar().CompileRaw("SELECT * FROM users WHERE id = ?", []any{1})
	// SQL: SELECT * FROM users WHERE id = 1
	_, _, _, _ = sqlStr, args, rawSQL, err
}
```

**边界与错误**：

- `db.Grammar()` 返回的是 DAO 内部同一实例，只读使用：内置三方言实现没有对外可写状态（PostgreSQL 的参数计数器只在单次编译的克隆副本上累加），并发调用安全。
- 直接调用 `Grammar.CompileSelect(b, b.columns)` 会**绕过** `ToSelect` 的错误短路、`ErrEmptyTable`、环检测与方言形态校验，也不会追加注释；只在已经确认 Builder 状态合法、或需要自行拼装 SQL 时使用。
- `NewXxxGrammar()` 不与任何 DAO 关联，用它编译不会带 DAO 默认注释。

**与其他方法的区别**：`db.Builder().ToSelect()` 是「带全部校验与注释的完整编译」；`db.Grammar()` 暴露的 `Compile*` 方法只是「方言文本生成器」。

## Table / TableSub：数据源

`Table` 与 `TableSub` 共同决定 `FROM`。二者互斥：调用 `Table` 会清空 `TableSub` 及其别名；调用 `TableSub` 不会清空 `table`，但只要 `tableSub` 非 nil，编译就优先输出派生表（`table` 被忽略）。没有数据源时 `ToSelect` 返回 `ErrEmptyTable`，不会生成残缺 SQL（只靠 UNION 的计数/存在性入口是例外）。

### Table

```text
func (b *Builder) Table(tableName string) *Builder
```

**语义**：设置主表名，同时清空 `TableSub` 设置的 FROM 子查询与别名，保证「后调用者生效」。这是替换语义，多次调用以最后一次为准。

**SQL**：

```go
func example() {
	sqlStr, args, _ := db.Builder().Table("users").Where("age", ">", 18).ToSelect()
	// SQL:  SELECT * FROM `users` WHERE `age` > ?
	// args: [18]

	// Table 后于 TableSub：派生表被清除，回到普通表
	sub := db.Builder().Table("orders").Select("user_id")
	sqlStr, _, _ = db.Builder().TableSub(sub, "o").Table("users").ToSelect()
	// SQL: SELECT * FROM `users`
	_, _ = sqlStr, args
}
```

**边界与错误**：

- `Table("")` 等同于清空数据源；若此时没有 `TableSub`，`ToSelect` 返回 `ErrEmptyTable`（`ToInsert`/`ToUpdate`/`ToDelete`/`ToTruncate` 也要求 `table` 非空）。
- 表名字符串最终交给 `Grammar.WrapTable`：支持 `"users AS u"` 形式（按 ` AS ` 拆成表名与别名）；不是原始 SQL 片段，不能用来塞子查询或表达式。
- 未设置数据源时不会在 `Table` 调用处报错，错误延迟到编译入口。

**与其他方法的区别**：`TableSub` 用子查询做 FROM（派生表）；JOIN 的派生表用 `JoinSub`，见 [builder-join](../reference/builder-join.md)。

### TableSub

```text
func (b *Builder) TableSub(sub *Builder, alias string) *Builder
```

**语义**：设置 FROM 子查询（派生表），编译为 `(子查询) AS 别名`，与 `Table` 互斥、后调用者生效。子查询的绑定参数排在最外层 WHERE 之前（SELECT 类绑定顺序为 `SELECT_SUB → FROM_SUB → JOIN → WHERE → …`）。

**SQL**：

```go
func example() {
	sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
	sqlStr, args, _ := db.Builder().TableSub(sub, "o").Where("o.user_id", ">", 1).ToSelect()
	// SQL:  SELECT * FROM (SELECT `user_id` FROM `orders` WHERE `amount` > ?) AS `o` WHERE `o`.`user_id` > ?
	// args: [100 1]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- `alias` 是标识符：为空时会编译出空标识符（MySQL 中表现为 `AS` 后面跟一对空反引号）；请显式给出非空别名。
- `sub` 必须自己设置 `Table`/`TableSub`。外层只检查自己的数据源，**不会**替子查询检查 `ErrEmptyTable`；子查询缺表时编译产物形如 `(SELECT * FROM <空标识符>) AS <别名>`，由数据库在执行期拒绝。
- 子查询图存在环（如 `b.TableSub(b, "x")`）时，`ToSelect` 返回 `ErrCyclicQuery`，`Clone` 返回携带该错误的副本；子 Builder 累积的错误也会经环检测上抛。
- 结构化子查询自身的 `Comment` 不会进入外层 SQL，也不会被清除；只有最外层追加一次，见下文 Comment。

**与其他方法的区别**：`TableSub` 是 FROM 派生表；`SelectSub` 是 SELECT 列标量子查询；`JoinSub` 是 JOIN 右表派生表；三者绑定位置不同。

## Comment（短业务标识）

### Comment

```text
func (b *Builder) Comment(text string) *Builder
```

**语义**：规范化并覆盖当前 Builder 的短业务标识，编译时追加在最终 SQL 末尾，形如 ` /* text */`。`NewBuilder` 从 DAO 复制的默认注释、本方法覆盖的值、`Comment("")` 清空后的空值，三种状态都随 Builder 保存，直到下一次 `Comment` 或克隆。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("orders").Comment("report:daily").ToSelect()
	// SQL: SELECT * FROM `orders` /* report:daily */

	sqlStr, _, _ = db.Builder().Table("logs").Comment("").ToSelect()
	// SQL: SELECT * FROM `logs`
	_, _ = sqlStr, db
}
```

**规范化规则（有损保存）**：所有 `*`、`/`、`\` 和 `unicode.IsControl` 为真的控制字符逐 rune 替换为 ASCII 空格；非法 UTF-8 转 U+FFFD；`strings.TrimSpace` 去首尾空白，内部空格不合并；超过 255 rune 截断后再次去首尾空白。不能依赖完整回显：URL/Unix 路径中的 `/` 与 Windows 路径中的 `\` 都会被替换为空格。

**边界与错误**：

- 多次调用以最后一次为准；空串、纯空白或规范化后为空都会**清空**注释，清空后不回退 DAO 默认值。编译/执行不会「消费」注释，不存在「仅下一次生效」。
- 规范化不新增错误，也不清除已有错误：Builder 已累积的 `ErrInvalidOperator` 等仍会在编译时优先返回。
- 注释只承载服务名、模块名、报表名等**短业务标识**；不得透传外部输入、请求体、URL 参数、密码、令牌或其他秘密。注释可能进入慢 SQL 回调、数据库日志和监控，本功能**不脱敏**。
- 结构化子查询（`TableSub`/`SelectSub`/`WhereSub`/`WhereExists` 等）自身携带的注释不进入外层产物，也不会被清除；只有最外层追加一次。原始 SQL、Schema 与直接 `Grammar` 调用不自动添加注释；Raw/Expression 中的手工注释不在去重范围内。
- 推荐固定、低基数标识：动态高基数文本可能影响预编译缓存或监控聚合，不能承诺零性能影响。

**与其他方法的区别**：DAO 第五参数是**默认值**（只在 `NewDBDao` 构造时规范化一次，之后不可变），`Comment` 是**当前 Builder 的覆盖值**；`Force`/`Primary` 不改变 SQL 文本，`Comment` 会改变 SQL 文本。`Clone` 保留默认、覆盖或已清空三种状态，修改副本不影响原 Builder 或 DAO；注释不改变并发安全性。

## Force（全表写授权）

### Force

```text
func (b *Builder) Force() *Builder
```

**语义**：标记「允许执行无有效 WHERE 的写操作」。`Update`/`Increment`/`Decrement`/`Delete`/`DeleteJoin` 默认拒绝无有效限定条件的执行；`Force()` 表示调用方已知意图并显式授权。它只影响执行层保护判定，**不改变编译出的 SQL**。

**SQL**：

```go
func example() {
	affected, err := db.Builder().Table("users").Force().Delete(ctx)
	// 无 Force() 时返回 ErrDeleteWithoutWhere；加了 Force() 后执行：
	// SQL: DELETE FROM `users`
	_, _ = affected, err

	affected, err = db.Builder().Table("users").Force().Update(ctx, struct {
		Status string `db:"status"`
	}{Status: "archived"})
	// SQL: UPDATE `users` SET `status` = ?
	// args: [archived]
	_, _ = affected, err
}
```

**边界与错误**：

- 只影响 `Update`/`Increment`/`Decrement`/`Delete`/`DeleteJoin` 的「有效限定」保护，不改 SQL、不改路由、不改变受影响行数语义。
- 不放行其它错误：普通 `Delete` 只要 Builder 带 JOIN 就返回 `ErrDeleteWithJoin`（`Force()` 也不放行），按关联删除请用 `DeleteJoin`；没有数据源、子查询成环等错误照常返回。
- 保护判定按 SQL 优先级把 WHERE 静态归约为恒真/恒假/未知：空 `WhereNotIn`（编译为 `1 = 1`）、空嵌套组、无条件的 `EXISTS` 都不算有效限定；非空 `WhereRaw`（包括 `WhereRaw("1 = 1")`）一律视为依赖行数据，是刻意保留的逃生口。
- 注释不是 WHERE/JOIN 条件，不能用 `Comment` 替代 `Force()`；`WhereRaw("1=1")` 虽然能通过保护判定，仍建议显式 `Force()`，便于代码审查与检索。
- 标记经 `Clone` 保留；同一 Builder 上调用一次即一直有效。

**与其他方法的区别**：`Force` 是**执行授权**（无 SQL 变化）；`WhereRaw("1 = 1")` 是**构造一个条件**（SQL 变化）——两者都能让无实际收窄的写操作通过，但只有前者在代码上明确表达「全表操作」意图。保护判定的完整规则见 [safety](../concepts/safety.md)。

## Primary（强制主库读）

### Primary

```text
func (b *Builder) Primary() *Builder
```

**语义**：标记本次读查询强制走写（主库）连接，典型场景是写后立即读（订单写入后马上回查，从库可能因复制延迟尚无数据）。它**不加任何锁、不改变编译出的 SQL**，只影响执行层的连接路由。

**SQL**：

```go
func example() {
	var order struct {
		Id int64 `db:"id"`
	}
	err := db.Builder().Table("orders").Where("id", "=", 1).Primary().First(ctx, &order)
	// SQL: SELECT * FROM `orders` WHERE `id` = ? LIMIT 1（与不加 Primary 完全一致，但强制命中主库）
	// args: [1]
	_ = err
}
```

**边界与错误**：

- 只对读查询有可观察效果；写操作本来就固定走主库/事务连接，加不加 `Primary()` 都一样。
- 带锁查询（`LockForUpdate`/`SharedLock`）本就会强制主库，`Primary()` 对它们不额外加锁；锁子句的 SQL 变化由锁方法负责。
- 标记经 `Clone` 保留，所以 `First`/`Value` 等内部克隆的终端方法、以及不克隆的 `Paginate` 都生效。
- 在事务中执行时读写都走事务连接，`Primary()` 不再决定连接。

**与其他方法的区别**：`Primary()` 只改路由（SQL 无锁、无变化）；`LockForUpdate`/`SharedLock` 既改 SQL（追加锁子句）又强制主库。DAO 层的对应入口是 `QueryPrimary`，见 [execution-routing](../concepts/execution-routing.md) 与 [builder-exec](../reference/builder-exec.md)。

## Clone（查询复用）

### Clone

```text
func (b *Builder) Clone() *Builder
```

**语义**：返回一份与当前 Builder 状态完全隔离的深拷贝。列、FROM 子查询、SELECT 子查询、JOIN（含派生表、嵌套 join 组、ON 条件、嵌套条件及其累积错误字段）、WHERE（含 `Values`/`Bindings` 切片、嵌套组与子查询）、GROUP BY、HAVING、ORDER BY、UNION、锁子句、`force`、`usePrimary`、注释（含显式清空）全部复制。

**SQL**：

```go
func example() {
	base := db.Builder().Table("users").Where("status", "active")
	admins := base.Clone().Where("role", "admin")

	sqlBase, _, _ := base.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `status` = ?
	// args: [active]

	sqlAdmins, _, _ := admins.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `status` = ? AND `role` = ?
	// args: [active admin]
	_, _ = sqlBase, sqlAdmins
}
```

**边界与错误**：

- 绑定值本身（`Value`/`Min`/`Max` 与 `Bindings` 元素，类型为 `any`）不做深拷贝：传入切片、map 或指针时，副本与原 Builder 共享同一份底层数据。编译与执行不会修改它们，因此共享是安全的；但调用方事后修改这些值，两边都会看到。
- 子查询图存在环（自引用/互引用，如 `TableSub`/`Union` 传入自身）时，`Clone` 返回携带 `ErrCyclicQuery` 的**浅副本**而不是递归崩溃；对副本调用 `ToXxx` 会返回该错误、不生成 SQL。子 Builder 携带的累积错误同样随副本保留。
- 克隆保留递归子 Builder 各自的注释，且副本与原 Builder 互不影响。
- `Clone` 不会让 Builder 变成并发安全的：副本之间独立，但单个 Builder 仍不能被多个 goroutine 同时改写。

**与其他方法的区别**：`First`/`Value`/`CursorBy` 等终端方法内部用 `Clone` 避免污染调用方的 Builder；`Paginate` 不克隆，它靠 `ToCount` 的临时状态保存/恢复复用原 Builder。需要「保留基线、派生多个查询」时用 `Clone`；直接链式调用改的是同一个对象。

## 标记速查

| 方法 | 改动的状态 | 是否改 SQL | `Clone` 是否保留 | 作用层 |
|---|---|---|---|---|
| `Table` / `TableSub` | 数据源（互斥） | 是 | 是（子查询深拷贝） | 编译 |
| `Comment` | `comment` | 是（末尾追加一次） | 是 | 编译/执行产物 |
| `Force` | `force` | 否 | 是 | 执行保护 |
| `Primary` | `usePrimary` | 否 | 是 | 连接路由 |
| `Clone` | 复制全部状态 | — | — | 构造 |

## 相关阅读

- 状态字段、替换/追加语义与终端方法的克隆策略：[builder-state](../concepts/builder-state.md)
- 编译入口的校验顺序与注释追加点：[compile-pipeline](../concepts/compile-pipeline.md)
- 完整编译入口与绑定顺序：[builder-compile](../reference/builder-compile.md)
- 方言差异（包裹符、占位符、语句形态）：[dialect-matrix](../reference/dialect-matrix.md)
- 全表写保护与注释安全边界：[safety](../concepts/safety.md)
- 错误哨兵清单：[errors](../reference/errors.md)
