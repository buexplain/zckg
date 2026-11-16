# 行扫描（ScanStruct / ScanStructClose）

`ScanStruct` / `ScanStructClose` 是**包级函数**（不是 Builder 方法），把原始 SQL 的 `*sql.Rows` 扫描进结构体。`Find`、`First`、`Paginate`、`KeyBy`、`Cursor`、`CursorBy` 内部复用同一套列映射与类型转换逻辑，因此本文的规则同样适用于这些终端方法。

```text
func ScanStruct(rows *sql.Rows, dest any, tagName ...string) error
func ScanStructClose(rows *sql.Rows, dest any, tagName ...string) error
```

典型用法是配合 [builder-exec](builder-exec.md) 中的原始 `DAO.Query`；示例统一包在 `func example()` 中，仅用于语法检查（`db`、`ctx`、`User` 是外部占位）。

## rows 生命周期：不关闭与自动关闭

| 函数 | 是否关闭 rows | 何时关闭 |
|---|---|---|
| `ScanStruct` | **不关闭** | 始终由调用方 `defer rows.Close()`，即使返回错误也不关闭 |
| `ScanStructClose` | 自动关闭 | 扫描结束或返回错误后由内部 `defer` 关闭 |

- `rows` 必须非 nil，本函数不做 nil 检查。
- 单行模式（`*struct`）只消费**第一行**，剩余行留在 rows 里未读；仍应关闭 rows 以释放连接。
- **单行模式必须在尚未读完的 rows 上调用**：`rows` 已被迭代到末尾（例如刚被上一次 `ScanStruct` 整表读完）时，`rows.Next()` 直接返回 false，得到 `sql.ErrNoRows`——不会回绕、不会重新执行查询。要再扫一遍必须重新 `Query`。

```go
func example() {
	rows, err := db.Query(ctx, "SELECT id, name, age FROM users WHERE age > ?", 18)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }() // ScanStruct 不关闭 rows，调用方负责

	var users []User
	if err := zcdb.ScanStruct(rows, &users); err != nil {
		slog.Default().Error("scan users", "err", err)
		return
	}

	// 上面的 rows 已读完：单行扫描必须重新查询，否则只会得到 sql.ErrNoRows
	rows2, err := db.Query(ctx, "SELECT id, name FROM users WHERE id = ?", 1)
	if err != nil {
		return
	}
	var user User
	err = zcdb.ScanStructClose(rows2, &user) // 自动 rows2.Close()，无需 defer
	if errors.Is(err, sql.ErrNoRows) {
		slog.Default().Info("未找到记录")
	}
}
```

## dest 类型

| dest 类型 | 行为 | 空结果 |
|---|---|---|
| `*struct` | 扫描第一行 | `sql.ErrNoRows` |
| `*[]struct` | 扫描所有行，逐行 append | 不写入元素，`err == nil` |
| `*[]*struct` | 扫描所有行，每行新建实例后 append | 不写入元素，`err == nil` |

校验顺序固定为「非指针检查 → 取 `rows.Columns()` → 按 `dest` 元素种类分派」，因此不同错误形态可以区分：

1. dest 不是指针 → `ErrNotPointer`；
2. 指针但指向的类型既不是结构体也不是切片（如 `*int`）→ `ErrScanDest`；
3. 切片但元素不是结构体/结构体指针（如 `*[]int`）→ `ErrInvalidStruct`。

切片模式下 `dest` 不会被清空：结果行追加到现有元素之后，复用同一个切片变量连续调用会累积。

## 标签变参

`tagName` 指定列映射使用的结构体标签名：

- 省略或传入空串 → 默认 `db`；
- 只读取**第一个**元素，多传无意义；
- 同一标签名同时用于「字段 → 列名」映射、嵌入结构体展开与游标列查找。`NewDBDao` 的 `tagName` 参数（第四个参数）会由终端方法自动透传，只有直接调用包级函数或使用多个不同标签的结构体时才需要显式传参。

```go
func example() {
	type zcUser struct {
		ID   int64  `zc:"id"`
		Name string `zc:"name"`
	}

	rows, err := db.Query(ctx, "SELECT id, name FROM users")
	if err != nil {
		return
	}
	var users []zcUser
	err = zcdb.ScanStructClose(rows, &users, "zc") // 第三个参数指定标签名，缺省为 "db"
	_ = err
}
```

字段到列名的规则：标签值即列名；标签值为 `-` 的字段跳过；无标签时字段名转 snake_case（`UserName` → `user_name`）；未导出字段直接跳过。

## 列名匹配与未匹配列

- 匹配是**精确字符串比较**：驱动返回的列名必须等于标签值或字段名的 snake_case 结果，区分大小写；不做大小写折叠，也不会把列名的 snake_case 反向还原成字段名。
- 结果集中未匹配的列被忽略（扫描目标为内部丢弃器），不报错、不影响其他列。
- 结构体有列、结果集没有该列时同样无影响——该字段保持零值。
- nil 嵌入指针导致字段不可用时，该列被静默忽略（不报错、不 panic）。
- 同一列名在结果集中出现多次时会写入同一字段，后者覆盖前者。

## 嵌入结构体展开

匿名嵌入结构体（值或指针）的字段会被递归展开，等价于「提升到外层」。展开采用广度优先，规则与 `encoding/json` 的「外层优先」一致，但有一处差异：

- **外层遮蔽嵌入层**：外层字段与嵌入层字段同名列时，映射到外层字段（与声明顺序无关）。
- **同深度冲突**：两个匿名嵌入结构体（或同一层内）出现同名列时，保留**声明顺序靠前**的字段；`encoding/json` 会丢弃冲突字段，二者不同。
- 未导出的嵌入类型不会被展开（匿名字段本身未导出），需要展开时类型名必须导出。
- 自引用/互引用嵌入（`type E struct{ *E }`、`A` 与 `B` 互相嵌入）只展开一次，不会无限递归。
- nil 嵌入指针在扫描时使对应字段不可用，该列被忽略；不会为它分配实例。

```go
func example() {
	type Base struct {
		ID int64 `db:"id"`
	}
	type userRow struct {
		Base        // 匿名嵌入：ID 被提升
		Name string `db:"name"`
	}

	rows, err := db.Query(ctx, "SELECT id, name, extra FROM users")
	if err != nil {
		return
	}
	var rowsOut []userRow
	err = zcdb.ScanStructClose(rows, &rowsOut) // id → Base.ID，extra 未匹配被忽略
	_ = err
}
```

## NULL 与类型转换

NULL 永远不是错误：非指针字段置零值，指针字段置 nil（`*[]byte` 也随之区分「NULL → nil」与「非空 BLOB → 非 nil」）。其余转换按「驱动返回值的实际类型」分派，而不是按数据库列类型：

| 驱动返回 | 目标字段 | 行为 |
|---|---|---|
| `[]byte` | `string`（含命名 string 类型） | 转字符串 |
| `[]byte` | `[]byte` 或 `[]byte` 命名类型 | 拷贝字节（与驱动缓冲区隔离） |
| `[]byte` / `string` | 其它切片（`[]int`、`[]string` 等） | 按 JSON 反序列化 |
| `[]byte` / `string` | map（键为 string） | 按 JSON 反序列化；键非 string 的 map 不支持 |
| `[]byte` / `string` | 结构体（含 `time.Time`） | 按 JSON 反序列化 |
| `[]byte` / `string` | 整数 | `strconv` 按**目标类型位宽**解析，溢出报错 |
| `[]byte` / `string` | 无符号整数 | 按位宽解析，负值与溢出报错 |
| `[]byte` / `string` | 浮点 | `strconv.ParseFloat` |
| `[]byte` / `string` | `bool` | `strconv.ParseBool`（`1`/`0`/`true`/`false` 等 Go 写法） |
| 整数 / 无符号 / 浮点 | `string` | 十进制格式化（不会当成 rune 转字符） |
| 整数 / 无符号 / 浮点 | `bool` | `!= 0` |
| `time.Time` | `string` | 格式化为 RFC3339 |
| `time.Time` | `time.Time` | 直接赋值 |
| 其它 | 反射可转换的类型 | `Convert` 兜底（含命名类型） |
| 其它 | 不可转换 | `ErrScanConvert` |

要点与边界：

- **数值溢出报错而非截断**：`"300"` 扫入 `int8`、`"-1"` 扫入 `uint` 都返回 `ErrScanConvert`，不会静默回绕。
- **数值→string 是格式化**：`int64(123)` 扫入 `string` 得到 `"123"`；若走反射 `Convert` 会被当成 rune 码点（`123` → `"{"`），因此该路径被显式拦截。
- **JSON 列**：数据库以文本/BLOB 存 JSON 时，扫入切片、string 键的 map、结构体都会自动反序列化；反序列化失败返回 `ErrScanConvert`，且错误链保留 `*json.SyntaxError`（可用 `errors.As` 提取）。
- **时间列 → `string` 的 RFC3339 前提**：只有当驱动把时间列以 `time.Time` 返回时才会格式化为 RFC3339。MySQL 需要在 DSN 开启 `parseTime=true`（本库测试统一用 `parseTime=true&loc=Local`）；驱动返回文本时按**原文本**处理，不会解析后再格式化。
- **文本 → `time.Time` 需要 JSON 形式**：目标为结构体时一律走 JSON 反序列化，`time.Time` 也在这条路径上，因此裸时间文本（如 `2024-01-02 03:04:05`）不是合法 JSON，会报 `ErrScanConvert`。要得到 `time.Time` 字段，应让驱动直接返回 `time.Time`（开启 `parseTime`）。
- **命名类型的赋值安全**：`type status string` 之类的命名类型会先 `Convert` 到目标类型再赋值；这是 fuzz 发现过的 `reflect.Set` panic 防护点。
- 无法转换时的兜底错误形如 `zcdb: scan value conversion failed: cannot convert <源类型> to <目标类型>`，行扫描层再包一层 `zcdb: scan row failed: %w`，`errors.Is` 仍可命中 `ErrScanConvert`。

## 错误哨兵

`ScanStruct` 系列可能返回以下错误（除 `sql.ErrNoRows` 外均可用 `errors.Is` 匹配）：

| 错误 | 含义 | 错误链形态 |
|---|---|---|
| `ErrNotPointer` | dest 不是指针 | `%w, got %T` |
| `ErrScanDest` | dest 是指针但指向类型非法（必须是 `*struct`、`*[]struct`、`*[]*struct`） | `%w, got *%s` |
| `ErrInvalidStruct` | 切片元素不是结构体/结构体指针 | `%w: slice element must be struct or *struct, got %s` |
| `ErrScanConvert` | 列值无法转换为目标字段类型（数值解析/溢出、JSON 失败、类型不可转换） | 外层 `zcdb: scan row failed: %w` |
| `sql.ErrNoRows` | `*struct` 模式下无结果行 | 未包装 |

此外还有一类**非哨兵**错误：取列名失败时返回 `zcdb: get columns failed: %w`（包装驱动错误）。`ScanStructClose` 关闭 rows 的错误被忽略，不会覆盖扫描结果（与 `database/sql` 惯例一致）。

```go
func example() {
	var user User
	err := zcdb.ScanStruct(rows, &user)
	switch {
	case err == nil:
		fmt.Println(user.Name)
	case errors.Is(err, sql.ErrNoRows):
		slog.Default().Info("未找到记录")
	case errors.Is(err, zcdb.ErrScanConvert):
		slog.Default().Error("数据转换失败", "err", err)
	case errors.Is(err, zcdb.ErrNotPointer), errors.Is(err, zcdb.ErrScanDest), errors.Is(err, zcdb.ErrInvalidStruct):
		slog.Default().Error("dest 类型不符合要求", "err", err)
	default:
		slog.Default().Error("扫描失败", "err", err)
	}
}
```

## 相关阅读

- 结构体与列的映射模型：[mapping](../concepts/mapping.md)
- 原始 SQL 执行入口：[builder-exec](builder-exec.md)
- 终端查询方法对扫描的复用：[builder-query](builder-query.md)、[builder-cursor](builder-cursor.md)
- 错误变量总表：[errors](errors.md)
- DAO 构造与 `tagName` 参数：[builder-core](builder-core.md)、[architecture](../concepts/architecture.md)
