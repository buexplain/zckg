# Builder 的状态模型

Builder 不是「一份不可变的 SQL 描述」，而是一个**持续被链式方法改写的状态机**。理解哪些方法改它、改哪些字段、何时被克隆、编译期有哪些字段会被临时改动，才能解释 zcdb 里大部分「看起来不一致」的行为。

## 状态字段

```text
Builder
├─ 编译依赖：grammar Grammar、dao *DBDao
├─ 查询状态：table、columns、selectSubs、distinct、tableSub、tableAlias
│            joins、wheres、groups、havings、orders、limit、offset
│            unions、lockClause
├─ 执行标记：force（允许全表写）、usePrimary（强制主库读）
├─ 输出标记：comment（短业务标识）
└─ 错误累积：err（构造期错误，编译入口统一短路）
```

字段的归属与复制行为：

| 字段 | 谁写 | `Clone` 是否复制 | 编译期是否临时改动 |
|---|---|---|---|
| `grammar`、`dao` | 构造 | 复制引用（不可变） | 否 |
| `table`、`tableAlias` | `Table`/`TableSub` | 是 | 否 |
| `columns`、`selectSubs` | `Select`/`AddSelect`/`SelectRaw`/`SelectSub` | 深拷贝切片 | `ToCount`/`ToExists`/`ToAggregate` 按分支替换后恢复 |
| `tableSub` | `TableSub` | 深拷贝子 Builder | 否 |
| `joins`、`wheres`、`groups`、`havings`、`unions` | 对应链式方法 | 深拷贝（含嵌套子 Builder 与绑定切片） | 否 |
| `orders`、`limit`、`offset`、`lockClause` | `OrderBy`/`Limit`/`Offset`/锁方法 | 是 | 计数/聚合类编译临时清空后恢复 |
| `distinct` | `Distinct` | 是 | 否 |
| `force`、`usePrimary` | `Force`/`Primary` | 是 | 否（只影响执行层） |
| `comment` | 构造/`Comment` | 是（字符串） | 否 |
| `err` | 校验失败时写入 | 是 | 编译入口先短路 |

`Clone` 对绑定值（`Value`/`Bindings` 里的 `any`）只复制切片结构，不深拷贝其中的切片、map、指针——编译与执行不会修改它们，因此共享底层数据是安全的。

## 链式方法是「改自身」而不是「返回新对象」

所有链式方法（`Where`、`Select`、`OrderBy`、`Join`…）都修改接收者并返回同一个 `*Builder`：

```go
func example() {
	base := db.Builder().Table("users").Where("status", "=", "active")
	admins := base.Where("role", "=", "admin") // 改的是 base，admins 与 base 是同一个对象
	// 需要互不影响时必须显式 Clone：
	admins2 := base.Clone().Where("role", "=", "admin")
}
```

选择可变 + `Clone` 的原因：链式 API 的可读性依赖「一路点下去」，每步返回新对象会让长链产生大量中间分配；把「保留基线」变成显式的 `Clone` 也更符合「调用方决定」的取向。

**推论**：不要在两个不相关的请求之间复用同一个 Builder。`db.Builder()` 每次返回新对象；复用需要 `Clone()`。

## 替换语义与追加语义

同一类状态有两种语义，混用是最常见的误判来源：

| 语义 | 方法 | 行为 |
|---|---|---|
| 替换 | `Select`、`Comment`、`Table`、`Limit`、`Offset`、`ForPage`、`Primary`（置位） | 覆盖前值 |
| 追加 | `AddSelect`、`SelectRaw`、`SelectSub`、`Where`、`Join`、`GroupBy`、`Having`、`OrderBy`、`Union`、`Increment` 的 extra | 累积到切片/末尾 |

`Select` 是替换（`Select("a").Select("b")` 只剩 `b`），`AddSelect` 是追加且按列名去重。终端方法 `Pluck`/`PluckMap`/`KeyBy` 会**覆盖** `columns`（它们只查自己需要的列）。

## 终端方法的克隆与临时状态策略

不同终端方法对原 Builder 的处理方式不同，这是有意的：

| 终端方法 | 是否 Clone | 说明 |
|---|---|---|
| `Find`、`Count`、`Exists`、`Max`/`Min`/`Sum`/`Avg`、`Pluck`/`PluckMap`/`KeyBy` | 否 | 直接编译；`Pluck` 系列会覆盖 `columns`，注意复用 |
| `First`、`Value` | 是（`limit` 已为 1 时复用原对象） | 需要附加 `LIMIT 1`，不能改调用方状态 |
| `Paginate` | 否 | 直接 `Count` + `ToSelect`；`ToCount` 用 `saveTransientState` 临时清除并 `defer` 恢复分页/排序/锁/列 |
| `Cursor` | 否 | 单条 SQL；rows 在迭代中被消费 |
| `CursorBy` | 每批 Clone | 每批要改 `limit` 与 `orders`，必须隔离 |

`saveTransientState` 保存 `limit`/`offset`/`orders`/`lockClause`/`columns`/`selectSubs`，返回恢复函数并由 `defer` 调用。因此 `ToCount`/`ToExists`/`ToAggregate` 编译后原 Builder 状态不变；即使编译中途 panic 也会恢复。

## 错误累积与环检测

- 构造期错误（如 `Where` 的非法运算符、`JoinOn` 的非法子查询入参）写入 `b.err`，链式方法不中断，**编译入口**统一返回该错误。这样链式调用不会被迫在每一行检查 error。
- 子查询图存在环（`TableSub(b)` 指向自己、或 A/B 互相引用）会让编译/克隆无限递归。`ToXxx` 与 `Clone` 先调用 `validateAcyclic`；`Clone` 遇到环时返回携带 `ErrCyclicQuery` 的浅副本，后续编译报错而不是栈溢出。
- 子 Builder 携带错误时，外层编译同样会返回该错误。

## 三个执行标记

- `Force()`：允许无有效 WHERE 的 `Update`/`Increment`/`Decrement`/`Delete`/`DeleteJoin` 执行，仅影响保护判定，不改 SQL。见 [safety](safety.md)。
- `Primary()`：强制读查询走主库（不加锁），仅影响执行层路由，不改 SQL。用于写后读等从库延迟场景。
- `Comment(text)`：设置短业务标识，编译时追加到 SQL 末尾；`nil` 注释表示不追加。规范化规则（替换 `*`/`/`/`\`/控制字符、255 rune 截断）与安全边界见 [safety](safety.md)。

## 常见陷阱

- **复用被 Pluck 改过的 Builder**：`Pluck`/`PluckMap`/`KeyBy` 会覆盖 `columns`，之后再用同一对象 `Find` 只会查 pluck 的列。需要复用先 `Clone()`。
- **以为 Paginate 会克隆**：它不克隆，只是 `ToCount` 临时状态恢复；对原对象的其他字段（如 `joins`、`wheres`）不做任何备份。
- **在并发请求间共享 Builder**：Builder 没有锁，链式方法会写字段，必须每请求新建或 Clone。
- **用 `Select` 追加列**：`Select` 是替换；追加用 `AddSelect`。

## 相关阅读

- 编译阶段如何使用这些字段：[compile-pipeline](compile-pipeline.md)
- SQL 与参数顺序：[compile-pipeline](compile-pipeline.md)
- 保护判定：[safety](safety.md)
