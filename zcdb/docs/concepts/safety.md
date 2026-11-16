# 安全边界：保护、逃逸口与责任划分

zcdb 的安全设计遵循一个取向：**框架只拦「静默扩大破坏范围」的形态，不替调用方兜底**。这份文档把边界讲清楚：哪些写法会被拒绝、哪些是刻意保留的逃逸口、哪些责任完全在调用方。

## 无 WHERE 的破坏性操作保护

`Update`、`Increment`、`Decrement`、`Delete`、`DeleteJoin` 在执行前判断「是否存在有效限定条件」，没有则返回哨兵错误：

| 操作 | 拒绝原因 | 错误 |
|---|---|---|
| `Update` / `Increment` / `Decrement` | 无有效 WHERE/JOIN | `ErrUpdateWithoutWhere` |
| `Delete` | 无有效 WHERE（JOIN 不参与） | `ErrDeleteWithoutWhere` |
| `DeleteJoin` | 无有效 WHERE/JOIN | `ErrDeleteWithoutWhere` |
| `Delete` | Builder 带 JOIN（无论是否有 WHERE/Force） | `ErrDeleteWithJoin` |

### 有效限定怎么判定

WHERE 条件按 SQL 优先级（AND 先于 OR）被归约为四种真值：

| 真值 | 含义 | 是否算有效限定 |
|---|---|---|
| 恒真 | 任何行都满足，如 `1 = 1`、空 `WhereNotIn` | 否 |
| 恒假 | 任何行都不满足，如 `0 = 1`、空 `WhereIn` | 是（放行但不影响行） |
| 未知 | 无法静态判定，视为逐行计算 | 是 |
| 不存在 | 编译后不产生条件（空嵌套组、`WhereRaw("")`） | 不计入 |

具体形态：

| 条件形态 | 归约结果 |
|---|---|
| `Where("id", "=", 1)` | 未知 → 有效 |
| 空嵌套回调 `WhereNested(func(q){})` | 不存在 → 不计入 |
| `WhereRaw("")` | 不存在 → 不计入 |
| `WhereRaw("   ")`（纯空白） | 保护判定按不存在 → 不计入；但编译仍会输出空白条件（`WHERE    `，SQL 非法），不要依赖它被跳过 |
| `WhereRaw("1 = 1")` | 未知 → **有效**（刻意保留的逃生口） |
| `WhereIn("id", []any{})` | 恒假（编译为 `0 = 1`）→ 有效 |
| `WhereNotIn("id", []any{})` | 恒真（编译为 `1 = 1`）→ 无效 |
| `WhereExists(...)` 内层无有效条件 | 恒真 → 无效 |

**EXISTS 的判定是句法的**：只检查内层是否含「未知/恒假」条件，**不分析子查询是否引用外层列**。因此非相关子查询也会被计为有效限定：

```go
func example() {
	// 危险：子查询与 users 无关，EXISTS 恒为真，但没有外层 WHERE，
	// 保护判定仍会放行，更新影响全部行。
	db.Builder().Table("users").
		WhereExists(func(q *zcdb.Builder) { q.Table("config").Where("id", "=", 1) }).
		Update(ctx, User{Status: "archived"})
}
```

需要真正的行级收窄时，显式补外层条件或改用关联子查询；确实要全表操作时用 `Force()`。

### JOIN 何时算限定

- `Update`/`Increment`/`Decrement`/`DeleteJoin` 会真实编译 JOIN，带 ON 条件的 JOIN 计入有效限定；无条件 JOIN（笛卡尔积）不计入。
- `Delete` 不编译 JOIN：只要 Builder 带 JOIN 就返回 `ErrDeleteWithJoin`，`Force()` 也不放行。按关联删除请用 `DeleteJoin` 或 `WhereInSub`。
- JOIN 的 ON 条件里，空/纯空白 Raw 不产生条件，不计入；嵌套 join 组中的有效 ON 条件同样计入。

### Force

`Force()` 仅跳过「无有效 WHERE」的保护判定，不改变 SQL、不跳过 JOIN 检查。推荐在代码审查/检索时把它当作显式的全表操作标记。

## 写语句的 ORDER BY / LIMIT

写语句携带 `OrderBy`/`Limit` 时，支持范围很窄，超出会在编译期返回 `ErrWriteOrderLimitUnsupported`：

| 方言 / 形态 | 行为 |
|---|---|
| MySQL 单表 UPDATE/DELETE/Increment/Decrement | 支持 `ORDER BY ... LIMIT n`（按序只改/删前 n 行；省略 ORDER BY 时由引擎选行，不确定） |
| MySQL 多表 UPDATE（带 JOIN） | 拒绝（引擎也会报 1221，框架提前拦截） |
| MySQL DeleteJoin | 拒绝 |
| PostgreSQL / SQLite 的 UPDATE/DELETE/DeleteJoin/Increment/Decrement | 拒绝（方言没有该语法，忽略会静默放大范围） |
| Insert 族、Truncate | 不读取这两个状态，设置后静默忽略 |

`ORDER BY`/`LIMIT` 不构成有效 WHERE，不能替代保护；`Offset` 不进入写语句。

## Expression 逃逸口

`zcdb.NewExpression("...")` 把原始 SQL 片段原样拼进产物，是类型安全 API 的逃生口。它的边界：

- **内嵌位置**（不占绑定）：`Where`/`OrWhere` 的值比较、`WhereIn`/`WhereNotIn` 的元素、`WhereLike`/`WhereNotLike`、NULL 安全比较、`Having` 基本比较、Insert/Update/Upsert 字段值、`WhereRaw` 等的绑定位置、`CompileRaw` 的 args；
- **不内嵌、会被当普通参数**：`Between`/`NotBetween`/`HavingBetween`/`HavingNotBetween` 的 min/max，`JoinBuilder.WhereIn`/`WhereNotIn` 的列表元素——传入 Expression 会在执行期被驱动拒绝（`unsupported type zcdb.Expression`）；
- **占位符**：Expression 内容里的 `?` 没有配对参数。在多数位置它原样进入 SQL（PostgreSQL 下成为非法占位符）；在 PG 的 UPDATE SET 值、JOIN ON 值比较里会被转成 `$N` 并占用占位符编号。不要在 Expression 里写 `?`。

Expression 不做标识符包裹、不做转义、不做参数化：**绝不要把用户输入拼进 Expression**。

## 原始 SQL 与注释

- `db.Exec`/`Query`/`QueryPrimary` 原文透传：不追加 DAO 注释、不剥离手写注释、不转换占位符（PostgreSQL 需自己写 `$N`）。
- DAO 默认注释与 `Builder.Comment` 只用于短业务标识：把 `*`、`/`、`\` 与全部 `unicode.IsControl` 控制字符替换为空格、非法 UTF-8 转 U+FFFD、去首尾空白、255 rune 截断。这是**有损清洗**，不是脱敏：不得透传外部输入、请求体、密码、令牌。
- 注释不是有效 WHERE/JOIN 条件，不能替代 `Force()`。

## 责任划分

| 事项 | 责任方 |
|---|---|
| 标识符/列名/表名合法性 | 调用方（zcdb 只做包裹，不做白名单） |
| Expression 内容、Raw SQL 内容 | 调用方（注入责任） |
| 注释内容是否含敏感信息 | 调用方（zcdb 只做字符清洗） |
| DSN、驱动、连接池参数 | 调用方 |
| 条件是否真正收窄范围（尤其 EXISTS） | 调用方判断语义，zcdb 只做静态归约 |
| 事务边界与 ctx 传递 | 调用方 |

## 相关阅读

- 保护判定的实现位置：[builder-state](builder-state.md)、[reference/builder-exec](../reference/builder-exec.md)
- 编译期校验顺序：[compile-pipeline](compile-pipeline.md)
- 错误全表：[reference/errors](../reference/errors.md)
