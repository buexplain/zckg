# 骨架保持式合并

> 返回 [zcmodel 文档地图](zcmodel.md)。这是 zcmodel 最核心、也最复杂的实现：目标文件已存在时，如何在不丢用户代码/注释的前提下替换生成声明。用户可见契约见 [增量再生成](regeneration.md)。

## 1. 问题与目标

整体重印 AST（`go/printer`）虽然简单，但会丢失或改写：

- 声明之间的**游离注释**（不附着于任何声明，如 `//go:generate`、分区注释）；
- 用户声明的**原始排列**与行尾注释的归属；
- 生成声明与用户声明交错时的位置关系。

因此实现选择“**骨架 + 槽位**”：

- **骨架**：原文件顶层元素序列（文件头、package 行、import、各顶层声明、文件尾注释）及其排列；
- **槽位（slot）**：骨架中的一个位置。生成声明占“生成槽位”，用户声明占“用户槽位”；
- 合并 = 生成槽位在原位替换为新文本，用户槽位原样输出（最终经 gofmt，行尾统一为 LF），缺失的生成声明按规则插入；
- 最后统一 `go/format` 格式化（缩进/对齐/import 排序），因此首次再生成可能同时出现用户区的格式类变更，之后稳定。

> 实现：`build.go` `writeOrReplaceStruct`；测试：`TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition`、`TestWriteOrReplaceStruct_KeepDeclLevelComments`。

## 2. 数据模型

| 名称 | 类型/形态 | 作用 |
|---|---|---|
| `genKind` | `genEntType` / `genToDO` / `genDoType` / `genToEntity` | 四类生成声明；枚举顺序即标准顺序 |
| `genText[genKind]` | 文本 | 四段生成声明的完整文本（含 Doc）；空串表示“不生成该声明” |
| `genSpecText[genKind]` | 文本 | 结构体的 Spec 形态（去掉 `type` 关键字），用于替换 `type (...)` 块内 Spec |
| `slots` | 有序切片 | 原文件顶层声明按原顺序映射成的槽位；生成槽位只保留前置游离注释前缀 |
| `seenGen` | `map[genKind]bool` | 已建立槽位的生成声明种类（重复出现时去重） |
| `carrierIdx` | `map[genKind]int` | 生成声明种类 → 承载它的槽位下标（混合块内多个种类指向同一个块槽位） |
| `beforeMap` | `map[int][]genKind` | 在某个槽位前插入的缺失生成声明 |
| `imports` / `existingImports` / `existingImportNames` | 切片/map | 保留的 import 声明、已导入路径、本地名 → 路径 |
| `removedSpecs` / `droppedImportDecls` | map | 要移除的 import spec / 整条 import 声明 |
| `pending` | 字符串 | 被移除声明之前的游离注释，顺延给下一个保留声明 |
| `tail` | 字符串 | 文件尾注释（含顺延到末尾的 `pending`） |
| `fileHeader` / `pkgClause` | 字符串 | package 之前的原文（build tags/文件级注释）与原 package 行 |

槽位示意（原文件 → 槽位序列）：

```text
原文件顶层：                     槽位：
package model                  [fileHeader + pkgClause]
import "fmt"                   [importEntry]
type Helper struct{}           [用户槽位]
type UserEntity struct{}       [生成槽位 genEntType]
func (e *UserEntity) ToDO()    [生成槽位 genToDO]
func (e *UserEntity) Extra()   [用户槽位]
type UserDO struct{}           [生成槽位 genDoType]
func (d *UserDO) ToEntity()    [生成槽位 genToEntity]
// EOF                         [tail]
```

## 3. 阶段 A：拆分生成代码

`Generate` 先拼出四段文本，再交给 `splitGeneratedCode` 按“结构体名 / 方法名”解析拆出：

- 结构体段 = 名为 `entityName`/`doName` 的 `type` 声明（含其 Doc 注释）；
- 方法段 = 名为 `ToDO`/`ToEntity` 的函数声明（含其 Doc 注释）；
- 解析失败返回 `生成代码存在语法错误`。

`structSpecText` 进一步把结构体文本转成块内 Spec 形态：按 AST 定位 `type` 关键字与类型名，删除 `type` 及其后的空白，保留 Doc。**不能用子串搜索**——Doc 注释或 tag 值里可能出现 `type <结构体名>` 字样，子串搜索会删错位置。

> 实现：`build.go` `splitGeneratedCode`、`structSpecText`；测试：`TestSplitGeneratedCode`、`TestStructSpecText`、`TestWriteOrReplaceStruct_BlockSpecTextWithKeywordInComment`。

## 4. 阶段 B：解析与区间计算

对存量文件执行 `parser.ParseFile(..., parser.ParseComments)`，失败直接返回 `解析文件失败`，**绝不覆盖**。随后计算：

- `declStarts[i]`：第 i 个声明的起点（含前置 Doc 注释）；
- `nextStart(i)`：下一个声明起点或文件末尾，作为同行尾注释的上限；
- `lineCommentEnd(end, limit)`：若声明 `End()` 之后同一行只隔空格/制表符/显式分号就紧跟注释组，则把注释并入本声明区间（`var X = 1 // c`、`type A struct{}; // c`）；
- `skipDeclSeparators(pos)`：跳过空格/制表符/显式分号，得到下一个声明的实际起点（`package p; var X = 1`、`type A struct{}; var X = 1` 中的分号只是分隔符，不进入产物）；
- 每个声明：`lead = 上一声明结束 → 本声明起点`（声明之间的游离注释），`body = 本声明起点 → 本声明结束（含同行尾注释）`；
- `fileHeader` = package 之前的原文；`pkgClause` = 原 package 行（含同行尾注释）；`tail` = 最后一个声明之后的内容 + 未被认领的 `pending`。

> 实现：`build.go` `writeOrReplaceStruct` 中的区间计算；测试：`TestWriteOrReplaceStruct_KeepDeclLevelComments`、`TestWriteOrReplaceStruct_SemicolonSeparatedDecls`、`TestWriteOrReplaceStruct_SingleImportDocAndComment`、`TestWriteOrReplaceStruct_RemoveUnusedImports`（CRLF/同行 import 行删除）。

## 5. 阶段 C：槽位分类

逐声明分类（**按原顺序**）：

| 声明形态 | 判定 | 处理 |
|---|---|---|
| import 声明 | `GenDecl.Tok == IMPORT` | 若整条被移除 → 其前置游离注释进 `pending`（声明自身的 Doc 随声明移除）；否则收进 `imports`，记录已导入路径与本地名 |
| 纯用户 type 块 | 块内无生成 Spec | 整块原位保留 |
| 仅含过期生成 Spec 的块 | 块内只有 `genText` 为空的生成 Spec | 整块移除，前置注释进 `pending` |
| 含用户类型 + 过期生成 Spec | `genCount == 0 && userCount > 0` | 块保留，只剔除过期 Spec（`renderTypeBlock`） |
| **单 Spec 生成块/声明** | `len(Specs)==1 && 无用户 Spec && 无过期 Spec` | 独立声明原位替换；单 Spec **括号块**通常展开为独立声明（不保留括号块），**例外**见下 |
| 混合 type 块 | 其余含生成 Spec 的块 | 块保留，生成 Spec 就地替换、过期/重复 Spec 剔除、用户 Spec 与游离注释保留 |
| 生成方法 | 接收者为 Entity 且名 `ToDO`，或接收者为 DO 且名 `ToEntity` | `genText` 为空或该种类已出现 → 移除（注释进 `pending`）；否则生成槽位 |
| 其他声明（用户方法、var/const、未知） | 默认 | 用户槽位，原样保留 |

**单 Spec 块的例外**：仅当块内**生成 Spec 区间之外没有其他注释**（无块级 Doc、无块内游离注释、无 `(`/`)` 同行注释）时才展开；存在这类注释则保留括号块，以免展开时丢失注释。

**重复生成声明**：同一 `genKind` 只认首个槽位（块内为首个 Spec），其余重复声明/重复 Spec 移除；被移除声明前的游离注释经 `pending` 顺延给下一个保留声明。生成声明的**自身** Doc 与行尾注释属于被替换区间，随之丢弃。

> 实现：`build.go` `renderTypeBlock`、`generatedTypeKind`、`isGeneratedTypeSpec`、`isGeneratedMethod`、`receiverTypeName`；测试：`TestWriteOrReplaceStruct_MixedBlockDocAndTrailing`、`TestWriteOrReplaceStruct_KeepFreeFloatingCommentInMixedBlock`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment`、`TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls`、`TestWriteOrReplaceStruct_RemoveValueReceiverToDO`。

## 6. 阶段 D：import 生命周期

预扫描两类选择器引用（只统计 `id.Obj == nil` 的标识符，避免局部变量遮蔽包名时误判）：

- `oldGenRefs`：旧生成声明中的 `pkg.X` 引用；
- `usedRefs`：保留的用户声明 + 新生成代码中的引用。

然后：

| 场景 | 处理 |
|---|---|
| 需要的路径已在文件中以**无别名**导入 | 视为已存在，不追加 |
| 以**不同名具名别名**（或 `_`、`.` 导入）导入所需包 | 不视为已存在，追加默认导入（生成代码按默认包名引用） |
| 别名恰好等于默认包名（如 `time "time"`） | 已满足生成代码，不重复追加 |
| 同一本地名已绑定到**其他路径** | 返回 `生成结构体失败: import 别名冲突: ...`，不写文件 |
| 旧生成代码引用过、新生成代码与用户代码都不再引用，且不在 `neededImports` | 移除该 spec；块内全部移除时整条 import 声明移除（前置注释 `pending` 顺延） |
| `_`、`.` 导入、`"C"`、路径末段无法确定包名（如 `gopkg.in/yaml.v3`、主版本后缀 `/v2`） | 不做移除（包名不可知，无法安全判断） |
| 缺失 import | 合并进**第一个保留的 import 块**；没有 import 声明时新建一条 |

> 实现：`build.go` `importPathLocalName`、`importLocalName`、`collectSelectorRefs`、`writeOrReplaceStruct` 的 import 段、`renderImport`；测试：`TestImportLocalName`、`TestWriteOrReplaceStruct_RemoveUnusedImports`、`TestWriteOrReplaceStruct_ExistingFile_AliasImport`、`TestWriteOrReplaceStruct_ExistingFile_SameNameAliasImport`、`TestWriteOrReplaceStruct_ImportLocalNameConflict`。

## 7. 阶段 E：缺失声明的落点与顺序

配对关系：**Entity 结构体 ↔ `ToDO`**，**DO 结构体 ↔ `ToEntity`**。

| 缺失声明 | 落点 |
|---|---|
| 结构体缺失、其方法存在 | 插在该方法**前** |
| 方法缺失、其结构体存在 | 插在该结构体**后** |
| 两个都缺或都无配对 | 追加到“最后一个承载生成声明的槽位”之后（没有任何生成声明时置于首个用户声明之前） |
| 结构体在同一个 `type (...)` 块内 | 方法缺失补在**整个块之后**，不插入块内 |

同一落点上可能同时有配对插入与无配对追加：合并后**按 `genKind` 标准顺序排序**（Entity 结构体 → ToDO → DO 结构体 → ToEntity），配对关系只决定插入位置、不决定同位置内的先后。因此当块内结构体把配对方法“钉”在块后、而更靠前的生成声明也缺失时，完整标准顺序不保证，但同一落点内插入的声明仍按标准顺序排列。

示例（块内只有 DO，Entity、ToDO、ToEntity 均缺失）：

```text
type (                 ← 块保留
    UserDO struct { ... }
)

type UserEntity ...    ← 同一落点，按标准顺序
func (e *UserEntity) ToDO() ...
func (d *UserDO) ToEntity() ...
```

> 实现：`build.go` `beforeMap`、`unpaired`、`base`、排序段；测试：`TestWriteOrReplaceStruct_MissingGenDeclsPairedInsertion`、`TestWriteOrReplaceStruct_MissingGenDeclsInMixedBlock`、`TestWriteOrReplaceStruct_MissingGenDeclsUnpairedFallback`、`TestWriteOrReplaceStruct_SharedInsertionOrder`。

## 8. 阶段 F：组装与落盘

按骨架顺序输出：

```text
fileHeader + pkgClause + "\n"
+ 每个保留的 import 声明（缺失项合并进第一条）
+ 按槽位顺序输出：beforeMap 插入 → 槽位文本（生成槽位 = 前缀注释 + genText）
+ tail（文件尾注释）
```

槽位之间统一一个空行；随后 `writeGeneratedFile` 执行 `go/format.Source` 自校验与格式化，`writeFileAtomic` 临时文件 + rename。完整产物必须能通过 `go/format`，否则报 `生成代码存在语法错误` 且不落盘。

> 实现：`build.go` `writeOrReplaceStruct` 尾部、`writeGeneratedFile`、`writeFileAtomic`；测试：`TestWriteOrReplaceStruct_NewFile`、`TestWriteOrReplaceStruct_EmptyFile`、`TestWriteGeneratedFile_SyntaxError`、`TestWriteFileAtomic_*`。

## 9. 失败与不变量

- 存量文件解析失败、生成代码拆分失败、最终 gofmt 失败、临时文件/rename 失败：一律返回错误，**目标文件逐字节不变**（临时文件由 defer 清理）。
- 生成声明按名字识别：同名的用户类型会被当作生成声明替换；同名的用户方法（值/指针接收者、含括号包裹的接收者）会被当作生成方法替换。
- 生成声明的自身 Doc/尾注释丢弃；声明之间的游离注释、用户声明 Doc/尾注释、文件尾注释保留（重复/移除声明的游离注释顺延）。
- 同一 `genKind` 最多一个槽位；同一落点的插入按标准顺序。
- 所有 tag/注释净化、import 增删、落盘自校验规则不因合并而改变。

## 10. 边界组合表

| 存量文件形态 | 新生成代码变化 | 结果 | 锁死测试 |
|---|---|---|---|
| 独立生成声明 | 字段增删 | 原位替换；import 相应增删 | `TestGenerate_KeepUserCode`、`TestWriteOrReplaceStruct_RemoveUnusedImports` |
| 生成声明之间夹用户方法 | 任意 | 用户方法原位保留 | `TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition` |
| 混合 `type (...)` 块 | 字段变化 | 块保留，生成 Spec 原位替换 | `TestWriteOrReplaceStruct_MixedBlockDocAndTrailing` |
| 单 Spec 块 + 块级 Doc/游离/括号注释 | 任意 | 保留括号块 | `TestWriteOrReplaceStruct_SingleSpecTypeBlockKeepsFreeComment`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment` |
| 单 Spec 块、无其他注释 | 任意 | 展开为独立声明 | `TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized` |
| 重复生成声明 | 任意 | 首个保留，其余移除 | `TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls` |
| 缺失声明（各种组合） | 首次引入 | 按配对/无配对落点插入 | `TestWriteOrReplaceStruct_MissingGenDecls*`、`TestWriteOrReplaceStruct_SharedInsertionOrder` |
| import 别名/`_`/`.` | 新增依赖 | 追加默认导入 | `TestWriteOrReplaceStruct_ExistingFile_AliasImport` |
| 同名别名 `time "time"` | 新增时间列 | 不重复追加 | `TestWriteOrReplaceStruct_ExistingFile_SameNameAliasImport` |
| 同本地名绑定其他路径 | 新增依赖 | 报冲突、不写文件 | `TestWriteOrReplaceStruct_ImportLocalNameConflict` |
| 局部变量遮蔽包名 | 删除依赖 | 移除未使用 import | `TestWriteOrReplaceStruct_RemoveUnusedImports` |
| 显式分号分隔声明 | 任意 | 正常合并，不残留分号 | `TestWriteOrReplaceStruct_SemicolonSeparatedDecls` |
| 语法错误的存量文件 | 任意 | 报 `解析文件失败`，不覆盖 | `TestGenerate_ExistingFileSyntaxError` |
