# 对外 API

> 返回 [zcmodel 文档地图](zcmodel.md)。本文是 `zcmodel` 包的完整对外契约：唯一的生成入口、输入结构、枚举与错误表。

## Generate

```go
func Generate(input Input) error
```

唯一生成入口。根据 `Input` 生成 Entity/DO 结构体及互转方法，写入 `{OutputDir}/{文件名}.go`（文件名取 `Input.FileName`，为空时取 `TableName`）。

- 成功返回 `nil`，产物保证通过 `go/format`，且以原子方式落盘。
- 失败返回普通 `error`（`fmt.Errorf`/`errors.New` 构造，**没有**导出的哨兵错误，不适用 `errors.Is` 分类）；校验类错误文本稳定（见下方错误表）；写文件类错误在固定前缀 `生成结构体失败:` 后附带底层错误原文，随 OS/环境变化，请勿按完整文本匹配。
- 校验/组装阶段失败不创建输出目录、不写文件；写文件阶段失败不覆盖原文件（详见 [生成流水线](pipeline.md) 与 [骨架保持式合并](skeleton-merge.md)）。

### 错误表

| 错误信息 | 触发场景 |
|---|---|
| `不支持的数据库方言: xxx` | `Dialect` 不是 mysql / postgres / sqlite |
| `表名为空` | `TableName` 为空字符串 |
| `表名非法: xxx` | `TableName` 为 `.` 或 `..` |
| `表名不是合法的 UTF-8: xxx` | `TableName` 含非法 UTF-8 字节（Windows 会替换为 U+FFFD，导致落盘路径与期望不一致） |
| `表名包含非法字符: xxx` | `TableName` 含路径分隔符（`/`、`\`）或 Windows 非法文件名字符（`< > : " \| ? *`） |
| `表名首字符必须为 ASCII 字母或下划线: xxx` | `TableName` 首字符为数字或非 ASCII 字符（如中文表名），会推导出无法编译的结构体名 |
| `表名 xxx 推导出的结构体名前缀 xxx 不是合法的导出 Go 标识符` | 首字符合法但 `ToPascalCase(TableName)` 不是导出标识符：含 `.`（如 schema 限定名 `public.users`）、含 `$`（如 `user$x`）、下划线后接数字（`_2fa` → `2fa`）或中文（`_用户` → `用户`，未导出） |
| `表名 xxx 用作文件名 xxx.go 会触发 Go 文件名规则（原因），请通过 Input.FileName 指定文件名` | `FileName` 为空且表名会触发 Go 文件名构建规则：`_` 开头（被 go 工具忽略）、`_test` 后缀（成为测试文件）、`_GOOS` / `_GOARCH` / `_GOOS_GOARCH` 后缀（如 `order_linux`，成为平台构建约束）。以 `.` 开头的表名在此前已被「表名首字符」校验拒绝，不进入本分支 |
| `文件名非法: xxx` | 显式 `FileName` 为 `.` 或 `..` |
| `文件名不是合法的 UTF-8: xxx` | 显式 `FileName` 含非法 UTF-8 字节 |
| `文件名包含非法字符: xxx` | 显式 `FileName` 含路径分隔符或 Windows 非法文件名字符 |
| `文件名不应包含 .go 扩展名: xxx` | 显式 `FileName` 以 `.go` 结尾（扩展名由生成器追加） |
| `文件名 xxx.go 会触发 Go 文件名规则（原因）` | 显式 `FileName` 触发上述 Go 文件名构建规则 |
| `列映射 tag 名非法: xxx` | `ColumnTagName` 非空且含空格/控制字符、`:`、`"`、反引号或 DEL，不能作为 struct tag 的 key |
| `列[n] 为 nil` | `Columns` 的第 n 个元素（下标从 0 起）为 nil |
| `不支持的 json tag 命名风格: xxx` | `JsonTagValueCase` 非空但不是合法的 `NameCase` 枚举值 |
| `列 xxx 转换后的字段名为空` | 列名经 `ToPascalCase` 转换后为空（如纯分隔符列名） |
| `列 xxx 转换后的字段名 xxx 不是合法的 Go 标识符` | 转换后字段名数字开头等（如列名 `2fa_code`）；请在 `StructFieldInfo.Name` 显式指定合法名称 |
| `列 xxx 转换后的字段名 xxx 不是导出标识符（首字符须为大写字母），请通过 StructFieldInfo.Name 指定导出名` | 字段名首字符不是大写字母（如中文列名 `用户名`、显式指定的 `_`）：未导出字段会被 json 与 zcdb 映射静默跳过，`_` 也无法在互转方法中引用 |
| `列 xxx 转换后的字段名 xxx 与生成方法同名，请通过 StructFieldInfo.Name 改名` | 字段名为 `ToDO` 或 `ToEntity`（如列名 `to_entity`、`to_d_o`），与生成的互转方法冲突，无法编译 |
| `列 xxx 与 xxx 转换后的字段名重复: xxx` | 不同列转换后字段名相同（如 `user_id` 与 `userId`） |
| `输出目录为空` | `OutputDir` 为空字符串 |
| `创建输出目录失败: ...` | `OutputDir` 无法创建 |
| `输出文件路径逃逸输出目录: xxx` | 输出路径经清理后不在 `OutputDir` 内（防御性二次校验） |
| `生成结构体失败: import 别名冲突: ...` | 存量文件已把生成代码需要的本地包名绑定到另一路径（如 `time "other/time"`）；追加默认导入会形成重复绑定，故报错且不覆盖原文件 |
| `生成结构体失败: ...` | 目标文件存在但无法被 `go/parser` 解析（用户代码有语法错误）、生成产物未通过落盘前语法自校验等写文件失败场景；内部错误（`解析文件失败` / `创建临时文件失败` / `写入临时文件失败` / `设置文件权限失败` / `关闭临时文件失败` / `替换目标文件失败` / `生成代码存在语法错误`）均统一包裹为该错误 |

> 上表错误文本中的列名、字段名、结构体名前缀与列映射 tag 名以 `%q` 格式输出（带双引号，如 `列 "2fa_code" …`）；表名与文件名通常原样输出，但非法 UTF-8 名称以 `%q` 输出（转义非法字节，避免日志与终端出现裸非法字节）。

## Input

| 字段 | 类型 | 说明 |
|---|---|---|
| `OutputDir` | `string` | 输出目录，不得为空，不存在时自动创建。**目录名须为合法 Go 包名**（仅新建文件时包名取该目录**绝对路径**的末级目录名，`"."` 等相对路径同样解析为实际目录名，根目录等无法推导时回退 `main`；存量文件再生成时尊重原 package 声明） |
| `Database` | `string` | 数据库名，仅用于生成结构体的注释（呈现为 `{Database}.{TableName}`；为空时只呈现表名） |
| `Dialect` | `Dialect` | 数据库方言，决定列类型映射表 |
| `TableName` | `string` | 表名。`FileName` 为空时原样用作输出文件名（`{TableName}.go`），转 PascalCase 后推导结构体名 |
| `FileName` | `string` | 输出文件名（不含 `.go` 扩展名）；为空时取 `TableName`。表名会触发 Go 文件名构建规则（如 `order_linux`、`user_test`、`_tmp`）时用它改名；结构体名仍由表名推导 |
| `TableComment` | `string` | 表注释，生成到结构体注释中；为空时使用“表” |
| `ColumnTagName` | `string` | 列映射 tag 的名称（如 `"column"`、`"db"`），与 zcdb 的列映射标签对应；为空时不生成列映射 tag，非空时须为合法 struct tag key |
| `JsonTagValueCase` | `NameCase` | JSON tag 的命名风格；**空值表示不自动生成 json tag**（单列显式 `JsonTagValue` 仍会生成） |
| `Columns` | `[]*Column` | 表的所有字段；为空时生成仅含空结构体与互转方法的文件；元素不得为 nil |
| `Indexes` | `[]IndexInfo` | 表索引；为空时不生成结构体 doc 注释中的索引块 |

### Column / StructFieldInfo / IndexInfo

```go
type Column struct {
    Name            string          // 列名
    Type            string          // 列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"），ddl 片段的锚点
    Comment         string          // 列注释，生成 description tag
    Nullable        *bool           // 是否可空；nil 表示未知（ddl 片段省略 NULL 标记）
    Default         *string         // 默认值；nil 表示无默认值（方言原生格式，见「ddl 片段」；显式 DEFAULT NULL 的呈现见其「已知局限」）
    PrimaryKey      bool            // 是否主键列（联合主键多列均为 true），生成 primary_key tag
    Extra           string          // MySQL information_schema 的 EXTRA 列原样（PG/SQLite 恒空）
    StructFieldInfo StructFieldInfo // 生成结构体字段时的信息
}

type StructFieldInfo struct {
    Name         string // 结构体字段名；留空则由列名自动推导（ToPascalCase）
    Type         string // 结构体字段类型；留空则由列类型自动映射（未命中兜底 string）
    Import       string // Type 对应的 import 路径（如 time.Time 需 "time"）；留空时仅 time.Time 自动补 "time"，其余不引入包
    JsonTagValue string // json tag 的值；非空时直接使用（JsonTagValueCase 为空也会生成）；留空时按 JsonTagValueCase 自动推导
}

type IndexInfo struct {
    Name    string   // 索引名（仅唯一/普通索引渲染该名字；主键行渲染为 PRIMARY KEY (cols)）
    Columns []string // 索引列，按定义顺序
    Unique  bool     // 是否唯一索引
    Primary bool     // 是否主键索引
}
```

**显式优先**：`StructFieldInfo` 的每个字段都允许调用方预先指定，`Generate` 只对留空的字段做自动推导。特殊列（如 JSON 列想映射为自定义结构体类型）可以完全手工控制。

**指针字段的语义**：`Nullable` 与 `Default` 用指针而非值类型，以区分“未知 / 无默认值”与零值——`Nullable` 为 `nil` 时 `ddl` 片段**省略** NULL 标记（而不是假定 `NOT NULL` 而误标可空列）；`Default` 为 `nil` 表示无默认值，非 `nil` 一律追加 DEFAULT 段（含空串，判定只看指针不看值）。手工构造时建议显式填写这两个字段（如 `Nullable: &nullable`、`Default: &defaultVal`）。

## NameCase

JSON tag 值的命名风格枚举，`IsValid()` 判断合法性：

| 枚举值 | 字符串值 | 示例（列名 user_id） |
|---|---|---|
| `NameCaseLowerCamel` | `lowerCamel` | `userId` |
| `NameCaseUpperCamel` | `upperCamel` | `UserId` |
| `NameCaseLowerSnake` | `lowerSnake` | `user_id` |
| `NameCaseUpperSnake` | `upperSnake` | `USER_ID` |
| `NameCaseLowerKebab` | `lowerKebab` | `user-id` |
| `NameCaseUpperKebab` | `upperKebab` | `USER-ID` |

`JsonTagValueCase` 为空时 `Generate` 跳过风格校验，且不自动生成 json tag。

## Dialect

| 枚举值 | 字符串值 |
|---|---|
| `DialectMysql` | `mysql` |
| `DialectPostgres` | `postgres` |
| `DialectSqlite` | `sqlite` |

## 其他导出 API

`toCase.go` 中导出的命名转换函数可直接复用：`SplitWords`、`ToPascalCase`、`ToLowerCamel`、`ToUpperCamel`、`ToLowerSnake`、`ToUpperSnake`、`ToLowerKebab`、`ToUpperKebab`。其中除 `SplitWords`、`ToPascalCase` 接收字符串外，其余六个接收 `SplitWords` 的单词列表并拼接为对应风格；同文件的 `formatJSONTag` 只服务于生成流程（按列名与 `NameCase` 推导 json tag），不对外导出。规则见 [类型映射与命名转换](types-and-naming.md)。

## 相关

- 生成流程与失败边界：[生成流水线](pipeline.md)
- 再生成契约：[增量再生成](regeneration.md)
- 字段 tag / ddl / 索引块：[字段、tag 与 ddl](tags-ddl-index.md)
