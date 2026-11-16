# 术语与不变量

> 返回 [zcmodel 文档地图](zcmodel.md)。前半是术语表，后半是代码/测试必须保持的行为不变量。

## 术语表

| 术语 | 定义 |
|---|---|
| 生成声明 | 由生成器负责的四个顶层声明：Entity 结构体、`ToDO` 方法、DO 结构体、`ToEntity` 方法 |
| 生成区 / 用户区 | 生成声明占据的槽位 / 用户声明占据的槽位；再生成只替换前者 |
| 骨架 | 原文件顶层元素序列（文件头、package 行、import、各顶层声明、文件尾注释）及其排列 |
| 槽位（slot） | 骨架中的一个位置；生成槽位在原位替换，用户槽位原样输出 |
| Spec | `type (...)` 块内的单个类型声明（不含 `type` 关键字）；块内替换以 Spec 为粒度 |
| 载体块（carrier） | 承载生成 Spec 的 `type (...)` 块；混合块的多个 `genKind` 指向同一个块槽位 |
| 配对落点 | Entity 结构体 ↔ `ToDO`、DO 结构体 ↔ `ToEntity` 的相互补位规则 |
| 同落点 | 多个缺失生成声明落在同一个插入位置；合并后按 `genKind` 标准顺序排序 |
| 游离注释 | 不附着于任何声明的注释（声明之间、文件尾、`(`/`)` 同行等） |
| 声明区间 | `上一声明结束 → 本声明结束`（含同行尾注释）；被替换生成声明的自身区间含旧 Doc/尾注释 |
| `lead` / `pending` / `tail` | 声明前的游离注释 / 被移除声明顺延的注释 / 文件尾注释 |
| 显式优先 | `StructFieldInfo` 中调用方给定的值优先，`Generate` 只补空字段 |
| `ddl` 片段 | 重组的类 DDL 列定义文本（类型 / NULL / DEFAULT / EXTRA），不是可执行 DDL |
| 索引块 | 渲染到 Entity/DO doc 注释中的索引清单（主键/唯一/普通） |
| 元数据快照 | `ddl`、`primary_key`、索引块来自生成时传入的元数据，不会自动刷新 |
| 生成标记 | Go 官方 `// Code generated ... DO NOT EDIT.`；zcmodel 刻意不写 |

## 不变量

以下行为被实现与测试锁死；修改需同时更新代码、注释、文档、测试。

1. **不写生成标记**：产物不包含官方 `// Code generated`，否则 lint/staticcheck 会整体跳过含用户代码的文件。实现：`build.go` `buildFileContent`；测试：`TestGenerate_NoGeneratedMarker`。
2. **产物必须可 gofmt**：落盘前对完整内容执行 `go/format.Source`，非法产物直接报错、不写文件。实现：`build.go` `writeGeneratedFile`；测试：`TestWriteGeneratedFile_SyntaxError`、`TestWriteOrReplaceStruct_InvalidGeneratedCode`。
3. **原子写与权限**：同目录临时文件 + rename；保留目标原权限位，新建用 `0644`。实现：`build.go` `writeFileAtomic`；测试：`TestWriteFileAtomic_*`、`TestWriteOrReplaceStruct_PreservePermissions`（Windows Skip）。
4. **存量文件解析失败不覆盖**：`go/parser` 失败直接报错，原文件逐字节不变。实现：`build.go` `writeOrReplaceStruct`；测试：`TestGenerate_ExistingFileSyntaxError`。
5. **生成声明按名字识别且唯一**：Entity/DO 类型与 `ToDO`/`ToEntity` 方法按名匹配；同一 `genKind` 只保留首个槽位；同名的用户类型/值接收者方法会被替换。实现：`build.go` `generatedTypeKind`、`isGeneratedMethod`；测试：`TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls`、`TestWriteOrReplaceStruct_RemoveValueReceiverToDO`。
6. **用户代码与注释原位保留**：用户声明、其 Doc/尾注释、声明间游离注释、文件尾注释不被搬动；例外是被替换生成声明自身的 Doc/尾注释，以及被移除重复声明的游离注释顺延。实现：`build.go` `writeOrReplaceStruct` 槽位循环；测试：`TestWriteOrReplaceStruct_KeepDeclLevelComments`、`TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition`。
7. **单 Spec 块展开条件**：仅当生成 Spec 区间外没有其他注释（块级 Doc、块内游离注释、`(`/`)` 同行注释）时才展开为独立声明。实现：`build.go` 单 Spec 分支；测试：`TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment`。
8. **缺失声明落点**：结构体补在方法前、方法补在结构体后；两个都缺/无配对追加到最后一个生成声明之后；同落点按标准顺序。实现：`build.go` `beforeMap`/排序；测试：`TestWriteOrReplaceStruct_MissingGenDecls*`、`TestWriteOrReplaceStruct_SharedInsertionOrder`。
9. **import 安全**：生成所需包缺失则补（合并进第一个保留块）；同名别名不重复；本地名冲突报错不写文件；只移除“旧生成代码引用过、且新生成代码与保留用户代码都不再引用”的 import；`_`、`.`、`"C"`、包名不可知者不移除。实现：`build.go` import 段；测试：`TestWriteOrReplaceStruct_RemoveUnusedImports`、`TestWriteOrReplaceStruct_ImportLocalNameConflict`、`TestImportLocalName`。
10. **tag 与注释安全**：tag 值统一 `strconv.Quote`，含反引号时整条 tag 改用解释型字符串；doc 注释换行净化并在成对引号间插入 U+200B 防止 gofmt 改写。实现：`build.go` `formatStructTag`、`sanitizeCommentLine`；测试：`TestFormatStructTag_ReflectRestore`、`TestBuildStruct_DocCommentQuotesSurviveGofmt`。
11. **Nullable/Default 指针语义**：`Nullable == nil` 省略 NULL 标记；`Default != nil` 追加 DEFAULT 段，空串渲染 `DEFAULT ''`。实现：`build.go` `assembleDDLFragment`、`formatDDLDefault`；测试：`TestAssembleDDLFragment_NullableTriState`、`TestAssembleDDLFragment_Default`。
12. **类型映射与兜底**：先归一化再查方言表，未命中兜底 `string`；`time.Time` 在 `Import` 为空时自动补 `time`。实现：`columnTypeToGoType.go`、`generate.go`；测试：`TestFormatStructFieldType`、`TestGenerate_AutoFillTimeImport`。
13. **ToEntity 跳过失败断言**：DO 字段为 `any`，断言失败（含 nil）跳过并保留 Entity 原值，不返回错误。实现：`build.go` `buildToEntityMethod`；测试：`TestBuildToEntityMethod`。
14. **元数据是快照**：`ddl`/`primary_key`/索引块不自动刷新，表结构变化需重新 `Generate`。实现：`generate.go` `Generate` 透传 `Column`/`Indexes`；测试：`TestGenerate_SampleWithMetadataGolden`、`TestGenerate_LegacyInputDelta`。
15. **表名/文件名主动校验**：空名、非法 UTF-8、`.`/`..`、路径分隔符、Windows 非法字符、首字符、导出结构体名前缀、Go 文件名构建规则都在写文件前拒绝。实现：`generate.go` `validateTableName`、`resolveFileName`、`goFileNameConflict`；测试：`TestGenerate_TableNameEscape`、`TestGenerate_FileNameConflictWithGoRules`、`TestGenerate_InvalidFileName`、`TestGoFileNameConflict`。

## 相关

- 合并算法细节：[骨架保持式合并](skeleton-merge.md)
- 文档 ↔ 代码 ↔ 测试对照：[代码地图](code-map.md)
