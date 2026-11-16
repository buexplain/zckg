# 测试与排障

> 返回 [zcmodel 文档地图](zcmodel.md)。本文回答：测试怎么分层、命令怎么跑、如何复现和定位一个生成缺陷。

## 测试文件布局

按被测源码文件归属（遵循仓库 AGENTS.md）：

| 文件 | 覆盖对象 |
|---|---|
| `input_test.go` | 枚举常量与 `IsValid` |
| `generate_test.go` | `Generate` 的校验、字段补全、端到端产物、golden、fuzz 入口 |
| `build_test.go` | 代码组装、tag 转义、ddl、索引块、骨架合并、原子写、fuzz 入口 |
| `toCase_test.go` | `SplitWords`、`ToPascalCase`、六种风格、`formatJSONTag`、fuzz |
| `columnTypeToGoType_test.go` | 归一化、映射表、fuzz |
| `integration_test.go` | 真实 MySQL/PostgreSQL/SQLite 的列类型与 Schema 桥接 |
| `docs_examples_compile_test.go` | 文档示例的编译级镜像（手工收录，需随文档同步） |
| `testhelpers_test.go` | 共享 helper：`writeAndVerify`、`typeCheckGoSource`、断言、样例列/索引 |

## 测试分层

- **纯单元**：映射表、命名、tag 转义、ddl 片段、索引块、错误文本；不触碰文件系统。
- **端到端/golden**：临时目录 `t.TempDir()` 中调用 `Generate`，逐字段核对 tag、索引块、gofmt 一致性（`TestGenerate_SampleWithMetadataGolden`）。
- **合并回归**：`writeAndVerify` 写存量文件 → 调用 `writeOrReplaceStruct` → 断言顺序/保留/移除，并用 `typeCheckGoSource` 做 `go/types` 编译级检查。
- **增量场景**：幂等（二次生成逐字节相同）、CRLF、权限位、只读目录（Windows 按平台 Skip）。
- **集成**：真实数据库建表 → `SchemaInspector` → 桥接 → 生成 → 核对类型/注释/索引；数据库不可达时 `t.Skip`，SQLite 用内存库始终执行。
- **fuzz**：`FuzzGenerate`、`FuzzWriteOrReplaceStruct`、`FuzzAssembleDDLFragment`、`FuzzColumnTypeToGoType`、`FuzzNameConversion`；`f.Add` 种子写在测试函数内，发现的失败语料写入 `testdata/fuzz/<Target>/`，普通 `go test` 跑种子与已存语料。

## 常用命令

```shell
# 全量（zcmodel）
go test ./zcmodel -count=1

# 竞态 / 乱序重复
go test ./zcmodel -count=1 -race
go test ./zcmodel -shuffle=on -count=5

# 覆盖率
go test ./zcmodel -cover -count=1
go tool cover -func=coverage.out

# 静态检查与格式
go vet ./zcmodel
gofmt -l zcmodel

# 单个用例
go test ./zcmodel -run '^TestWriteOrReplaceStruct_SemicolonSeparatedDecls$' -count=1 -v
```

集成测试需要本地数据库（与 zcdb 共用 DSN）：

```shell
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
```

`-v` 输出中的 `--- SKIP` 需要逐条确认原因：数据库不可达属预期；Windows 上的 `TestWriteOrReplaceStruct_PreservePermissions`、`TestWriteOrReplaceStruct_ReadOnlyDir` 属平台条件。

### fuzz

```shell
go test ./zcmodel -run '^$' -fuzz '^FuzzGenerate$' -fuzztime=10s
go test ./zcmodel -run '^$' -fuzz '^FuzzWriteOrReplaceStruct$' -fuzztime=10s
go test ./zcmodel -run '^$' -fuzz '^FuzzAssembleDDLFragment$' -fuzztime=10s
go test ./zcmodel -run '^$' -fuzz '^FuzzColumnTypeToGoType$' -fuzztime=10s
go test ./zcmodel -run '^$' -fuzz '^FuzzNameConversion$' -fuzztime=10s
```

失败的语料会写入 `testdata/fuzz/<Target>/`，普通 `go test` 会重放，作为永久回归语料保留。

## 关键 helper

| helper | 用途 |
|---|---|
| `writeAndVerify(t, initialContent, entityCode, doCode, neededImports)` | 在临时目录 `model/` 中写存量文件、调用 `writeOrReplaceStruct`、返回产物 |
| `typeCheckGoSource(t, src)` | 把产物放入临时 package 做 `go/types` 编译级检查（能发现 imported and not used、字段与方法同名、未定义标识符等 parser 层发现不了的错误） |
| `assertDeclOrder` / `assertContains` / `assertNotContains` / `assertContainsAll` | 声明顺序与内容断言 |
| `importPathsOf` | 从 AST 提取 import 路径 |
| `fieldTagOf` | 从 AST 提取字段 tag 并按 `strconv.Unquote` 还原 |
| `sampleFieldExpectations` | 端到端 golden 与集成测试共用的字段/tag 期望 |
| `openMySQLDAO` / `openPgDAO` / `openSQLiteDAO`、`bridgeColumns`、`bridgeIndexes` | 集成测试建连与 Schema 桥接 |

## 排障方法

1. **先确认复现层级**：`go/parser` 可解析、`go/format` 可格式化、`go/types` 可编译是三个不同层级；生成失败时看错误文本属于哪个阶段（见 [失败矩阵](pipeline.md)）。
2. **最小化输入**：把 `Input` 缩到一列、一个存量声明，确认是校验、组装还是合并阶段。
3. **看中间产物**：需要检查包内未导出行为（槽位、`beforeMap`、import 增删）时，用 `go test -overlay` 把临时 `_test.go` 映射进 `zcmodel` 包，不修改仓库文件：
   ```shell
   go test ./zcmodel -run '^TestProbeXxx$' -count=1 -v -overlay "$env:TEMP\overlay.json"
   ```
   overlay JSON 用正斜杠路径；映射目标文件可以不存在。
4. **验证回归用例能失败**：临时把实现退回旧行为（或写反实现），确认新用例变红；zcmodel 的合并用例常用这种做法自检（例如把 `structSpecText` 换回子串搜索、把同落点排序注释掉）。
5. **检查副作用**：失败后确认目标文件逐字节不变、目录中没有 `*.tmp*` 残留；`writeAndVerify` 与 `snapshotFiles` 已覆盖这些断言。
6. **集成差异**：真实数据库路径优先核对 `zcdb` 的 `ColumnInfo`/`IndexInfo` 是否符合预期（类型原样、Extra、Default 指针），再怀疑生成端。

## 文档示例维护

`docs_examples_compile_test.go` 手工镜像 `quickstart.md` 中的三个示例（最小示例、zcdb 桥接、显式字段信息），并以外部包 `zcmodel_test` 做编译级核对；文档示例变更时必须同步该文件。文档中的 `go` 代码块还应能通过 `gofmt -e` 语法解析（整文件示例用独立文件校验）。

## 覆盖率

- `go test ./zcmodel -cover` 当前约 98%（以实测为准，README 记录快照）。
- 覆盖率下降时先用 `go tool cover -func` 定位函数，再对照新增分支判断是缺测试还是死代码；不要为了数字补无效断言。

## 相关

- 生成阶段与失败矩阵：[生成流水线](pipeline.md)
- 合并算法的可测边界：[骨架保持式合并](skeleton-merge.md)
- 文档 ↔ 测试对照：[代码地图](code-map.md)
