# DBConfig：数据库连接配置与 DSN 组装

`DBConfig` 是一个纯数据 + 纯函数结构：只负责按驱动名把字段拼成 DSN，**不连接数据库、不注册驱动、不做网络探测**。建池仍由调用方（如 `zcdb.NewPool`）完成。

## 字段

| 字段 | 类型 | 语义 |
|---|---|---|
| `Driver` | string | 实际的 `database/sql` 驱动注册名（如 `mysql`/`postgres`/`sqlite`）；仅用于选择 DSN 格式，**大小写敏感** |
| `Dialect` | string | 仅在 `Driver` 为空时回退用于选择格式；不回填 `Driver` |
| `Host` | string | 主机名或 IP |
| `Port` | int | 端口（MySQL 默认 3306、PostgreSQL 默认 5432；本结构不做默认值填充） |
| `Username` | string | 登录用户名；MySQL 不能含 `:`（见下） |
| `Password` | string | 登录密码；MySQL 按原文输出，不做 URL 转义 |
| `Database` | string | 原始数据库名（MySQL 由组装时做路径转义，**不要预编码**）；SQLite 为原始 DSN |
| `Charset` | string | 仅 MySQL：字符集名或逗号分隔列表，原样输出；为空默认 `utf8mb4` |
| `Loc` | string | 仅 MySQL：原始时区名（如 `Asia/Shanghai`）；为空默认 `Local` |
| `Slaves` | []DBSlaveConfig | 从库列表；从库复用主库的 `Database`/`Charset`/`Loc` |

`DBSlaveConfig` 只有 `Host` / `Port` / `Username` / `Password` 四个字段——从库不重复定义库名、字符集、时区。

## 格式选择

```text
driver := DBConfig.Driver
if driver == "" { driver = DBConfig.Dialect }

sqlite / sqlite3        → Database 原样返回
postgres / postgresql / pgsql → 键值对格式
mysql                   → user:pass@tcp(host:port)/db?charset=...&parseTime=true&loc=...
其他（含空串）            → ""（由调用方校验报错）
```

- 驱动名**大小写敏感**：`MySQL`、`SQLite` 不匹配任何格式，返回空字符串。
- 别名只决定格式，不注册驱动：`pgsql` 不能直接作为 `PoolConfig.DriverName`；本仓库使用的驱动实际注册名是 `mysql`、`postgres`、`sqlite`。

## MySQL

格式：

```text
<username>:<password>@tcp(<host>:<port>)/<PathEscape(Database)>?charset=<Charset>&parseTime=true&loc=<QueryEscape(Loc)>
```

| 项 | 规则 |
|---|---|
| `Charset` | 为空 → `utf8mb4`；原样拼接，不能含 `&`/`?`/`=`/`/`；逗号用于驱动支持的候选字符集列表 |
| `Loc` | 为空 → `Local`；用 `url.QueryEscape` 转义一次，传原始时区名（如 `Asia/Shanghai`），不要预编码 |
| `Database` | 用 `url.PathEscape` 转义一次；字面库名 `a%2Fb` 会被转义成 `a%252Fb`，驱动解码后仍是 `a%2Fb` |
| `parseTime` | 恒为 `true`，保证 `time.Time` 字段可直接扫描 |
| `Username` | **不能含 `:`**：驱动把凭据中的首个冒号当用户名/密码分隔符，百分号转义不能解决此限制 |
| `Password` | 可含 `@`、`:`、`/`，按原文输出；不做 URL 转义（驱动也不解码凭据） |

## PostgreSQL

格式（lib/pq 键值对）：

```text
host=<v> port=<n> user=<v> [password=<v>] [dbname=<v>] sslmode=disable
```

| 项 | 规则 |
|---|---|
| 值转义 | 空值，或含空白/单引号/反斜杠 → 单引号包裹，并按 lib/pq 规则转义：`\`→`\\`、`'`→`\'`；简单值原样输出 |
| `host` / `port` / `user` | 恒输出；空 host/user 写为 `host=''`、`user=''`（避免吞掉后继参数） |
| `password` / `dbname` | 为空时**省略**对应键（驱动可能用环境变量/默认值补全） |
| `sslmode` | 恒为 `disable` |
| `Charset` / `Loc` | 忽略（PostgreSQL 由驱动/服务端决定编码） |

## SQLite

`GetMasterDSN()` 直接返回 `Database` 原文：`":memory:"` 或文件路径（如 `file:/path/to.db`）。从库同样复用该值。

## 从库

`GetSlaveDSN()` 与 `Slaves` 一一对应：每个 DSN 复用主库的 `Database`/`Charset`/`Loc`，仅 `Host`/`Port`/`Username`/`Password` 取自各 `DBSlaveConfig`。`Slaves` 为空时返回 `nil`。

```go
func example() {
    cfg := zcconfig.DBConfig{
        Driver:   "mysql",
        Host:     "127.0.0.1",
        Port:     3306,
        Username: "root",
        Password: "root",
        Database: "test_db",
        Slaves: []zcconfig.DBSlaveConfig{
            {Host: "127.0.0.1", Port: 3307, Username: "root", Password: "root"},
        },
    }
    master := cfg.GetMasterDSN() // root:root@tcp(127.0.0.1:3306)/test_db?charset=utf8mb4&parseTime=true&loc=Local
    slaves := cfg.GetSlaveDSN()  // []string{"root:root@tcp(127.0.0.1:3307)/test_db?charset=utf8mb4&parseTime=true&loc=Local"}
}
```

对接连接池时：`DriverName` 必须是真实注册名，`DSN` 用 `GetMasterDSN()`，`SlaveDSNs` 用 `GetSlaveDSN()`；驱动需由调用方导入。

## 实现路径

| 行为 | 实现 |
|---|---|
| 主库 DSN | `dbConfig.go:GetMasterDSN` |
| 从库 DSN 列表 | `dbConfig.go:GetSlaveDSN` |
| 格式选择与三种驱动组装 | `dbConfig.go:buildDSN` |
| PostgreSQL 值转义（空值/空白/引号/反斜杠） | `dbConfig.go:escapePostgresValue` |

## 代码与测试锚点

| 能力 | 测试 |
|---|---|
| 三种驱动主库 DSN、默认值、空值省略 | `TestDBConfig_GetMasterDSN` |
| PostgreSQL 转义与回读 | `TestDBConfig_PostgresDSNEscape`、`TestDBConfig_PostgresDSNRoundTrip` |
| MySQL 库名/时区转义与驱动回读 | `TestDBConfig_MySQLDSNRoundTrip`、`TestDBConfig_MySQLPreencodedLoc` |
| MySQL 凭据字符限制（`@`/`:`/`/`/`%`） | `TestDBConfig_MySQLCredentials` |
| 别名、Driver 回退、大小写敏感、空/未知驱动 | `TestDBConfig_DriverFormatOnly` |
| 从库复用与空列表 | `TestDBConfig_GetSlaveDSN`、`TestDBConfig_GetSlaveDSN_Empty` |
| Charset 对 MySQL 生效、对 PostgreSQL 忽略 | `TestDBConfig_Charset` |

相关章节：[errors.md](errors.md)（`Config[DBConfig]` 类型不符 panic）。
