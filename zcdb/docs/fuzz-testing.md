# zcdb 模糊测试（fuzz）设计与执行

fuzz 是定向单测、跨方言差分与真实引擎 oracle 的补充：定向用例负责精确期望，差分负责语义一致性，fuzz 在更大的输入空间里寻找 `panic`、不变量违背与 oracle 分歧。fuzz 不替代精确断言。

## 一、目标清单

| 目标 | 所在文件 | 主要断言 |
|---|---|---|
| `FuzzSplitSQLCode` | `sqlScan_test.go` | 片段拼接等于原文、无空段、无相邻代码段、结果确定 |
| `FuzzCompileRaw` | `rawLiteral_test.go` | 占位符数量/`$N` 编号规则、内联后代码区无剩余占位符、重复编译确定 |
| `FuzzNormalizeSQLComment` | `builder_unit_test.go` | 合法 UTF-8、≤255 rune、无禁止字符、幂等 |
| `FuzzIdentifierWrapping` | `grammar_wrap_fuzz_test.go` | 三方言引号加倍可逆；SQLite 执行列/别名不破坏解析结构 |
| `FuzzDestructiveGuard` | `builder_exec_fuzz_test.go` | Update/Increment/Decrement/Delete/DeleteJoin 的无有效 WHERE/JOIN 保护 |
| `FuzzWhereTruthEvaluation` | `builder_exec_truth_fuzz_test.go` | AND 优先、OR、嵌套组、NOT 的静态真值归约与保护联动 |
| `FuzzInsertUpdateData` | `reflect_fuzz_test.go` | 列/值提取模型与 ToInsert/ToUpdate 参数、占位符一致 |
| `FuzzStructFieldMapping` | `reflect_mapping_fuzz_test.go` | 外层优先、遮蔽、嵌入 BFS、标签/蛇形、nil 嵌入指针 |
| `FuzzNullSafeFieldScan` | `scanner_conversion_fuzz_test.go` | 目标类型 × 源类型转换矩阵；NaN 使用 NaN-aware 比较 |
| `FuzzScanRoundTrip` | `scanner_fuzz_test.go` | SQLite 往返、NULL 零值、未匹配列、空结果、`*[]byte` 区分 NULL/空 BLOB |
| `FuzzCursorByPagination` | `builder_cursor_fuzz_test.go` | 分页不重不漏、chunk 0/负数、升序/降序、break、NULL/字段缺失/字段不可用 |
| `FuzzRawLiteralSQLiteRoundTrip` | `rawLiteral_engine_fuzz_test.go` | SQLite 字面量往返；NaN/Inf 与非空指针返回 `ErrRawValueNotInlinable` |
| `FuzzBuilderCompile` | `builder_compile_fuzz_test.go` | 三方言编译确定性、错误文本一致、跨方言 args 一致、占位符数量/顺序 |
| `FuzzBuilderSubqueryCompile` | `builder_compile_fuzz_test.go` | 子查询/UNION/InsertUsing 的同上不变量 |
| `FuzzBuilderDeleteJoinCompile` | `builder_compile_fuzz_test.go` | DeleteJoin 绑定形态；shape 5 断言 `ErrDeleteWithJoin` |
| `FuzzBuilderClone` | `builder_clone_fuzz_test.go` | 编译一致、克隆链、双向修改隔离、Force/Primary 直接字段断言、环引用快速失败 |
| `FuzzUpsertInsertUsingMatrix` | `builder_upsert_fuzz_test.go` | 方言错误规则、参数/占位符一致、冲突目标与 SET 列集合 |
| `FuzzRawLiteralMySQLRoundTrip` | `rawLiteral_engine_external_fuzz_test.go` | 真实 MySQL 字符串/字节转义往返（夜间门控） |
| `FuzzRawLiteralPGRoundTrip` | `rawLiteral_engine_external_fuzz_test.go` | 真实 PostgreSQL 字符串/字节转义往返（夜间门控） |

## 二、种子与失败语料

- 普通 `go test ./...` 回放 `f.Add` 种子与 `zcdb/testdata/fuzz/<Target>/` 下的失败语料，不主动探索。
- 失败输入由 Go 自动写入 `testdata/fuzz/<Target>/`，应与修复一起提交，并补一条定向回归用例。
- 输入长度上限统一为 4 KB（SQL 文本 64 KB）；NaN/Inf、超范围无符号整数等已知不可表示值要么显式断言错误，要么在文件内注明跳过原因。
- 通过 Builder 构造 SQL 的非注释专项目标统一用 `newTestBuilder`（注入 `zcdb:test` 注释）；`FuzzBuilderClone` 的注释本身是 fuzz 维度，属于注释专项例外。

## 三、执行命令

一次只能探索一个目标；常规 CI 只跑种子，探索作为本地或夜间任务：

```powershell
go test ./zcdb -run '^$' -fuzz '^FuzzSplitSQLCode$' -fuzztime=30s
go test ./zcdb -run '^$' -fuzz '^FuzzBuilderCompile$' -fuzztime=30s
go test ./zcdb -run '^$' -fuzz '^FuzzScanRoundTrip$' -fuzztime=30s
```

真实引擎目标默认跳过，设置环境变量后运行（需要本机容器）：

```powershell
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
$env:ZCDB_ENGINE_FUZZ=1
go test ./zcdb -run '^$' -fuzz '^FuzzRawLiteralMySQLRoundTrip$' -fuzztime=30s
go test ./zcdb -run '^$' -fuzz '^FuzzRawLiteralPGRoundTrip$' -fuzztime=30s
```

报错时先分清**实现缺陷**还是 **oracle 缺陷**：参考模型与共享 helper 都可能被 fuzz 证伪。

## 四、已知边界

- `FuzzCompileRaw` / 占位符计数与实现共用 `splitSQLCode` / `numberedDigits`，验证的是编译语义，不独立证明扫描器正确；扫描器边界由 `TestSplitSQLCode` 的精确期望锁定。
- `FuzzSplitSQLCode` 只断言拼接、空段、相邻代码段与确定性；各类引号/注释/美元引用的精确切分由表驱动用例覆盖。
- `FuzzStructFieldMapping` 的期望值遍历使用测试内独立实现；类型级字段集合仍是硬编码 fixture，新增映射规则时需同步扩展。
- `FuzzScanRoundTrip` 中 modernc SQLite 对 NULL 与空 BLOB 扫描到 `[]byte` 都返回 nil，二者区分由 `*[]byte` 字段单独验证。
- 跨方言错误当前比较错误文本；若未来出现方言特有错误，应改为 `errors.Is` 哨兵比较并在目标中注明。
- MySQL 在“全部插入列均为冲突列”时生成 no-op 自赋值（优先 uniqueBy 中真实插入列，否则首列）；`FuzzUpsertInsertUsingMatrix` 按该规则独立推导 SET 列。
- `FuzzCursorByPagination` 依据文档要求游标列有序且唯一；重复游标值的分页语义不在输入域内。
- `FuzzDestructiveGuard` 在拒绝路径使用 nil DAO 证明保护先于数据库访问；`Force()` 的真实放行与执行由 SQLite 定向/集成用例覆盖。
- `FuzzIdentifierWrapping` 排除 `table.column`、`AS` 别名与 `*` 形态，这些分支由 `TestGrammar_WrapColumn_AllGrammars` 等定向用例覆盖。
- 外部引擎 fuzz 当前只覆盖 string/bytes；bool、数值、时间、命名类型与 `driver.Valuer` 待扩展。
- 扫描器已知未覆盖的语法：SQLite `[...]`、MySQL `/*! ... */`、PostgreSQL `U&'...'`，命中原样按代码区处理。
- SchemaInspector、连接池并发路由目前没有 fuzz 目标，分别由结构化 DDL 定向用例、故障注入与 race 测试覆盖。
