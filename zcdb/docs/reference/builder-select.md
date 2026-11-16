# SELECT 列与 DISTINCT

SELECT 的投影由两类状态组成：`columns`（普通列与 `SelectRaw` 表达式，按调用顺序）与 `selectSubs`（`SelectSub` 子查询列，按添加顺序）。编译时**先输出全部 `columns`，再输出全部 `selectSubs`**，所以子查询列永远排在同一 SELECT 的最后，与 `Select`/`SelectSub` 的调用先后无关。没有列、也没有子查询列时输出 `SELECT *`。

本文覆盖 `Select`、`AddSelect`、`SelectRaw`、`SelectSub`、`Distinct`，以及它们共用的列引用规则。列状态在 `Clone`、终端方法中的复制/覆盖行为见 [builder-state](../concepts/builder-state.md)；绑定参数整体顺序见 [compile-pipeline](../concepts/compile-pipeline.md)。

示例统一为 MySQL 形态（反引号、`?` 占位符），假定 DAO 第五参数为 `""`（无注释）。`db` 是外部占位变量，示例只保证语法可解析。

## 先读结论

1. `Select` 是**替换**：`Select("a").Select("b")` 只剩 `b`；无参 `Select()` 清空普通列，无子查询列时回到 `SELECT *`。
2. `AddSelect` 是**追加**且对非 Raw 列做全等去重；`SelectRaw`/`SelectSub` 一律追加，不去重。
3. `SelectRaw` 的内容原样进入 SQL（不做标识符包裹）；`SelectSub` 编译为 `(子查询) AS 别名`。
4. 只有 `SelectSub` 会引入绑定，且位置在所有其它绑定之前（`SELECT_SUB → FROM_SUB → JOIN → WHERE → …`）。
5. 所有列名最终都过 `Grammar.WrapColumn`：`*` 不包裹，`表.列` 拆分包裹，`列 AS 别名` 识别别名，含 `(` 的字符串按原始表达式输出。

## Select

```text
func (b *Builder) Select(columns ...string) *Builder
```

**语义**：用给定列名**替换**当前普通列清单（`SelectRaw` 追加的列也在其中，会被一并覆盖）。不调用 `Select`/`AddSelect`/`SelectRaw` 时投影为 `*`。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").Select("id", "name").ToSelect()
	// SQL: SELECT `id`, `name` FROM `users`

	sqlStr, _, _ = db.Builder().Table("users").Select("id").Select("name").ToSelect()
	// SQL: SELECT `name` FROM `users`   // 替换语义：只剩最后一次的列

	sqlStr, _, _ = db.Builder().Table("users").Select().ToSelect()
	// SQL: SELECT * FROM `users`        // 无列且无子查询列，回到默认投影

	sqlStr, _, _ = db.Builder().Table("users").Select("users.*").ToSelect()
	// SQL: SELECT `users`.* FROM `users`   // 星号不包裹，表名部分照常包裹
	_, _ = sqlStr, db
}
```

**边界与错误**：

- 列名统一经 `Grammar.WrapColumn`：含 `(` 的字符串视为原始表达式、不包裹（`Select("COUNT(*)")` 会原样输出）；列名不是绑定参数，不要把外部输入拼进列名，值应放在 WHERE/HAVING 等条件里。
- `Select()` 无参时只清空普通列，不清空 `SelectSub` 已添加的子查询列：`Select("id").SelectSub(sub, "c").Select()` 编译为 `SELECT (子查询) AS c …`。
- 空字符串列名会编译成空标识符（如 MySQL 的空反引号标识符），不报错；列名不做存在性校验，错误由数据库在执行期给出。
- 该方法不引入绑定参数，也不改变 `Distinct`、表、WHERE 等状态。

**与其他方法的区别**：`AddSelect` 是追加且去重，`SelectRaw` 是不包裹的原始表达式，`SelectSub` 是子查询列。终端方法 `Pluck`/`PluckMap`/`KeyBy` 会**覆盖** `columns`，复用被它们执行过的 Builder 前请 `Clone`，见 [terminal-methods](../reference/terminal-methods.md)。

## AddSelect

```text
func (b *Builder) AddSelect(columns ...string) *Builder
```

**语义**：向当前普通列清单**追加**列，追加前对每个列做去重：已存在**非 Raw** 且 `Value` 完全相等的列时跳过。不要求先调用 `Select`；没有既有列时等价于建立列清单（不再是 `SELECT *`）。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").Select("id").AddSelect("name", "id").ToSelect()
	// SQL: SELECT `id`, `name` FROM `users`   // id 已存在，不重复添加

	sqlStr, _, _ = db.Builder().Table("users").AddSelect("id").ToSelect()
	// SQL: SELECT `id` FROM `users`

	sqlStr, _, _ = db.Builder().Table("users").SelectRaw("name").AddSelect("name").ToSelect()
	// SQL: SELECT name, `name` FROM `users`   // Raw 列不参与去重，两者都保留
	_, _ = sqlStr, db
}
```

**边界与错误**：

- 去重只比较非 Raw 列的 `Value` 字符串（全等、区分大小写）：`"users.id"` 与 `"id"` 视为不同列，会同时保留。
- 同一次调用内的重复参数也会按顺序去重（第一次加入后，后续相同值被跳过）。
- `AddSelect()` 无参是空操作；调用 `Select` 之后再用 `AddSelect` 会追加到 `Select` 重置后的清单上。
- 与 `Select` 一样不做列名校验、不引入绑定。

**与其他方法的区别**：`Select` 替换、`AddSelect` 追加去重；`SelectRaw` 追加原始表达式且**不参与**去重（即使文本相同）。

## SelectRaw

```text
func (b *Builder) SelectRaw(expression string) *Builder
```

**语义**：追加一个原始 SQL 表达式作为列，内容原样写入 SQL，不经过 `WrapColumn`，适用于聚合函数、窗口函数、`CASE` 等无法用普通列名表达的场景。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").ToSelect()
	// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders`

	sqlStr, _, _ = db.Builder().Table("users").Select("id").
		SelectRaw("COUNT(*) OVER() AS total").ToSelect()
	// SQL: SELECT `id`, COUNT(*) OVER() AS total FROM `users`
	_, _ = sqlStr, db
}
```

**边界与错误**：

- 表达式原样内嵌，**安全责任归调用方**：绝不能拼接用户输入、请求体或密钥。
- 不支持绑定参数：`expression` 里的 `?` 不会被扫描或绑定，会作为无参占位符留给驱动，执行期报错（`SelectRaw` 不是 `WhereRaw`，没有占位符处理）。
- 追加顺序固定在普通列之后、`SelectSub` 列之前；不参与 `AddSelect` 的去重。
- 不改变表、WHERE、`Distinct` 等状态；`Select` 调用会把它一起覆盖掉。

**与其他方法的区别**：`SelectRaw` 是「一段文本列」；`SelectSub` 是「一个 Builder 子查询列」，后者会引入绑定并加别名。`Select("COUNT(*)")` 虽然因含 `(` 也会原样输出，但语义上属于普通列路径，无法表达别名与后续追加关系，推荐聚合表达式用 `SelectRaw`。

## SelectSub

```text
func (b *Builder) SelectSub(sub *Builder, alias string) *Builder
```

**语义**：追加一个子查询列，编译为 `(子查询) AS 别名`。子查询的绑定参数收集在 `SELECT_SUB` 阶段，位置在所有其它绑定（FROM 子查询、JOIN、WHERE…）之前。列顺序上，所有 `SelectSub` 列都排在同一 SELECT 的普通列/Raw 列之后。

**SQL**：

```go
func example() {
	cnt := db.Builder().Table("orders").SelectRaw("COUNT(*)").
		WhereColumn("orders.user_id", "=", "users.id")
	sqlStr, _, _ := db.Builder().Table("users").Select("id").
		SelectSub(cnt, "order_count").ToSelect()
	// SQL: SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `orders`.`user_id` = `users`.`id`) AS `order_count` FROM `users`

	// 子查询列的绑定参数最先计入
	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
	sqlStr, args, _ := db.Builder().Table("users").
		SelectSub(sub, "order_count").Where("status", "active").ToSelect()
	// SQL:  SELECT (SELECT COUNT(*) FROM `orders` WHERE `amount` > ?) AS `order_count` FROM `users` WHERE `status` = ?
	// args: [100 active]
	_, _ = sqlStr, args
}
```

**边界与错误**：

- `sub` 必须自己设置 `Table`/`TableSub`。外层不会替子查询检查 `ErrEmptyTable`；缺表时编译产物形如 `(SELECT * FROM <空标识符>) AS <别名>`，由数据库执行期拒绝。
- `alias` 是标识符：为空会编译出空别名；请显式给出。
- 子查询必须是标量形态（单列、至多一行）；多列/多行由数据库在执行期报错，编译层不做检查。
- 子 Builder 的累积错误、子查询图环会经外层的环检测上抛（`ErrInvalidOperator`/`ErrCyclicQuery`）；子查询自身的 `Comment` 不会进入外层 SQL。
- 子查询列不参与 `AddSelect` 去重，也不被 `Select`（无参）清空。

**与其他方法的区别**：`SelectSub` 是 SELECT 投影里的标量子查询；`TableSub` 是 FROM 派生表；`JoinSub` 是 JOIN 右表；三者绑定位置不同，见 [builder-core](../reference/builder-core.md) 与 [builder-join](../reference/builder-join.md)。

## Distinct

```text
func (b *Builder) Distinct() *Builder
```

**语义**：置位去重标记，编译为 `SELECT DISTINCT …`。该标记是布尔状态，重复调用幂等，没有对应的「取消去重」方法（要取消只能新建 Builder 或不调用它）。

**SQL**：

```go
func example() {
	sqlStr, _, _ := db.Builder().Table("users").Select("city").Distinct().ToSelect()
	// SQL: SELECT DISTINCT `city` FROM `users`

	// DISTINCT 下 Count 会包裹为子查询再计数，保持去重语义
	sqlStr, _, _ = db.Builder().Table("users").Select("city").Distinct().ToCount()
	// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
	_, _ = sqlStr, db
}
```

**边界与错误**：

- `DISTINCT` 作用于整个投影（普通列、Raw 列与子查询列共同决定一行是否重复），不是只作用于第一列；需要按部分列去重请用 `GroupBy`。
- `Distinct()` 不改变 WHERE/JOIN/ORDER 等状态，也不引入绑定。
- 只有 `ToCount` 会为保留去重语义而包裹子查询；`Find`/`Cursor` 等按原样执行 `SELECT DISTINCT`，返回行数仍受 `Limit` 影响。
- 本库不提供 `DISTINCT ON`（PostgreSQL 特性）；需要时用 `SelectRaw`/`WhereRaw` 自行表达。

**与其他方法的区别**：`Distinct` 去重；`GroupBy` 分组并允许聚合；两者都会影响 `Count` 的编译形态（分组查询同样包裹子查询计数），但语义不同。

## 列引用规则（WrapColumn）

`Select`/`AddSelect` 的列、`WhereColumn` 的两侧、`OrderBy`/`GroupBy` 的列名都走同一套 `Grammar.WrapColumn`。规则可用下表概括，方言差异的完整矩阵见 [dialect-matrix](../reference/dialect-matrix.md)：

| 输入形态 | 输出 | 说明 |
|---|---|---|
| `*` | `*` | 不包裹 |
| `users.*` | `` `users`.* `` / `"users".*` | 表名部分包裹，星号不包裹 |
| `users.id` | `` `users`.`id` `` / `"users"."id"` | 按第一个 `.` 拆成表名与列名 |
| `name AS n` | `` `name` AS `n` `` / `"name" AS "n"` | 大小写不敏感地识别 ` AS `，别名单独包裹 |
| `COUNT(*)` | `COUNT(*)` | 含 `(` 视为原始表达式，整体不包裹 |
| 含方言引号字符 | 引号加倍转义 | MySQL 反引号、PG/SQLite 双引号 |

**边界**：这条规则是「便捷 + 逃逸」的混合体——含 `(` 的输入按原始表达式处理，意味着 `Select("f(x)")` 不会报错但也不会校验；不要把外部输入直接作为列名传入。表别名建议用 `Table("users AS u")` 或 `TableSub(sub, "u")` 的别名参数表达，而不是手工拼进列名。

## 绑定顺序与终端方法的影响

SELECT 类查询的绑定顺序固定为：

```text
SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION
```

`SelectSub` 的绑定最先，是因为它出现在 SQL 文本最前面的投影里；`Distinct` 本身不产生绑定。完整规则与 UPDATE/DELETE 的差异见 [compile-pipeline](../concepts/compile-pipeline.md)。

计数/存在性/聚合类终端方法会临时改写投影，编译后由 `saveTransientState` 恢复，不改调用方 Builder 的状态；`Pluck` 系列是例外（见下）。

- `ToCount`：替换为 `COUNT(*)`；`Distinct`、`GroupBy`/`HAVING`、`UNION` 分支改为包裹子查询计数。
- `ToExists`：普通查询替换为常量 `1`，`UNION`/`HAVING` 查询保留原投影后整体包裹。
- `ToAggregate`：替换为 `AGG(列) AS aggregate`；有分组时返回 `ErrAggregateGrouped`。
- `Pluck`/`PluckMap`/`KeyBy`：**覆盖** `columns`，且不恢复；复用前先 `Clone`。

细节见 [builder-exec](../reference/builder-exec.md) 与 [terminal-methods](../reference/terminal-methods.md)。

## 相关阅读

- 投影状态与替换/追加语义：[builder-state](../concepts/builder-state.md)
- 编译顺序与绑定收集：[compile-pipeline](../concepts/compile-pipeline.md)
- WHERE 条件（含 `WhereColumn` 的列引用）：[builder-where](../reference/builder-where.md)
- 数据源与 TableSub：[builder-core](../reference/builder-core.md)
- 方言包裹符/占位符差异：[dialect-matrix](../reference/dialect-matrix.md)
