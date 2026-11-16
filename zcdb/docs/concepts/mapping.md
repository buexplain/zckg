# 结构体映射与扫描

zcdb 的结构体映射是**运行期反射**而不是代码生成：读路径把 `*sql.Rows` 的列写进结构体字段，写路径把结构体字段展开成列与值。两者共用同一套「类型 + 标签名 → 字段列表」的解析结果。

## 标签与列名

- 默认读取 `db` 标签；DAO 创建时可以通过 `NewDBDao` 的第四参数改成任意标签名（如与其它 ORM 共用结构体）。
- 无标签的字段把 Go 字段名转 snake_case：`UserID` → `user_id`。
- 标签值为 `-` 的字段跳过。
- 标签名在 DAO 创建时确定、之后不可变；不同标签名的 DAO 各自使用独立缓存。

```go
func example() {
	type User struct {
		ID   int64  `db:"id"`
		Name string // 无标签 → name
		skip string `db:"-"` // 未导出，天然跳过
	}
}
```

## 嵌入结构体：广度优先、外层优先

`collectStructFields` 用 BFS 展开匿名嵌入结构体（值类型与指针类型都支持）：

1. 先处理外层字段，嵌入结构体入队到下一层；
2. 同层按声明顺序；
3. 列名一旦出现就登记（`seen`），后续同名列跳过——**浅层遮蔽深层**；
4. 同深度冲突（两个匿名嵌入结构体都有同名列）保留**声明顺序靠前者**。这一点与 `encoding/json` 不同：JSON 会直接丢弃冲突字段，zcdb 保留先声明者。
5. 同一嵌入类型只展开一次（`visited`），自引用/互引用嵌入不会无限入队。

| 结构 | 结果 |
|---|---|
| 外层 `Name` + 嵌入层 `Name` | 外层生效，嵌入层字段保持零值 |
| 两个同层嵌入都有 `Age` | 先声明的那个生效 |
| 嵌入指针为 nil | 扫描时该列跳过（不回填），写入时按 nil 处理 |

## 缓存

解析结果按 `(结构体类型, 标签名)` 缓存（`scanCache`/`structCache`）。因此：

- 同一类型在不同标签名的 DAO 下结果不同，缓存不会串；
- 缓存是无界 `sync.Map`，结构体类型数量有限，正常使用不会成为问题；
- 首次使用某类型时有一次性反射开销，之后命中缓存。

## 读路径：列 → 字段

1. `rows.Columns()` 得到列名列表；
2. 用 `(类型, 标签名)` 取字段映射，构造每列的扫描目标：
   - 命中字段 → 指向该字段的 `nullSafeField`；
   - 未命中 → `discard`（扫描后丢弃）；
   - nil 嵌入指针下的字段 → `discard`（无法安全取值）。
3. 逐行 `rows.Scan`，`nullSafeField.Scan` 负责类型转换。

### 扫描转换规则

| 源值 | 目标 | 行为 |
|---|---|---|
| `NULL` | 非指针字段 | 保持零值 |
| `NULL` | 指针字段 | 置 `nil` |
| `time.Time` | `string` | 格式化为 RFC3339（MySQL 需 DSN 开启 `parseTime=true`，否则驱动返回 []byte，按原文本转换） |
| `[]byte`/`string` 的数字文本 | 数值字段 | 按目标位宽解析，溢出/非法返回 `ErrScanConvert` |
| `[]byte`/`string` | `bool` | `strconv.ParseBool` |
| `[]byte`/`string` | 切片/结构体/map | 按 JSON 反序列化；失败返回 `ErrScanConvert` |
| 数值 | `string` | 格式化为数字文本（不会把 int64 当 rune 转字符） |
| 其他可转换类型 | 目标字段 | 使用 `reflect` 的可转换规则；不可转换返回 `ErrScanConvert` |

数值溢出（如 `"300"` → `int8`）是**报错**而不是截断。

## 写路径：结构体 → 列与值

`extractInsertData`/`extractUpdateData` 负责：

- **列集合**：由字段映射决定；批量插入以**首行**为模板，后续行同列取各自的值；
- **nil 跳过**：指针字段为 nil、`any` 字段为 nil 时不参与该行的列集合；
- **解引用**：非 nil 指针自动取指向的值；非 nil 的 `driver.Valuer` 在参数绑定时由驱动处理；
- **Expression**：直接内联为 SQL 文本，不进 args；
- **全部字段都被跳过**：返回 `ErrNoFields`（不会生成空列集合的 SQL）；
- **非法 data**：非结构体/结构体指针/切片返回 `ErrInvalidStruct`。

`Update` 的规则相同，但要求单个结构体或结构体指针；nil 字段跳过意味着可以做「部分更新」：

```go
func example() {
	name := "alice"
	// 只更新 name；Age 为 nil 不参与 SET
	affected, err := db.Builder().Table("users").Where("id", "=", 1).
		Update(ctx, UserUpdate{Name: &name})
	// SQL: UPDATE `users` SET `name` = ? WHERE `id` = ?
	// args: [alice 1]
}
```

## 错误归属

| 错误 | 触发 |
|---|---|
| `ErrInvalidStruct` | 写入 data 不是结构体/结构体指针/切片，或 ScanStruct 切片元素非法 |
| `ErrNoFields` | 结构体没有可写/可映射字段，或全部字段被跳过 |
| `ErrScanDest` | ScanStruct 的 dest 是指针但指向非法类型 |
| `ErrScanConvert` | 扫描时的数值溢出、JSON 反序列化失败、类型不可转换 |

完整清单见 [reference/errors.md](../reference/errors.md)。

## 常见陷阱

- **NULL 不是错误**：非指针字段遇到 NULL 静默保留零值；需要区分「NULL」与「零值」时用指针字段。
- **未匹配列被丢弃**：`SELECT *` 与结构体字段不齐不会报错，只是多余列被忽略。
- **同名列遮蔽**：外层字段永远优先，嵌入层不会「拼接」。
- **写入零值**：非指针字段的零值会参与 INSERT/UPDATE（`Id: 0` 也会进列），需要跳过时用指针/`any` 并置 nil。
- **标签名写错**：字段会回退 snake_case，不会报错；需要确认时用编译出的列名对照。

## 相关阅读

- 查询终端方法与 ScanStruct：[reference/builder-query](../reference/builder-query.md)、[reference/scanner](../reference/scanner.md)
- 写操作字段规则：[reference/builder-exec](../reference/builder-exec.md)
- 错误全表：[reference/errors](../reference/errors.md)
