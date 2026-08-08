# Builder API 分类文档

## 目标

新建 `zcdb/docs/builder-api.md`，将 `builder.go` / `builder_exec.go` / `builder_cursor.go` 中 Builder 对外的全部 API 按功能意图分组整理。每个 API 给出：名字、一句话中文说明、当前所在文件（便于后续按分类拆分文件时对照移动）。

本任务只新增这一个文档，不改动任何源码、测试与既有文档。

## 文档结构（分组方案）

用表格呈现（列：API 名字 | 说明 | 当前位置），每组前有简短导语。共 11 组，约 120 个方法：

### 1. 创建与状态管理
`NewBuilder`、`Clone`（深拷贝）、`Force`（显式允许无 WHERE 的 Delete/Update）。均在 builder.go。

### 2. 数据源与查询列
`Table`、`TableSub`、`Select`、`SelectRaw`、`SelectSubquery`、`AddSelect`、`AddSelectSub`、`Distinct`。

### 3. WHERE 查询条件（最大一组，约 48 个，builder.go）
按子意图细分小节：
- 基本比较：`Where`/`OrWhere`、`WhereDate`、`WhereColumn`、`WhereRaw`/`OrWhereRaw`
- IN / NULL / BETWEEN：`WhereIn`/`WhereNotIn`、`WhereNull`/`WhereNotNull`、`WhereBetween` 及其 Or/Not 变体、`WhereBetweenColumns` 四变体、`WhereValueBetween`/`OrWhereValueBetween`
- LIKE / 空安全：`WhereLike`/`OrWhereLike`、`WhereNotLike`、`WhereNullSafeEquals`/`WhereNullSafeNotEquals`
- 嵌套逻辑组：`WhereNested`/`OrWhereNested`、`WhereNot`/`OrWhereNot`、`WhereAll`/`OrWhereAll`、`WhereAny`/`OrWhereAny`、`WhereNone`/`OrWhereNone`
- 子查询：`WhereExists` 四变体、`WhereSub`/`OrWhereSub`、`WhereInSub`/`WhereNotInSub`

### 4. 连表 JOIN
- Builder 层（builder.go）：`Join`/`LeftJoin`/`RightJoin`、`JoinOn`/`LeftJoinOn`/`RightJoinOn`、`CrossJoin`/`CrossJoinOn`/`CrossJoinSub`、`JoinSub`/`LeftJoinSub`/`RightJoinSub`
- JoinBuilder 回调 API（clause.go，附录小节）：`On`/`OrOn`、`Where`/`OrWhere`、`WhereNull`/`WhereNotNull`、`WhereIn`/`WhereNotIn`、`WhereExists`、`WhereNested`/`OrWhereNested`/`OnNested`

### 5. 分组与 HAVING
`GroupBy`、`GroupByRaw`、`Having`/`OrHaving`、`HavingRaw`、`HavingBetween`/`HavingNotBetween`、`HavingNull`/`HavingNotNull`、`HavingNested`/`OrHavingNested`。

### 6. 排序、分页、UNION、锁
`OrderBy`/`OrderByDesc`/`OrderByRaw`/`InRandomOrder`、`Limit`/`Offset`/`ForPage`、`Union`/`UnionAll`、`LockForUpdate`/`SharedLock`（注明带锁查询走写连接、SQLite 不支持）。

### 7. ToXxx 编译系列（只生成 SQL 不执行，builder.go）
`ToSelect`、`ToInsert`、`ToInsertOrIgnore`、`ToUpsert`、`ToInsertUsing`、`ToInsertOrIgnoreUsing`、`ToUpdate`、`ToDelete`、`ToDeleteJoin`、`ToTruncate`、`ToCount`、`ToExists`、`ToAggregate`、`ToIncrement`、`ToDecrement`。说明注明返回 `(SQL, 绑定参数, error)` 形态与关键约束。

### 8. 执行查询系列（builder_exec.go）
`First`、`Find`、`Pluck`（单列/map 键值/keyBy 三形态）、`Paginate`、`Count`、`Exists`、`Value`、`Max`/`Min`/`Sum`/`Avg`/`Average`（注明空集语义差异）。

### 9. 执行写入系列（builder_exec.go）
`Insert`、`InsertGetId`、`InsertOrIgnore`、`InsertUsing`、`InsertOrIgnoreUsing`、`Upsert`、`Update`、`Increment`/`Decrement`、`Delete`、`DeleteJoin`、`Truncate`。注明无 WHERE 保护机制与 Force 的关系。

### 10. 流式迭代系列（builder_cursor.go）
`Cursor`、`CursorBy`、`CursorByDesc`。

### 11. 内部辅助（不对外但影响拆分边界，单列备注）
`query`（锁路由入口）、`collectSelectBindings` 等绑定收集函数、`hasEffectiveWhere`/`hasEffectiveJoin`、`pluckKeyBy` 等——标注"内部方法，拆分时需随所属分类一并移动"。

## 文档尾部附注

- 绑定参数顺序规则：SELECT 为 SELECT_SUB→FROM_SUB→JOIN→WHERE→GROUP BY→HAVING→UNION；UPDATE 按方言区分 JOIN→SET→WHERE（MySQL）与 SET→JOIN→WHERE（PG/SQLite）。
- 运算符白名单与错误累积机制简述。

## 交付物

仅 `zcdb/docs/builder-api.md` 一个新文件。