# zcdb 数据库访问模块

zcdb 是 `database/sql` 之上的查询构造与执行层：**Builder 累积查询意图，Grammar 编译方言 SQL，DBDao 决定走哪个连接并执行，Pool 管理主从连接**。它不绑定具体驱动，也不隐藏数据库语义——SQL 形态、方言差异、事务与路由规则都尽量显式。

## 30 秒示例

```go
func example() {
	pool, err := zcdb.NewPool(zcdb.PoolConfig{DriverName: "sqlite", DSN: "file::memory:?cache=shared"})
	if err != nil {
		panic(err)
	}
	db, err := zcdb.NewDBDao(pool, "sqlite", nil, "", "")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	var users []User
	err = db.Builder().Table("users").Where("status", "=", "active").OrderBy("id", "ASC").Find(ctx, &users)
}
```

## 文档地图

| 想知道什么 | 去哪里 |
|---|---|
| 从零跑通一条完整链路 | [getting-started.md](getting-started.md) |
| 一次查询在内部经历了什么、为什么这样分层 | [concepts/architecture.md](concepts/architecture.md) |
| Builder 的状态、Clone 与「哪些方法会改自己」 | [concepts/builder-state.md](concepts/builder-state.md) |
| SQL 怎么编译出来、参数按什么顺序绑定 | [concepts/compile-pipeline.md](concepts/compile-pipeline.md) |
| 结构体与列怎么映射、NULL/类型怎么转换 | [concepts/mapping.md](concepts/mapping.md) |
| 读写分离、事务与连接是怎么选定的 | [concepts/execution-routing.md](concepts/execution-routing.md) |
| 无 WHERE 保护、Expression、注释注入的边界 | [concepts/safety.md](concepts/safety.md) |
| 每个 API 的签名、SQL、边界与错误 | [reference/](reference/) 下的分文件参考 |
| 分页、事务、动态条件、UPSERT 等任务写法 | [how-to/](how-to/) |
| 旧版本升级与破坏性变更 | [migration/breaking-changes.md](migration/breaking-changes.md) |
| 模块行为的历史审查记录 | [consistency-review.md](consistency-review.md)、[consistency-review-20261116.md](consistency-review-20261116.md) |

## 四层模型

```
  用户代码
     │  db.Builder().Table("users").Where(...).OrderBy(...).Find(ctx, &dest)
     ▼
┌──────────┐   链式累积状态，不生成 SQL
│ Builder  │   （表、列、WHERE、JOIN、排序、分页、锁、注释、错误）
└────┬─────┘
     │  ToSelect() / ToUpdate() / ...（纯编译，不碰数据库）
     ▼
┌──────────┐   标识符包裹、占位符编号、方言差异、绑定参数顺序
│ Grammar  │   MySQLGrammar / PostgresGrammar / SQLiteGrammar
└────┬─────┘
     │  (sqlStr, args, err)
     ▼
┌──────────┐   事务检测（ctx）→ 锁/Primary → 读库或写库
│  DBDao   │   慢 SQL 回调、rows 生命周期
└────┬─────┘
     ▼
┌──────────┐   主库 *sql.DB + 从库列表，路由策略
│   Pool   │   创建时 Ping 校验；Close 幂等
└──────────┘
```

职责边界：

| 层 | 负责 | 不负责 |
|---|---|---|
| `Builder` | 收集查询意图、维护链式状态、执行终端方法 | 方言细节、连接选择 |
| `Grammar` | 把 Builder 状态编译成某方言的 SQL 与参数 | 执行、事务、路由 |
| `DBDao` | 选连接（事务/主从）、执行、回调、扫描入口 | 构造 SQL 文本 |
| `Pool` | 主从 `*sql.DB` 生命周期、从库策略 | SQL 语义 |

## 一条查询的五个阶段

1. **构造**：链式方法把状态写入 `Builder`（`table`/`wheres`/`orders`/`columns` …），方法返回同一个 `*Builder` 以支持继续链式。
2. **编译**：终端方法调用对应的 `ToXxx`（如 `ToSelect`），`Grammar` 按方言生成 SQL 与占位符，绑定参数按固定顺序收集；DAO 默认注释在这一步一次性追加。
3. **路由**：`DBDao` 检查 `ctx` 是否携带本 Pool 的事务；否则按锁子句、`Primary()`、读/写入口选择主库或从库。
4. **执行**：通过 `*sql.DB` / `*sql.Tx` 执行，慢 SQL 回调在语句执行后触发（panic 被隔离）。
5. **扫描**：`Find`/`First` 等把 `*sql.Rows` 按列映射标签写入结构体；`Cursor` 逐行迭代，`ScanStruct` 用于原始 SQL 结果。

阅读顺序建议：先 [getting-started.md](getting-started.md)，再 [concepts/architecture.md](concepts/architecture.md)，之后按需查 [reference/](reference/)。

## 完整文件清单

| 目录 | 文件 | 内容 |
|---|---|---|
| 根 | `README.md` | 本文：模型总览、文档地图、约定 |
| 根 | `getting-started.md` | 可完成的入门教程（SQLite 内存库） |
| `concepts/` | `architecture.md` | 四层职责与一次查询的生命周期 |
| `concepts/` | `builder-state.md` | 状态字段、可变性、Clone 与终端方法策略 |
| `concepts/` | `compile-pipeline.md` | 编译阶段、Grammar 职责、绑定顺序、注释追加 |
| `concepts/` | `mapping.md` | 标签、嵌入展开、扫描转换、写入提取 |
| `concepts/` | `execution-routing.md` | 连接池、读写路由、事务、慢 SQL 回调 |
| `concepts/` | `safety.md` | 破坏性保护、ORDER BY/LIMIT 限制、Expression/注释边界 |
| `reference/` | `builder-core.md` | NewBuilder、Table/TableSub、Comment、Force、Primary、Clone |
| `reference/` | `builder-select.md` | Select/AddSelect/SelectRaw/SelectSub/Distinct |
| `reference/` | `builder-where.md` | 全部 WHERE 家族 |
| `reference/` | `builder-join.md` | JOIN 与 JoinBuilder |
| `reference/` | `builder-order-group.md` | GROUP BY/HAVING/ORDER BY/LIMIT/UNION/锁 |
| `reference/` | `builder-query.md` | Find/First/Value/Count/Exists/聚合/Pluck 族/Paginate |
| `reference/` | `builder-cursor.md` | Cursor/CursorBy |
| `reference/` | `builder-exec.md` | Insert/Upsert/Update/Increment/Delete/Truncate |
| `reference/` | `builder-compile.md` | 15 个 ToXxx、绑定顺序、Expression、CompileRaw |
| `reference/` | `scanner.md` | ScanStruct/ScanStructClose |
| `reference/` | `schema.md` | SchemaInspector 与元数据结构 |
| `reference/` | `terminal-methods.md` | 终端方法语义矩阵与选型 |
| `reference/` | `dialect-matrix.md` | 三方言差异总表 |
| `reference/` | `errors.md` | 45 个哨兵按入口分组 |
| `how-to/` | `pagination.md` | 分页与大偏移、CursorBy 选型 |
| `how-to/` | `transactions.md` | 事务、嵌套复用、跨 Pool |
| `how-to/` | `dynamic-queries.md` | 动态条件、Clone、Force、EXISTS 陷阱 |
| `how-to/` | `upsert.md` | UPSERT/InsertOrIgnore/InsertUsing |
| `how-to/` | `schema-inspection.md` | 巡检、模型草稿、迁移比对 |
| `migration/` | `breaking-changes.md` | 破坏性变更与升级清单 |

## 核心约定

- **Builder 不是不可变对象**：链式方法修改自身并返回 `b`；需要保留基线时显式 `Clone()`。各终端方法的克隆策略见 [concepts/builder-state.md](concepts/builder-state.md)。
- **编译与执行分离**：所有 `ToXxx` 只返回 `(sql, args, err)`，不执行；可把 SQL 交给别的组件执行或写入日志。
- **错误是值**：可导出的哨兵错误用 `errors.Is` 判定；驱动错误原样透传或 `%w` 包装。完整清单见 [reference/errors.md](reference/errors.md)。
- **方言差异集中在 Grammar**：Builder 层不写死包裹符与占位符；三方言行为差异汇总在 [reference/dialect-matrix.md](reference/dialect-matrix.md)。
- **安全边界显式**：无 WHERE 的 Update/Delete 默认拒绝；`Expression` 与原始 SQL 是刻意保留的逃逸口，责任归调用方。见 [concepts/safety.md](concepts/safety.md)。

## 术语

| 术语 | 含义 |
|---|---|
| 编译入口 | `ToSelect`、`ToUpdate` 等 `ToXxx` 方法：只生成 SQL 与参数 |
| 终端方法 | `Find`、`First`、`Update`、`Delete`、`Paginate`、`Cursor` 等：编译并执行 |
| 绑定参数 | SQL 占位符对应的 `args`，按固定顺序收集 |
| 列映射标签 | 结构体 tag（默认 `db`），DAO 创建时确定、不可变更 |
| 主库 / 从库 | 写连接 / 读连接；无配置从库时读操作降级主库 |
| transient state | 编译期临时修改并在 `defer` 中恢复的 Builder 状态（分页/排序/锁/列） |

## 示例约定

文档中的 Go 示例统一包在 `func example()` 中，`db`、`ctx`、`pool`、模型类型是外部变量，不保证可独立运行；`getting-started.md` 的完整程序补上 `package main` 后可直接运行。示例的语法由仓库门禁 `TestDocsGoBlockSyntax` 与 `doc_go_examples.py --strict` 校验，SQL/args 注释以 MySQL 形态为基线，方言差异单独说明。
