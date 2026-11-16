# 终端查询方法

Builder 的终端查询方法负责「编译 SQL → 选连接 → 执行 → 扫描」的最后一步。它们读取 Builder 当前累积的状态（表、列、WHERE、JOIN、排序、分页、锁、注释），因此同一套链式构造可以被不同的终端方法消费；其中计数、存在性与标量聚合会临时忽略列、排序与分页（见各自小节）。

本文覆盖 `Find`、`First`、`Value`、`Count`、`Exists`、`Max`/`Min`/`Sum`/`Avg`、`Pluck`/`PluckMap`/`KeyBy`、`Paginate`。流式迭代见 [builder-cursor](builder-cursor.md)，行扫描的列映射与类型转换见 [scanner](scanner.md)。

以下示例统一给出 MySQL 形态 SQL（反引号、`?` 占位符），并假定 DAO 第五参数为 `""` 的无注释基线；PG/SQLite 仅标识符改双引号、PG 占位符为 `$1..$N`。示例统一包在 `func example()` 中，仅用于语法检查：`db`、`ctx`、`User` 等都是外部占位，不保证可独立运行。

## 方法一览

| 方法 | 签名 | 结果基数 | 空结果 |
|---|---|---|---|
| `Find` | `Find(ctx, dest any) error` | 0..N 行 | 不写入 dest，`err == nil` |
| `First` | `First(ctx, dest any) error` | 0..1 行 | `sql.ErrNoRows` |
| `Value` | `Value(ctx, dest any) error` | 0..1 行 | `sql.ErrNoRows` |
| `Count` | `Count(ctx) (int, error)` | 标量 | `(0, nil)` |
| `Exists` | `Exists(ctx) (bool, error)` | 标量 | `(false, nil)` |
| `Max`/`Min` | `Max(ctx, column string) (float64, error)` | 标量 | `(0, sql.ErrNoRows)` |
| `Sum`/`Avg` | `Sum(ctx, column string) (float64, error)` | 标量 | `(0, nil)` |
| `Pluck` | `Pluck(ctx, dest any, column string) error` | 切片 | 不追加元素 |
| `PluckMap` | `PluckMap(ctx, dest any, keyColumn, valueColumn string) error` | map | 空 map |
| `KeyBy` | `KeyBy(ctx, dest any, keyColumn string) error` | map | 空 map |
| `Paginate` | `Paginate(ctx, dest any) (int, error)` | 分页 | `(0, nil)`，且不查数据 |

## 执行路由与注释

- 读操作默认路由到从库；`LockForUpdate`/`SharedLock` 与 `Primary()` 强制走主库（前者因锁在从库不生效，后者用于写后读）。细节见 [execution-routing](../concepts/execution-routing.md) 与 [builder-exec](builder-exec.md)。
- `Count`/`Exists`/`Max`/`Min`/`Sum`/`Avg` 在编译期临时清除锁子句（SQL 不带锁），但执行仍按 Builder 上的锁标记与 `Primary()` 选择主库——路由看的是执行时的 Builder 状态，不是最终 SQL 文本。
- 设置 DAO 默认注释或 `Builder.Comment` 后，终端方法使用最终编译的带注释 SQL，不在执行层再次追加。`First`/`Value`/`Pluck`/`PluckMap`/`KeyBy` 等内部派生查询保留当前注释（包括显式清空）。
- 慢 SQL 回调收到的是**实际执行**的 SQL 与原绑定参数。DAO 原始 `Exec`/`Query`/`QueryPrimary` 原文透传（也不剥离手写注释），Schema 查询不自动追加。
- 注释只承载服务名、模块名等短业务标识；不得透传外部输入或秘密，规则的边界见 [safety](../concepts/safety.md)。

## Find

```text
func (b *Builder) Find(ctx context.Context, dest any) error
```

**语义**：编译 `ToSelect` → 执行 → 把结果集逐行扫描进 `dest`。它是「多行读取」的基准方法，`Paginate` 的数据查询阶段与它走同一条扫描路径。

**dest 要求**：`*[]struct` 或 `*[]*struct`（结构体切片指针 / 结构体指针切片指针）。非指针返回 `ErrNotPointer`，指针但目标既不是结构体也不是切片返回 `ErrScanDest`；切片元素既不是结构体也不是结构体指针返回 `ErrInvalidStruct`。注意 `*struct` 不会被拒绝：扫描器会按单行处理（只取第一行），这是退化用法，不要依赖。字段映射、NULL 与类型转换规则与包级扫描函数完全一致，见 [scanner](scanner.md)。

```go
func example() {
	var users []User
	err := db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &users)
	// SQL:  SELECT * FROM `users` WHERE `status` = ?
	// args: [active]

	// 指针切片：每个元素指向独立实例
	var ptrs []*User
	err = db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &ptrs)
	_ = err
}
```

**SQL/结果语义**：Builder 状态原样编译，`Find` 不追加任何 LIMIT/OFFSET/排序。结果行按数据库返回顺序**追加**到 `dest` 现有元素之后，不会先清空——复用同一个切片变量连续调用会累积。

**边界与错误**：

- 无匹配行时不写入任何元素、`err == nil`（`dest` 保持调用前状态；从 nil 开始时仍是空切片）。与 `First`/`Value` 的 `sql.ErrNoRows` 语义不同。
- 编译错误（未设表、子查询成环等）在查询前返回，不会发起数据库往返。
- 扫描失败返回 `zcdb: scan row failed: %w`，可用 `errors.Is` 命中 `ErrScanConvert`。

## First

```text
func (b *Builder) First(ctx context.Context, dest any) error
```

**语义**：在查询上强制 `LIMIT 1` 后执行，扫描第一行到 `dest`。实现为「按需克隆」：`Builder.limit` 已为 1 时直接复用原对象，否则 `Clone()` 出副本并把 `limit` 置 1，因此**不修改调用方 Builder 的 limit**。

**dest 要求**：`*struct`（结构体指针）。非指针、非结构体指针的错误与 `Find` 相同，来自同一套扫描校验。

```go
func example() {
	var user User
	err := db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)
	// SQL:  SELECT * FROM `users` WHERE `id` = ? LIMIT 1
	// args: [1]

	// 已 Limit(1)：跳过克隆，直接使用原 Builder
	var one User
	err = db.Builder().Table("users").Limit(1).OrderBy("id", "ASC").First(ctx, &one)
	_ = err
}
```

**SQL/结果语义**：克隆只改 `limit`，`offset`、排序、条件、锁、注释全部保留。所以 `ForPage(2, 20).First(...)` 取的是第 2 页的第一行（`LIMIT 1 OFFSET 20`）。多行命中时由数据库返回顺序决定取哪一行，需要确定性请显式 `OrderBy`。

**边界与错误**：

- 无匹配行返回 `sql.ErrNoRows`，可用 `errors.Is` 匹配。
- `limit` 已被设置为非 1 的正数时会被强制覆盖为 1（例如 `Limit(10).First(...)` 只取 1 行）；`limit <= 0` 时同样补成 1。
- 编译错误与扫描错误原样返回，不再包装成 `ErrNoRows`。

## Value

```text
func (b *Builder) Value(ctx context.Context, dest any) error
```

**语义**：取单行单列的标量值，`dest` 直接交给 `database/sql` 的 `Scan`（不经过本库的 NULL 安全字段包装）。与 `First` 一样按需克隆并强制 `LIMIT 1`，不修改原 Builder。

**dest 要求**：基本类型的指针（`*string`、`*int`、`*int64` 等）、二级指针（`**string`），或实现了 `sql.Scanner` 的类型（如 `sql.NullString`）。`dest` 必须与结果列数一一对应，因此应配 `Select` 只取一列。

```go
func example() {
	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).Value(ctx, &name)
	// SQL:  SELECT `name` FROM `users` WHERE `id` = ? LIMIT 1
	// args: [1]

	// 二级指针区分 NULL 与零值
	var remark *string
	err = db.Builder().Table("users").Select("remark").Where("id", "=", 1).Value(ctx, &remark)
	// err == nil 且 remark == nil → 该列值为 NULL
	// err == nil 且 remark != nil → 正常字符串（可能为空串）
	_ = err
}
```

**SQL/结果语义**：只消费结果集第一行第一列；多余的列会被 `database/sql` 视为目标个数不匹配而报错（不是本库哨兵）。需要 `NULL` 与零值区分时用二级指针或 `sql.NullString`。

**边界与错误**：

- 无匹配行返回 `sql.ErrNoRows`。
- 列值为 `NULL` 而 `dest` 指向非指针标量（如 `*string`）时，`database/sql` 无法把 NULL 赋给它，返回扫描错误；这是调用方选择的目标类型问题，不是 `ErrNoRows`。
- 与 `Find` 不同，`Value` 不做 `db` 标签映射，`dest` 就是扫描目标，类型必须与驱动的返回值兼容。

## Count

```text
func (b *Builder) Count(ctx context.Context) (int, error)
```

**语义**：编译 `ToCount` 执行计数，返回 `int`。计数不改变原 Builder：`ToCount` 通过临时状态保存机制清除 `limit`/`offset`/`orders`/`lockClause` 并按需替换 `columns`，用 `defer` 恢复，即使编译中途 panic 也会还原。

**dest 要求**：无 `dest`（标量返回）。

**SQL/结果语义**：按查询形态分类编译（`t` 为方言包裹后的别名）。

| 查询形态 | COUNT SQL |
|---|---|
| 普通查询 | `SELECT COUNT(*) FROM 表 WHERE ...` |
| 含 UNION | `SELECT COUNT(*) FROM (<完整 UNION 查询>) AS t`，保留各分支原投影（MySQL/PG 的分支自带括号，SQLite 不带，故嵌套括号层数随方言不同） |
| 含 GROUP BY / HAVING | `SELECT COUNT(*) FROM (SELECT 1 FROM ... GROUP BY ...) AS t`，先保留分组结果由外层数行数 |
| GROUP BY + HAVING 且调用方有显式列或 SELECT 子查询 | 同上包裹，但保留调用方投影（HAVING 可引用这些列别名） |
| 含 DISTINCT | `SELECT COUNT(*) FROM (SELECT DISTINCT 列 FROM ...) AS t`，保住去重语义 |

包成子查询而非直接 `SELECT COUNT(*)` 的原因：UNION 直接加聚合会列数失配，GROUP BY/HAVING 会丢分组与筛选，`SELECT DISTINCT COUNT(*)` 的 DISTINCT 对聚合结果无效（会退化成总行数）。

```go
func example() {
	count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
	// SQL:  SELECT COUNT(*) FROM `users` WHERE `status` = ?
	// args: [active]

	// UNION：整体包裹后计数，两个分支的行都算入（UNION ALL 不去重）
	sub := db.Builder().Table("users").Where("age", ">", 25)
	count, err = db.Builder().Table("users").Where("status", "=", "active").UnionAll(sub).Count(ctx)
	// SQL: SELECT COUNT(*) FROM ((SELECT * FROM `users` WHERE `status` = ?) UNION ALL (SELECT * FROM `users` WHERE `age` > ?)) AS `t`
	_ = count
	_ = err

	// DISTINCT：先去重再计数
	count, err = db.Builder().Table("users").Select("city").Distinct().Count(ctx)
	// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
	_ = count
	_ = err
}
```

**边界与错误**：

- 计数**忽略**已设置的分页与排序：`Limit(10)` 不影响总数，`OrderBy` 不进入 COUNT SQL（原状态在编译后恢复）。
- 无匹配行返回 `(0, nil)`；表未设置返回 `ErrEmptyTable`。
- 编译错误、查询错误、`rows.Scan` 错误均如实返回，不吞成 0。
- 大表上只关心「有没有」时用 `Exists`，它不会遍历匹配行。

## Exists

```text
func (b *Builder) Exists(ctx context.Context) (bool, error)
```

**语义**：编译 `ToExists` 后只读一行——有行即存在，命中首行立即返回，避免 `COUNT(*)` 扫描全部匹配行。同样用临时状态机制清除分页/排序/锁。

**dest 要求**：无 `dest`（标量返回）。

**SQL/结果语义**：分两种形态。

| 查询形态 | EXISTS SQL |
|---|---|
| 普通查询（含仅 GROUP BY、DISTINCT） | `SELECT 1 FROM 表 WHERE ... LIMIT 1`（列被替换为常量 `1`） |
| 含 UNION 或 HAVING | `SELECT 1 FROM (原查询，保留原投影) AS t LIMIT 1` |

UNION 必须保留原投影，否则各分支列数不一致、SQL 会被数据库拒绝；HAVING 可能引用 SELECT 别名，也必须保留投影。两者都整体包裹后由外层只取一行。

```go
func example() {
	exists, err := db.Builder().Table("users").Where("email", "=", "a@t.com").Exists(ctx)
	// SQL:  SELECT 1 FROM `users` WHERE `email` = ? LIMIT 1
	// args: [a@t.com]

	// GROUP BY + HAVING：语义是「是否存在满足条件的分组」
	exists, err = db.Builder().Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		Exists(ctx)
	// SQL: SELECT 1 FROM (SELECT 1 FROM `orders` GROUP BY `user_id` HAVING SUM(amount) > ?) AS `t` LIMIT 1
	_ = exists
	_ = err
}
```

**边界与错误**：

- 无匹配返回 `(false, nil)`。
- 行集迭代过程中出错（连接中断、ctx 取消）返回 `(false, err)`，不会把「出错」当成 `false`。
- 与 `Count` 一样忽略分页：`Limit(1)` 不影响判断结果。

## Max / Min / Sum / Avg

```text
func (b *Builder) Max(ctx context.Context, column string) (float64, error)
func (b *Builder) Min(ctx context.Context, column string) (float64, error)
func (b *Builder) Sum(ctx context.Context, column string) (float64, error)
func (b *Builder) Avg(ctx context.Context, column string) (float64, error)
```

**语义**：编译 `ToAggregate(fn, column)`，生成 `SELECT AGG(列) AS aggregate FROM ...`，把单行结果扫描进 `sql.NullFloat64` 后归一。四个方法共享同一实现，区别只在聚合函数名。

**dest 要求**：无 `dest`，四个方法统一返回 `float64`——整型列的和、平均、最大最小也都经浮点归一，不会回传整型。

**SQL/结果语义**：

- 聚合表达式编译为 `AGG(列) AS aggregate`（列与别名按方言包裹），别名固定为 `aggregate`。
- 调用方的 SELECT 列、`SelectSub`、`Limit`/`Offset`、`OrderBy`、锁子句会被**临时清除或替换**，编译后恢复：聚合作用于 WHERE 过滤后的**整个结果集**，不受分页影响。
- 含 UNION 时把整个 UNION 包裹为子查询再聚合。
- 含 GROUP BY 或 HAVING 时**不执行 SQL**，直接返回 `ErrAggregateGrouped`——分组会产生多行，与标量返回值冲突；需要每组一行时用 `Select` + `GroupBy` + `Find`。

空集语义（SQL 聚合对空集返回 NULL 的归一化）：

| 方法 | 空表 / 无匹配行 |
|---|---|
| `Max` / `Min` | 返回 `(0, sql.ErrNoRows)`，对齐 `First` 未命中的语义 |
| `Sum` / `Avg` | 返回 `(0, nil)`，NULL 归一为 0 |

```go
func example() {
	maxAge, err := db.Builder().Table("users").Max(ctx, "age")
	// SQL: SELECT MAX(`age`) AS `aggregate` FROM `users`

	total, err := db.Builder().Table("orders").Where("status", "=", "paid").Sum(ctx, "amount")
	// SQL:  SELECT SUM(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
	// args: [paid]
	_ = maxAge
	_ = total
	_ = err

	// 分组语义不能用标量聚合：直接返回 ErrAggregateGrouped，不发起查询
	_, err = db.Builder().Table("orders").GroupBy("user_id").Max(ctx, "amount")
	if errors.Is(err, zcdb.ErrAggregateGrouped) {
		slog.Default().Warn("改用 Select + GroupBy + Find 接收多组结果")
	}
}
```

**边界与错误**：

- 结果列无法扫描为 `float64` 时报扫描错误（例如 `MAX(name)` 返回文本）。
- SQL 聚合函数本身忽略 NULL 行，`Sum`/`Avg` 在全部为 NULL 时得到 NULL，归一为 0。
- `ErrAggregateGrouped` 的判定发生在编译期，不会先查一次库再报错。
- 内部 API `ToAggregate(fn, column)` 对非法函数名返回 `ErrInvalidAggregate`（四个公开方法不会触发）。

## Pluck

```text
func (b *Builder) Pluck(ctx context.Context, dest any, column string) error
```

**语义**：把一个列的值提取成切片。方法名与目标容器一一对应：切片用 `Pluck`，标量 map 用 `PluckMap`，结构体 map 用 `KeyBy`。

**dest 要求**：非 nil 的切片指针；底层切片本身可以为 nil，元素类型由 NULL 安全扫描支持（基本类型、指针、命名类型等）。非指针、nil 指针、指向非切片（结构体、map）都返回 `ErrPluckDest`。

**SQL/结果语义**：先 `Select(column)` **覆盖** Builder 的 SELECT 列（替换语义，且改的是接收者本身），再以单列编译执行；WHERE/JOIN/ORDER BY/LIMIT/UNION/锁全部保留。结果值按 SQL 返回顺序**追加**到 `dest` 现有元素之后，不清空。

```go
func example() {
	var names []string
	err := db.Builder().Table("users").Where("vip", "=", 1).Pluck(ctx, &names, "name")
	// SQL:  SELECT `name` FROM `users` WHERE `vip` = ?
	// args: [1]

	// 列序即结果顺序；NULL 扫描为零值
	var ids []int64
	err = db.Builder().Table("users").OrderBy("id", "DESC").Pluck(ctx, &ids, "id")
	// SQL: SELECT `id` FROM `users` ORDER BY `id` DESC
	_ = err
}
```

**边界与错误**：

- 同一切片上多次调用会累积（append 语义），需要「只保留本次结果」时先 `names = nil`。
- NULL 值扫描为零值（`""`、`0`、指针字段为 nil）。
- 扫描失败返回 `zcdb: pluck scan row failed: %w`，错误链中保留 `ErrScanConvert`。
- 覆盖 SELECT 列会改变接收者状态：之后用同一 Builder 再 `Find` 只会查这一列，需要复用时先 `Clone()`（详见 [builder-state](../concepts/builder-state.md)）。

## PluckMap

```text
func (b *Builder) PluckMap(ctx context.Context, dest any, keyColumn, valueColumn string) error
```

**语义**：把两列提取成 `map[K]V`：`keyColumn` 作键、`valueColumn` 作值，参数顺序与 `map[K]V` 声明顺序一致，也与 SQL 列序、`Scan` 目标顺序一致。

**dest 要求**：非 nil 的 map 指针；**键与值都不能是结构体或结构体指针**（值类型为指针时先解引用再判断），否则返回 `ErrPluckMapDest`。结构体值请改用 `KeyBy`。`map` 本身为 nil 时由方法初始化。

**SQL/结果语义**：`Select(keyColumn, valueColumn)` 覆盖既有 SELECT，编译为键列在前的两列查询。

```go
func example() {
	var m map[int64]string
	err := db.Builder().Table("users").PluckMap(ctx, &m, "id", "name") // id => name
	// SQL: SELECT `id`, `name` FROM `users`

	var pairs map[string]int64
	err = db.Builder().Table("users").OrderBy("id", "ASC").PluckMap(ctx, &pairs, "status", "id")
	// SQL: SELECT `status`, `id` FROM `users` ORDER BY `id` ASC
	_ = err
}
```

**边界与错误**：

- 参数顺序就是「键列、值列」的位置：写反了不会报错，只会把两列的类型互换或错位，API 不校验列名与 map 类型的匹配。
- 重复键后者覆盖前者（按结果集顺序，最后一行生效）；键列为 NULL 时使用零值键，值列为 NULL 时使用零值。
- 空结果得到已初始化的空 map（非 nil），可直接 `len()` 判断。
- 覆盖 SELECT 列同样作用于接收者，复用前先 `Clone()`。

## KeyBy

```text
func (b *Builder) KeyBy(ctx context.Context, dest any, keyColumn string) error
```

**语义**：以 `keyColumn` 为键，把整行按列映射标签扫描进结构体，构建 `map[K]struct` 或 `map[K]*struct`。它是「按唯一列建索引」的常用写法。

**dest 要求**：非 nil 的 map 指针；值类型必须是结构体或结构体指针，**键不能是结构体或结构体指针**，否则返回 `ErrKeyByDest`。结构体没有任何可用导出字段时返回 `ErrNoFields`。map 为 nil 时由方法初始化。

**SQL/结果语义**：SELECT 列 = 结构体字段列（按字段声明与展开顺序）+ 键列。键列已在结构体字段中时不重复 SELECT，直接复用该字段扫描出的值；不在字段中时追加为 SELECT 的最后一列单独扫描。

```go
func example() {
	var byID map[int64]User
	err := db.Builder().Table("users").KeyBy(ctx, &byID, "id") // id => User 整行
	// SQL: SELECT `id`, `name`, `age` FROM `users`   // 键列已在字段中，不重复

	type brief struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var byName map[string]brief
	err = db.Builder().Table("users").KeyBy(ctx, &byName, "email") // 键列不在字段中，追加到末尾
	// SQL: SELECT `name`, `age`, `email` FROM `users`
	_ = err
}
```

**边界与错误**：

- 重复键后者覆盖前者（最后一行生效）；键列或值列出现 NULL 时使用零值。
- 键列在结构体字段中、但字段类型不能转换为 map 键类型时，键保持零值——所有行会折叠到同一个零值键上且不报错，请让两者类型兼容。
- 键列不在字段中时，该列只用于取键，不写入结构体。
- 覆盖 SELECT 列（列完全由结构体字段决定，调用方既有的 `Select` 被丢弃），复用前先 `Clone()`。
- 空结果得到已初始化的空 map。

## Paginate

```text
func (b *Builder) Paginate(ctx context.Context, dest any) (int, error)
```

**语义**：分页查询并自动统计总数，返回 `(totalCount, err)`。流程是两次查询：

1. 复用 `Count`（内部 `ToCount`）统计过滤后的总数；
2. `totalCount == 0` 时直接返回 `(0, nil)`，**不执行数据查询**；否则用 `ToSelect` 执行带分页的数据查询并扫描进 `dest`。

**dest 要求**：与 `Find` 完全一致（`*[]struct` / `*[]*struct`）。

**SQL/结果语义**：

```go
func example() {
	var users []User
	total, err := db.Builder().Table("users").
		Where("status", "=", "active").
		OrderBy("id", "ASC").
		ForPage(2, 20). // 第 2 页、每页 20 条
		Paginate(ctx, &users)
	// COUNT SQL: SELECT COUNT(*) FROM `users` WHERE `status` = ?                    args: [active]
	// 数据 SQL:  SELECT * FROM `users` WHERE `status` = ? ORDER BY `id` ASC LIMIT 20 OFFSET 20
	_ = total
	_ = err
}
```

- COUNT 与数据 SQL 是两次独立执行：COUNT 忽略分页与排序，数据 SQL 保留分页与排序。
- 临时状态：`Paginate` **不克隆** Builder，直接调用 `Count` 再 `ToSelect`；`ToCount` 内部保存并 `defer` 恢复 `limit`/`offset`/`orders`/`lockClause`/`columns`/`selectSubs`，因此 `Paginate` 返回后原 Builder 的分页、排序、列都还在，可以继续链式使用。
- 注释与回调：设置 DAO 默认注释或 `Builder.Comment` 后，COUNT 与数据 SQL **各在最终编译出口追加一次**，不是整个 `Paginate` 只追加一次；执行层不再次追加，慢 SQL 回调分别收到两条实际执行的 SQL 及其原绑定参数。`totalCount == 0` 时只观测到带注释的 COUNT SQL。
- 分页范围必须预先通过 `ForPage` 或 `Limit` + `Offset` 设置；未设置时分页参数为 0，数据 SQL 不输出 `LIMIT`/`OFFSET`（全量返回），`Paginate` 不会补默认页长。

**边界与错误**：

- COUNT 阶段失败返回 `(0, err)`，不会执行数据查询。
- COUNT 成功后数据查询或扫描失败返回 `(total, err)`——总数已确定并随错误一起返回。
- `dest` 非法只在扫描阶段暴露：`total > 0` 时 COUNT 与数据查询都已执行；`total == 0` 时提前返回，`dest` 根本不参与校验（非法 dest 也可能拿到 `(0, nil)`）。
- 总数是 WHERE 过滤后的总行数，不是当前页行数；含 UNION/GROUP/HAVING/DISTINCT 时沿用 `Count` 的包裹规则。

## 相关阅读

- 结构体映射、NULL 与类型转换：[scanner](scanner.md)、[mapping](../concepts/mapping.md)
- 终端方法的克隆与临时状态策略：[builder-state](../concepts/builder-state.md)
- 编译顺序与参数绑定：[compile-pipeline](../concepts/compile-pipeline.md)、[builder-compile](builder-compile.md)
- 连接选择与读写分离：[execution-routing](../concepts/execution-routing.md)、[builder-exec](builder-exec.md)
- 流式迭代：[builder-cursor](builder-cursor.md)
- 分页任务写法与边界：[pagination](../how-to/pagination.md)
- 方法总览与选型：[terminal-methods](terminal-methods.md)
