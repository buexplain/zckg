# 代码地图

> 返回 [zcmodel 文档地图](zcmodel.md)。本文把文档章节、源码符号与锁死测试对齐，便于“从文档跳到代码/测试”和反向审查。

## 追溯表

| 主题 | 文档 | 源码符号 | 锁死测试 |
|---|---|---|---|
| 唯一入口与端到端 | [README](README.md)、[quickstart](quickstart.md) | `generate.go` `Generate` | `TestGenerate`、`TestDocExamples_ManualInput` |
| 表名/文件名校验 | [api](api.md#错误表) | `generate.go` `validateTableName`、`resolveFileName`、`goFileNameConflict` | `TestGenerate_TableNameEscape`、`TestGenerate_FileNameConflictWithGoRules`、`TestGenerate_FileNameOverride`、`TestGenerate_InvalidFileName`、`TestGenerate_InvalidUTF8Name`、`TestGoFileNameConflict` |
| 输出路径与目录 | [pipeline](pipeline.md) | `generate.go` `isPathWithinDir`；`build.go` `packageNameOf` | `TestIsPathWithinDir_RelError`、`TestPackageNameOf`、`TestWriteOrReplaceStruct_PkgNameFromAbsDir`、`TestGenerate_EmptyOutputDir`、`TestGenerate_MkdirFail` |
| tag 名校验 | [api](api.md#错误表) | `generate.go` `validateColumnTagName` | `TestGenerate_InvalidColumnTagName` |
| 列复制与补全（显式优先） | [pipeline](pipeline.md) | `generate.go` `Generate` 中段；`columnTypeToGoType.go` `formatStructFieldType` | `TestGenerate_NoInputSideEffect`、`TestGenerate_AutoFillTimeImport`、`TestGenerate_CustomTypeImport`、`TestGenerate_UnknownColumnTypeFallback` |
| 字段名五项检查 | [api](api.md#错误表) | `generate.go` 字段名检查循环 | `TestGenerate_EmptyFieldName`、`TestGenerate_InvalidColumnNames`、`TestGenerate_ChineseColumnName`、`TestGenerate_DuplicateFieldNames`、`TestGenerate_UnusableFieldNames`、`TestGenerate_NilColumn` |
| 结构体/tag 组装 | [generated-code](generated-code.md)、[tags-ddl-index](tags-ddl-index.md) | `build.go` `buildStruct`、`buildStructDocComment`、`formatStructTag` | `TestBuildStruct_Entity`、`TestBuildStruct_DO`、`TestBuildStruct_CustomTagName`、`TestBuildStruct_EmptyComment`、`TestBuildStruct_EmptyColumnTagName`、`TestBuildStruct_CompositePrimaryKey`、`TestBuildStruct_CommentWithSpecialChars`、`TestBuildStruct_JsonTagValueWithSpecialChars`、`TestBuildStruct_JsonTagValueWithBacktick`、`TestBuildStruct_ColumnTagValueWithSpecialChars` |
| tag 转义/反引号 | [tags-ddl-index](tags-ddl-index.md) | `build.go` `formatStructTag` | `TestFormatStructTag`、`TestFormatStructTag_ReflectRestore` |
| ddl 片段 | [tags-ddl-index](tags-ddl-index.md) | `build.go` `assembleDDLFragment`、`formatDDLDefault`、`ddLExtraParts`、`precisionSuffix` | `TestAssembleDDLFragment_NullableTriState`、`TestAssembleDDLFragment_Default`、`TestAssembleDDLFragment_ExtraWhiteList`、`TestAssembleDDLFragment_TypePreservedVerbatim` |
| 索引块 | [tags-ddl-index](tags-ddl-index.md) | `build.go` `buildIndexCommentBlock` | `TestBuildIndexCommentBlock_Ordering`、`TestBuildIndexCommentBlock_Format`、`TestBuildIndexCommentBlock_Empty`、`TestBuildIndexCommentBlock_SymbolOnlyName`、`TestBuildStruct_EmptyColumnsWithIndexes` |
| doc 注释净化 | [tags-ddl-index](tags-ddl-index.md) | `build.go` `sanitizeCommentLine`、`buildStructDocComment` | `TestSanitizeCommentLine`、`TestBuildStruct_TableCommentNewlineRegression`、`TestBuildStruct_DocCommentQuotesSurviveGofmt` |
| 互转方法 | [generated-code](generated-code.md) | `build.go` `buildToDOMethod`、`buildToEntityMethod` | `TestBuildToDOMethod`、`TestBuildToEntityMethod` |
| 类型归一化与映射 | [types-and-naming](types-and-naming.md) | `columnTypeToGoType.go` `normalizeColumnType`、`makeColumnTypeToGoTypeMap`、`formatStructFieldType` | `TestNormalizeColumnType`、`TestNormalizeColumnTypeMatchesMap`、`TestFormatStructFieldType` |
| 命名转换 | [types-and-naming](types-and-naming.md) | `toCase.go` `SplitWords`、`ToPascalCase`、六个风格函数、`formatJSONTag`、`lowerFirst` | `TestSplitWords`、`TestToPascalCase`、`TestToCase_各命名风格`、`TestToCase_EmptyWordSkipped`、`TestFormatJSONTag`、`TestLowerFirst` |
| 生成文本拆分 | [skeleton-merge](skeleton-merge.md) | `build.go` `splitGeneratedCode`、`structSpecText` | `TestSplitGeneratedCode`、`TestStructSpecText`、`TestWriteOrReplaceStruct_BlockSpecTextWithKeywordInComment` |
| 骨架合并主流程 | [skeleton-merge](skeleton-merge.md)、[regeneration](regeneration.md) | `build.go` `writeOrReplaceStruct` | `TestWriteOrReplaceStruct_NewFile`、`TestWriteOrReplaceStruct_EmptyFile`、`TestWriteOrReplaceStruct_KeepUserCode`、`TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition`、`TestWriteOrReplaceStruct_KeepDeclLevelComments` |
| 区间/分隔符/注释归属 | [skeleton-merge](skeleton-merge.md) | `writeOrReplaceStruct` 的 `declStarts`、`lineCommentEnd`、`skipDeclSeparators` | `TestWriteOrReplaceStruct_KeepDeclLevelComments`、`TestWriteOrReplaceStruct_SemicolonSeparatedDecls`、`TestWriteOrReplaceStruct_SingleImportDocAndComment` |
| type 块处理 | [skeleton-merge](skeleton-merge.md) | `build.go` `renderTypeBlock`、`generatedTypeKind`、`specSpan` | `TestWriteOrReplaceStruct_MixedBlockDocAndTrailing`、`TestWriteOrReplaceStruct_KeepFreeFloatingCommentInMixedBlock`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockKeepsFreeComment`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockFreeCommentNoBlockDoc`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockLeadCommentNormalized`、`TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls` |
| 生成方法识别/去重 | [skeleton-merge](skeleton-merge.md) | `build.go` `receiverTypeName`、`isGeneratedMethod` | `TestWriteOrReplaceStruct_RemoveValueReceiverToDO`、`TestWriteOrReplaceStruct_RemoveParenthesizedValueReceiverToDO`、`TestWriteOrReplaceStruct_ValueReceiverCustomMethod`、`TestWriteOrReplaceStruct_NonIdentReceiverKept` |
| import 增删/别名/冲突 | [skeleton-merge](skeleton-merge.md#6-阶段-dimport-生命周期) | `build.go` `importPathLocalName`、`importLocalName`、`collectSelectorRefs`、`renderImport` | `TestImportLocalName`、`TestWriteOrReplaceStruct_RemoveUnusedImports`、`TestWriteOrReplaceStruct_ExistingFile_AliasImport`、`TestWriteOrReplaceStruct_ExistingFile_SameNameAliasImport`、`TestWriteOrReplaceStruct_ImportLocalNameConflict`、`TestWriteOrReplaceStruct_NoImportDeclAddMissing`、`TestWriteOrReplaceStruct_NamedImportWithDoc`、`TestWriteOrReplaceStruct_SingleImportDocAndComment` |
| 缺失声明落点/顺序 | [skeleton-merge](skeleton-merge.md#7-阶段-e缺失声明的落点与顺序) | `build.go` `beforeMap`、`unpaired`、`base` | `TestWriteOrReplaceStruct_MissingGenDeclsPairedInsertion`、`TestWriteOrReplaceStruct_MissingGenDeclsInMixedBlock`、`TestWriteOrReplaceStruct_MissingGenDeclsUnpairedFallback`、`TestWriteOrReplaceStruct_SharedInsertionOrder` |
| 落盘安全/原子写 | [pipeline](pipeline.md)、[generated-code](generated-code.md) | `build.go` `writeGeneratedFile`、`writeFileAtomic` | `TestWriteGeneratedFile_SyntaxError`、`TestWriteFileAtomic_StatErrorNonNotExist`、`TestWriteFileAtomic_CreateTempFails`、`TestWriteFileAtomic_RenameFails`、`TestWriteOrReplaceStruct_PreservePermissions`、`TestWriteOrReplaceStruct_ReadOnlyDir`、`TestWriteOrReplaceStruct_ReadFileFails` |
| package/build tags/无生成标记 | [generated-code](generated-code.md) | `build.go` `packageNameOf`、文件头保留；`buildFileContent` | `TestWriteOrReplaceStruct_KeepBuildTagsAndPackageComment`、`TestGenerate_KeepBuildTags`、`TestGenerate_NoGeneratedMarker`、`TestWriteOrReplaceStruct_PkgNameFromAbsDir` |
| 错误文本/无效输入 | [api](api.md#错误表) | `generate.go` 各校验函数；`build.go` 写文件错误包裹 | `TestGenerate_UnknownDialect`、`TestGenerate_EmptyOutputDir`、`TestGenerate_ExistingFileSyntaxError`、`TestWriteOrReplaceStruct_InvalidGeneratedCode` |
| 真实数据库桥接 | [quickstart](quickstart.md#4-配合-zcdb-schema-从真实数据库生成) | `integration_test.go` `bridgeColumns`、`bridgeIndexes` | `TestInteg_MySQL_AllTypes`、`TestInteg_Postgres_AllTypes`、`TestInteg_SQLite_AllTypes`、`TestInteg_MySQL_SampleBridge`、`TestInteg_SQLite_IndexChangeRegeneration` |
| 文档示例 | [quickstart](quickstart.md) | `docs_examples_compile_test.go` | `TestDocExamples_ManualInput` + 外部包编译级函数 |
| fuzz | [testing](testing.md) | `build_test.go`、`generate_test.go`、`columnTypeToGoType_test.go`、`toCase_test.go` | `FuzzGenerate`、`FuzzWriteOrReplaceStruct`、`FuzzAssembleDDLFragment`、`FuzzColumnTypeToGoType`、`FuzzNameConversion` |

## 维护约定

1. **代码引用用符号名，不写行号**：行号会随重构漂移；本文用 `文件` + `函数/类型` 定位，章节末尾的「实现/测试」同样如此。
2. **行为变更同步四处**：代码、源码注释、对应文档章节、锁死测试；新行为还要在本表增加一行。
3. **文档示例同步**：`quickstart.md` 的三个示例与 `docs_examples_compile_test.go` 一一对应，改动示例必须同步改测试镜像。
4. **测试命名**：新用例按被测源码归入 `generate_test.go` / `build_test.go` / `toCase_test.go` / `columnTypeToGoType_test.go` / `input_test.go`，不新建跨模块聚合文件。
5. **行号型引用仅允许出现在审查报告**：审查报告明确标注基线提交，属于历史快照；用户文档不写行号。

## 相关

- 术语与不变量：[glossary](glossary.md)
- 测试与排障：[testing](testing.md)
- 架构与取舍：[architecture](architecture.md)
