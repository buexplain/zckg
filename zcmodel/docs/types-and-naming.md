# 类型映射与命名转换

> 返回 [zcmodel 文档地图](zcmodel.md)。本文覆盖两件“从数据库名字到 Go 代码”的转换：列类型 → Go 类型，任意命名风格 → Go 命名。

## 列类型映射

`Generate` 对未显式指定 `Type` 的列执行：**归一化（`normalizeColumnType`）→ 查方言映射表（`makeColumnTypeToGoTypeMap`）→ 未命中兜底 `string`**。`time.Time` 在调用方未显式指定 `Import` 时自动引入 `time` 包。

### 归一化规则

列类型先统一小写、去除括号及其内容（长度、精度、枚举值列表）、压缩连续空格，再按方言做特有处理：

| 方言 | 特有处理 | 示例 |
|---|---|---|
| MySQL | 去除 `unsigned`、`zerofill` 修饰符 | `BIGINT(20) UNSIGNED` → `bigint` |
| PostgreSQL | 时区后缀规范化 | `timestamp with time zone` → `timestamptz`、`timestamp without time zone` → `timestamp` |
| SQLite | 去除类型名上的引号 | `"VARCHAR"(255)` → `varchar` |

> 括号内容会被整体移除，但括号后的内容保留，因此 `timestamp(6) with time zone` 能正确归一化为 `timestamptz`。映射表键为**不带长度/精度后缀**的类型名；缺失或未知方言返回 `nil` 映射表（由 `Generate` 前置拒绝方言，正常不会走到）。

### MySQL 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| tinyint / smallint / mediumint / int / integer / year | `int` | char / varchar / tinytext / text / mediumtext / longtext | `string` |
| bigint | `int64` | enum / set / json | `string` |
| float / double / decimal / numeric | `float64` | date / time / datetime / timestamp | `time.Time` |
| bool / boolean | `bool` | binary / varbinary / tinyblob / blob / mediumblob / longblob | `[]byte` |

### PostgreSQL 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| smallint / int2 / integer / int / int4 / smallserial / serial | `int` | money | `string` |
| bigint / int8 / bigserial | `int64` | character varying / varchar / character / char / bpchar / text | `string` |
| real / float4 / double precision / float8 / numeric / decimal | `float64` | uuid / json / jsonb / inet / interval | `string` |
| boolean / bool | `bool` | date / time / timetz / timestamp / timestamptz | `time.Time` |
| bytea | `[]byte` | | |

### SQLite 映射表

| 列类型 | Go 类型 | 列类型 | Go 类型 |
|---|---|---|---|
| tinyint / smallint / mediumint / int / integer / int2 | `int` | character / varchar / varying character / nchar / native character / nvarchar / text / clob | `string` |
| bigint / int8 / unsigned big int | `int64` | json | `string` |
| real / double / double precision / float / numeric / decimal | `float64` | date / datetime / timestamp | `time.Time` |
| boolean / bool | `bool` | blob | `[]byte` |

三张表的键数量分别为 MySQL 32、PostgreSQL 36、SQLite 30（与源码映射表逐键一致，测试会校验键集）。

> 实现：`columnTypeToGoType.go` `normalizeColumnType`、`makeColumnTypeToGoTypeMap`、`formatStructFieldType`；测试：`TestNormalizeColumnType`、`TestNormalizeColumnTypeMatchesMap`、`TestFormatStructFieldType`、三方言集成测试。

### 已知边界

- 未知类型（如 PG 自定义类型）兜底 `string`，避免生成非法代码；需要精确类型时显式指定 `StructFieldInfo.Type` 与 `Import`。
- MySQL 的 BOOL/BOOLEAN 实际存储为 `tinyint(1)`，从真实 MySQL 读到的列类型是 `tinyint`，映射结果为 `int` 而非 `bool`。
- SQLite 没有列注释，通过 zcdb Schema 读取时 `Comment` 恒为空，生成的 `description` tag 也为空；手工构造并填写 `Comment` 时仍会生成该 tag。

## 命名转换

`toCase.go` 中导出的命名转换函数可直接复用：`SplitWords`、`ToPascalCase`、`ToLowerCamel`、`ToUpperCamel`、`ToLowerSnake`、`ToUpperSnake`、`ToLowerKebab`、`ToUpperKebab`。其中除 `SplitWords`、`ToPascalCase` 接收字符串外，其余六个接收 `SplitWords` 的单词列表并拼接为对应风格；`formatJSONTag` 只服务于生成流程（按列名与 `NameCase` 推导 json tag），**不对外导出**。

### SplitWords：任意风格拆词

拆词是全部命名转换的基础，规则：

- 下划线 `_`、连字符 `-`、空格作为分隔符；
- 小写 → 大写转换处拆分（`userName` → `user | Name`）；
- 连续大写后接小写时，在最后一个大写前拆分（`HTTPServer` → `HTTP | Server`）。

### ToPascalCase：结构体名 / 字段名

任意风格输入统一转为 PascalCase（每个单词仅首字母大写，其余转小写），唯一特例是单词 `id`（不区分大小写）统一转为 `ID`：

| 输入 | 输出 |
|---|---|
| `user_id` | `UserID` |
| `getUserById` | `GetUserByID` |
| `order-no` | `OrderNo` |
| `HTTPServer` | `HttpServer` |

结构体名与字段名都由它推导：表名 → `UserOrderEntity` / `UserOrderDO`，列名 → 字段名。字段名还必须是**导出标识符**且不与 `ToDO`/`ToEntity` 同名，否则 `Generate` 前置报错（见 [错误表](api.md)）。

### formatJSONTag：json tag 值（内部函数）

按 `NameCase` 枚举输出对应风格（见 [NameCase](api.md#namecase) 表）；未识别的风格返回空串。`Generate` 在 `JsonTagValueCase` 非空时调用它填充未显式指定的 `JsonTagValue`，因此该函数不导出。

> 实现：`toCase.go` `SplitWords`、`ToPascalCase`、`ToLowerCamel`、`ToUpperCamel`、`ToLowerSnake`、`ToUpperSnake`、`ToLowerKebab`、`ToUpperKebab`、`formatJSONTag`；测试：`TestSplitWords`、`TestToPascalCase`、`TestToCase_各命名风格`、`TestFormatJSONTag`、`TestToCase_EmptyWordSkipped`。

## 相关

- 字段名/类型/tag 如何写入产物：[字段、tag 与 ddl](tags-ddl-index.md)
- 完整 API 与错误表：[对外 API](api.md)
