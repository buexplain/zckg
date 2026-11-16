# 生成流水线

> 返回 [zcmodel 文档地图](zcmodel.md)。本文按执行顺序拆解 `Generate`：每阶段做什么、失败时有什么副作用。合并算法本身见 [骨架保持式合并](skeleton-merge.md)。

## 阶段总览

| 阶段 | 代码位置 | 输入/输出 | 失败副作用 |
|---|---|---|---|
| 0 输入校验 | `generate.go` `Generate` 前半 | `Input` → 合法化 | 无（不建目录、不写文件） |
| 1 复制与补全列 | `Generate` 中段 | `[]*Column` → 内部 `[]Column` | 无 |
| 2 字段名检查 | `Generate` 中段 | 补全后的列 → 通过/报错 | 无 |
| 3 组装生成文本 | `build.go` `buildStruct`、`buildToDOMethod`、`buildToEntityMethod` | 列 → Entity/DO/方法文本 + import 列表 | 无 |
| 4 输出路径 | `Generate` 后段 | `OutputDir`/`FileName` → 目标路径 | `OutputDir` 为空报错；`MkdirAll` 可能创建目录 |
| 5 合并与落盘 | `writeOrReplaceStruct` → `writeGeneratedFile` → `writeFileAtomic` | 目标文件 → 新内容 | 既有文件不被覆盖；临时文件被清理 |

**首错即返回**：阶段 0—3 的检查按固定顺序执行，遇到第一个错误立即返回；因此错误优先级是稳定的，且不会产生部分产物。

## 阶段 0：输入校验

顺序如下（与错误表一致，见 [对外 API](api.md#错误表)）：

1. `Dialect` ∈ {mysql, postgres, sqlite}；
2. 表名：非空、合法 UTF-8、不是 `.`/`..`、不含路径分隔符与 Windows 非法字符、首字符为 ASCII 字母或下划线；
3. 表名推导的结构体名前缀：`ToPascalCase(TableName)` 必须是合法且**导出**的 Go 标识符（拒绝 `public.users`、`user$x`、`_2fa`、`_用户`）；
4. 文件名：显式 `FileName` 或回退表名；拒绝非法 UTF-8、`.`/`..`、非法字符、`.go` 后缀，以及会触发 Go 文件名构建规则的名字（`_` 开头、`_test` 后缀、`_GOOS`/`_GOARCH`/`_GOOS_GOARCH` 后缀）；
5. `ColumnTagName`：非空时须能作为 struct tag 的 key（不得含空格/控制字符、`:`、`"`、反引号、DEL）；
6. 列切片：任一元素为 nil 报错（下标从 0 起）。

> 复制列之后还会校验 `JsonTagValueCase`（见阶段 1）。

> 表名与文件名的校验是“主动拒绝”而不是依赖操作系统/编译器：路径分隔符与 Windows 非法字符同时防路径穿越；首字符与导出标识符检查避免产出无法编译或无法映射的结构体名。

## 阶段 1：复制与补全列

`Generate` 先把 `[]*Column` 复制成内部 `[]Column`，后续补全只作用于副本，**不修改调用方数据**（因此重复调用结果稳定）。复制时任一元素为 nil 报错；随后校验 `JsonTagValueCase`（非空且非法 → `不支持的 json tag 命名风格`）。补全规则是“显式优先”：

| 字段 | 留空时的行为 |
|---|---|
| `StructFieldInfo.JsonTagValue` | `JsonTagValueCase` 非空时按风格生成；`JsonTagValueCase` 为空则不填（不生成 json tag） |
| `StructFieldInfo.Name` | `ToPascalCase(列名)` |
| `StructFieldInfo.Type` | `formatStructFieldType(方言, Column.Type)`；未命中兜底 `string` |
| `StructFieldInfo.Import` | 仅当 `Type == "time.Time"` 且 `Import` 为空时补 `"time"`；其余不引入包 |

## 阶段 2：字段名检查

五项检查都前置报错（附列名，避免落盘自校验才暴露根因）：

1. 字段名为空（列名经 `ToPascalCase` 后为空，如纯分隔符）；
2. 不是合法 Go 标识符（数字开头等）；
3. 不是导出标识符（中文列名、显式 `_`）——未导出字段会被 json 与 zcdb 映射静默跳过，`_` 也无法在互转方法中引用；
4. 与生成方法同名（`ToDO`/`ToEntity`）；
5. 与其他列转换后重名（如 `user_id` 与 `userId`）。

## 阶段 3：组装生成文本

- `buildStruct` 生成 Entity（具体类型）与 DO（`any`）结构体：doc 注释（表注释 + 索引块）、字段 tag、对齐；
- `buildToDOMethod`/`buildToEntityMethod` 生成互转方法；
- `neededImportsOf` 汇总 `StructFieldInfo.Import`，去重排序；
- 生成文本只做字符串拼装，最终由 gofmt 统一格式；此时不解析、不落盘。

## 阶段 4：输出路径

- `OutputDir` 为空 → `输出目录为空`；
- `os.MkdirAll(OutputDir, 0755)`，失败 → `创建输出目录失败: ...`；
- `filePath = filepath.Join(OutputDir, fileName+".go")`，再用 `isPathWithinDir` 做防御性二次校验（清理后仍须位于输出目录内），失败 → `输出文件路径逃逸输出目录: ...`。

## 阶段 5：合并与落盘

- 文件不存在或内容为空白：`buildFileContent` 生成 `package` + import + 生成代码，直接走落盘；
- 存量文件：解析 + 骨架保持式合并（见 [skeleton-merge.md](skeleton-merge.md)）；
- 所有写文件错误统一包裹为 `生成结构体失败: ...`。

### 落盘安全三重防线

1. **落盘前语法自校验**：`writeGeneratedFile` 在写出前对完整文件内容执行 `go/format.Source`（内部含 `go/parser` 解析），任何语法非法的产物都直接报错、不落盘；
2. **原子写入**：`writeFileAtomic` 在同目录创建临时文件写入后 rename 覆盖目标（Windows 上 rename 可直接覆盖已存在文件），进程中断/磁盘满不留下截断的目标文件，且保留目标文件原权限位（不存在时用 `0644`）；
3. **存量文件解析失败不覆盖**：已有文件无法被 `go/parser` 解析时直接报错返回，绝不覆盖用户代码。

### 失败矩阵

| 失败点 | 结果 |
|---|---|
| 阶段 0—3 任一校验 | 返回错误；不创建输出目录、不写文件 |
| `MkdirAll` 失败 | `创建输出目录失败`；已有目录/文件不变 |
| 输出路径逃逸 | `输出文件路径逃逸输出目录`；不写文件 |
| 存量文件读取失败 | 原始错误经 `生成结构体失败` 包裹；不写文件 |
| 存量文件解析失败 | `生成结构体失败: 解析文件失败: ...`；原文件逐字节不变 |
| 生成代码拆分/解析失败 | `生成结构体失败: 生成代码存在语法错误: ...`；不写文件 |
| `go/format.Source` 失败 | `生成结构体失败: 生成代码存在语法错误: ...`；不写文件 |
| 创建/写入/设置权限/关闭临时文件失败 | 对应 `...失败` 包裹；临时文件被 defer 清理，目标不变 |
| rename 失败 | `替换目标文件失败: ...`；目标不变，临时文件被清理 |

> 权限相关用例在 Windows 上按平台条件 Skip（见 [测试与排障](testing.md)）。

## 相关

- 槽位、落点、import、注释的算法：[骨架保持式合并](skeleton-merge.md)
- 再生成的用户可见契约：[增量再生成](regeneration.md)
- 错误文本与触发条件：[对外 API](api.md)
