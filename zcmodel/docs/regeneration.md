# 增量再生成

> 返回 [zcmodel 文档地图](zcmodel.md)。本文是**用户可见的再生成契约**：什么时候调用、会替换什么、会保留什么、有哪些边界。算法实现见 [骨架保持式合并](skeleton-merge.md)。

## 契约一句话

目标文件已存在时，`Generate` 以原文件的顶层声明序列为骨架：**生成声明在原位替换为新内容，用户声明与注释保留在原位置**，整个文件最终经 gofmt 统一格式化（行尾统一为 LF，CRLF 输入再生成后变为 LF）。因此可以在生成文件里直接写业务方法，表结构变化后重新生成即可。

## 会替换 / 会保留什么

| 内容 | 再生成行为 |
|---|---|
| Entity / DO 结构体 | 原位替换为新字段、tag、doc 注释 |
| `ToDO` / `ToEntity` 方法 | 原位替换（含用户手写的值接收者版本，避免同名方法共存） |
| 用户自定义方法（Entity/DO 上或其他类型） | 内容与顺序原位保留，不搬到生成代码之后 |
| 声明之间的游离注释、`//go:generate` 等指令 | 原位保留 |
| 用户声明的同行尾注释、文件尾注释 | 保留 |
| package 之前的 build tags / 文件级注释、原 package 行 | 原样前置保留（包名不被目录名覆盖） |
| 被替换的生成声明**自身**的旧 Doc / 尾注释 | 随声明一起丢弃 |
| 重复的生成声明（同名同种类） | 首个保留，其余移除；其前置游离注释顺延给下一个保留声明 |
| 缺失的生成声明 | 按配对规则补入（结构体补在方法前、方法补在结构体后） |
| import | 缺则补、旧生成代码不再引用则删；`_`、`.`、`"C"`、包名不可知的 import 不删；本地名冲突报错 |
| 生成标记 | 不写 `// Code generated ... DO NOT EDIT.`（产物含用户代码，需保留静态检查） |

## 例子

### 例 1：用户方法原位保留

再生成前（`Between` 被刻意放在 Entity 与 `ToDO` 之间）：

```text
type UserEntity struct {
    ID int
}

// 用户方法：插在 Entity 结构体与 ToDO 之间
func (e *UserEntity) Between() int {
    return e.ID
}

func (e *UserEntity) ToDO() {}
```

再生成后（Entity 与 `ToDO` 被替换，`Between` 仍在原位置）：

```text
type UserEntity struct {
    ID int64
}

// 用户方法：插在 Entity 结构体与 ToDO 之间
func (e *UserEntity) Between() int {
    return e.ID
}

func (e *UserEntity) ToDO() {}
```

> 测试：`TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition`。

### 例 2：新增列自动补 import

```text
新增 datetime 列前：             新增 datetime 列后：
package model                    package model

import "fmt"                     import (
                                     "fmt"
                                     "time"
                                 )
```

`time` 会被合并进**第一个保留的 import 块**，不会形成两个 import 块；如果文件里已有 `time "time"`（同名别名），则不再重复追加。若同一本地名已绑定到其他路径（如 `time "other/time"`），直接报 `import 别名冲突` 且不写文件——绝不产出重复绑定的不可编译文件。

> 测试：`TestWriteOrReplaceStruct_ExistingFile_AutoAddTimeImport`、`TestWriteOrReplaceStruct_ExistingFile_SameNameAliasImport`、`TestWriteOrReplaceStruct_ImportLocalNameConflict`。

### 例 3：删列自动移除不再引用的 import

```text
删去最后一个时间列前：            删去后：
import "time"                    （import 声明整体移除）
```

只有“旧生成代码引用过、新生成代码与保留的用户代码都不再引用、且不是 `_`/`.`/`"C"`/包名不可知”的 import 才会被移除；用户代码仍在用则保留。

> 测试：`TestWriteOrReplaceStruct_RemoveUnusedImports`。

### 例 4：单 Spec 括号块与注释

```text
type ( // AFTER-LPAREN
    UserEntity struct {
        ID int
    }
) // AFTER-RPAREN
```

块内生成 Spec 会被替换，但 **`(`/`)` 同行注释、块级 Doc、块内游离注释**会让实现保留括号块，而不是展开成独立声明（展开会丢注释）。只有“生成 Spec 区间外没有任何其他注释”的单 Spec 块才会展开为独立声明。

> 测试：`TestWriteOrReplaceStruct_SingleSpecTypeBlockTrailingComment`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockKeepsFreeComment`、`TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized`。

### 例 5：混合 `type (...)` 块

用户把 Entity/DO 与自定义类型放进同一个 `type (...)` 块时，块形态保留：生成 Spec 就地替换，用户类型与游离注释保留；缺失的生成方法补在**整个块之后**（不插入块内）。因此当更靠前的生成声明也缺失时，完整标准顺序不保证——同一落点内插入的声明仍按 Entity → ToDO → DO → ToEntity 排列。

> 测试：`TestWriteOrReplaceStruct_MissingGenDeclsInMixedBlock`、`TestWriteOrReplaceStruct_SharedInsertionOrder`。

## 边界与例外

1. **按名字识别生成声明**：Entity/DO 类型与 `ToDO`/`ToEntity` 方法按名字匹配。不要在同一文件中定义同名类型；否则再生成时会被当作生成声明替换。用户手写的**值接收者** `ToDO` 也会被替换（避免与生成方法同名共存）。
2. **生成声明自身的注释属于被替换区间**：直接在生成结构体上方写 `// 说明` 会被视为它的 Doc，再生成时丢弃；需要长期保留的注释请放在声明之间（与生成声明隔一个空行）或写到用户声明上。
3. **存量文件必须语法正确**：解析失败直接报错，不覆盖原文件。显式分号分隔声明（如 `type A struct{}; var X = 1`、`package p; var X = 1`）是合法的，会正常处理，分号不进入产物。
4. **首次再生成可能重排用户区格式**：整个文件经 gofmt 统一格式化（缩进、对齐、import 排序、声明间空行，行尾 CRLF 规范为 LF），首次 diff 可能包含格式类变化；之后保持稳定（再生成幂等）。
5. **块边界不是完全自由**：结构体在 `type (...)` 块内时，其方法只能补在块后，无法插到块内结构体紧邻位置。
6. **生成声明缺失时的插入顺序**：结构体缺失补在其方法前、方法缺失补在其结构体后；两个都缺或都无配对时追加到最后一个生成声明之后；同一落点内按标准顺序排序。
7. **写文件失败不覆盖**：解析失败、生成文本非法、临时文件/rename 失败都返回错误，目标文件逐字节不变；临时文件被清理。落盘前还有 gofmt 自校验（见 [生成流水线](pipeline.md)）。

## 相关

- 算法与数据模型：[骨架保持式合并](skeleton-merge.md)
- 落盘与失败矩阵：[生成流水线](pipeline.md)
- 产物整体契约：[生成代码契约](generated-code.md)
