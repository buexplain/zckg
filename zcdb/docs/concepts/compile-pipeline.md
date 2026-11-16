# 编译管线与绑定顺序

`ToXxx` 系列是 zcdb 的纯编译入口：输入 Builder 状态，输出 `(sql, args, err)`，不接触数据库。理解编译顺序与参数收集顺序，才能解释「SQL 里占位符的位置」和「args 为什么这样排」。

## 两个阶段：校验与生成

每个编译入口都按固定顺序执行：

1. **错误短路**：`b.err != nil` 直接返回（构造期累积的错误）。
2. **数据源校验**：未设置 `Table`/`TableSub` 返回 `ErrEmptyTable`。
3. **环检测**：`validateAcyclic` 遍历 FROM/JOIN/WHERE/HAVING/UNION 子查询图，有环返回 `ErrCyclicQuery`。
4. **形态校验**：例如 PG 的 `UNION + 锁`、SQLite 的锁子句、写语句携带 ORDER BY/LIMIT。
5. **生成**：调用 `Grammar` 对应方法得到 SQL 文本；再单独收集绑定参数。
6. **注释追加**：`appendSQLComment` 在最终 SQL 末尾追加一次 DAO 默认注释或 `Builder.Comment` 的值（空则不追加）。

第 5 步的「生成 SQL」和「收集 args」是两条独立的遍历，但遍历顺序必须一致——这就是下面的绑定顺序规则。

## Grammar 的职责边界

`Grammar` 接口只做方言相关的事：

| 职责 | 例子 |
|---|---|
| 标识符包裹 | MySQL 反引号，PG/SQLite 双引号 |
| 占位符形态 | MySQL/SQLite `?`，PostgreSQL `$1`/`$2`… |
| 语句结构差异 | `TRUNCATE`、`ON DUPLICATE KEY`/`ON CONFLICT`、`DELETE ... USING`、UNION 括号 |
| 表达式差异 | `RAND()`、`date()`/`::date`/`strftime`、`BINARY LIKE`/`ILIKE`/`GLOB` |
| 写语句子句支持 | UPDATE 的 ORDER BY/LIMIT 只 MySQL 单表支持 |

Grammar 不负责：选连接、事务、扫描、注释（注释在 Builder 层追加）。

### PostgreSQL 的占位符计数器

`PostgresGrammar` 有一个 `paramCount` 计数器。为了避免并发编译互相干扰，所有 `Compile*` 入口都在入口处 `cloneForCompile()` 一个零值副本，计数器只在副本上累加，然后丢弃。`CompileRaw` 不经过计数器（它把 `$N` 内联成字面量）。因此 `db.Grammar()` 返回的共享实例没有可见的可变状态，可以并发调用。

## 绑定参数顺序

参数顺序按 **SQL 中占位符出现的逻辑顺序** 收集，而不是按调用链的顺序：

### SELECT 类

```text
SELECT_SUB → FROM_SUB → JOIN → WHERE → GROUP BY → HAVING → UNION
```

- `SelectSub` 产生的子查询绑定最先；
- `TableSub` 的 FROM 子查询次之；
- JOIN 的 ON 条件绑定在 WHERE 之前；
- UNION 的后续分支绑定在最后。

### UPDATE 类（方言不同）

| 方言 | 顺序 |
|---|---|
| MySQL | JOIN → SET → WHERE |
| PostgreSQL / SQLite | SET → JOIN → WHERE |

差异来自 SQL 形态：MySQL 的多表 UPDATE 把 JOIN 写在 SET 之前，PG/SQLite 用 `FROM` 表达 JOIN，SET 在前。`Increment`/`Decrement` 复用同一编译路径，顺序相同。

### 写语句的其余入口

`Insert`/`Upsert`/`InsertUsing` 的绑定 = VALUES 行展开（或子查询绑定）；`Delete`/`DeleteJoin`/`Truncate` 的绑定来自 WHERE/JOIN/USING 子查询。`Expression` 类型在支持内嵌的位置直接写入 SQL 文本，**不占绑定位置**；不支持的位置（Between/HavingBetween 的 min/max、JoinBuilder 的 IN 列表）会当作普通参数绑定，执行期由驱动拒绝——完整边界见 [reference/builder-compile.md](../reference/builder-compile.md)。

## 注释追加点

注释只在 `ToXxx` 的最后一步追加一次：

- 有 DAO 默认注释时，新 Builder 复制一份；`Comment("")` 清空且不回退默认值。
- 结构化子查询（`TableSub`/`JoinSub`/`WhereExists` 的回调）在内部编译时**不会**追加注释，只有最外层产物带一次。
- 直接调用 `Grammar.CompileSelect` 或 DAO 原始 `Exec`/`Query` 不会追加注释。
- `ToTruncate` 的 SQLite 辅助语句（`DELETE FROM sqlite_sequence`）由 DAO 直接执行，不带注释。

规范化（替换 `*`/`/`/`\`/控制字符、255 rune 截断）在写入时完成；追加只是拼接。安全边界见 [safety](safety.md)。

## 原始片段与占位符扫描

`WhereRaw`、`JoinBuilder.Raw`、`HavingRaw`、`GroupByRaw`、`CompileRaw` 传入的是「半可信 SQL 片段」。zcdb 用 `sqlScan.go` 的扫描器区分代码区与非代码区：

- 字符串字面量、引号标识符、行注释/块注释、PostgreSQL 美元引用里的 `?`/`$N` 是字面内容，原样保留；
- 代码区里的 `?` 按方言规则处理：MySQL/SQLite 保持 `?`，PostgreSQL 转 `$N`；PG 下 `??` 转义为字面 `?`（jsonb 键存在操作符），不消耗绑定。

已知未覆盖语法（按代码区处理）：SQLite 的 `[...]` 标识符、MySQL 的 `/*! ... */` 可执行注释、PostgreSQL 的 `U&'...'`。在这些位置写 `?` 会与引擎解析不同，绑定可能错位。

## 纯编译怎么用

```go
func example() {
	// 无 DAO 的纯编译：默认无注释
	b := zcdb.NewBuilder(zcdb.NewMySQLGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err := b.ToSelect()
	// SQL: SELECT * FROM `users` WHERE `id` = ?
	// args: [1]

	// 有 DAO 时直接取共享 Grammar（不追加 DAO 默认注释）
	sqlStr, args, err = db.Grammar().CompileRaw("SELECT * FROM users WHERE id = ?", []any{1})
}
```

## 相关阅读

- 状态字段与克隆策略：[builder-state](builder-state.md)
- 方言差异矩阵：[reference/dialect-matrix](../reference/dialect-matrix.md)
- 编译入口逐方法说明：[reference/builder-compile](../reference/builder-compile.md)
- 占位符边界与 Expression：[reference/builder-compile](../reference/builder-compile.md)
