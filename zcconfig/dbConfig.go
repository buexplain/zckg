package zcconfig

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// DBConfig 数据库连接配置结构体，用于承载数据库连接所需的各项参数。
// 通常与 Register / Config 配合使用：通过 Register 注册配置数据后，
// 使用 Config[T] 按路径读取，再通过 GetMasterDSN / GetSlaveDSN
// 生成主库与从库的连接字符串（DSN）。
//
// 使用示例：
//
//	zcconfig.Register("database", func() map[string]any {
//		config := map[string]any{}
//		config["test_db"] = zcconfig.DBConfig{
//			Driver:   "mysql",
//			Dialect:  "mysql",
//			Host:     zcconfig.Env("DB_HOST", "127.0.0.1"),
//			Port:     zcconfig.Env("DB_PORT", 3306),
//			Username: zcconfig.Env("DB_USERNAME", "root"),
//			Password: zcconfig.Env("DB_PASSWORD", "root"),
//			Database: zcconfig.Env("DB_DATABASE", "test_db"),
//			Charset:  zcconfig.Env("DB_CHARSET", "utf8mb4"),
//			Loc:      zcconfig.Env("DB_LOC", "Local"),
//			Slaves: []zcconfig.DBSlaveConfig{
//				{Host: "127.0.0.1", Port: 3307, Username: "root", Password: "root"},
//			},
//		}
//		return config
//	})
//
//	testDB := zcconfig.Config("database.test_db", zcconfig.DBConfig{})
//
//	// 主库 DSN：root:root@tcp(127.0.0.1:3306)/test_db?charset=utf8mb4&parseTime=true&loc=Local
//	masterDSN := testDB.GetMasterDSN()
//	// 从库 DSN 列表：从库复用主库的 Database / Charset / Loc
//	slaveDSNs := testDB.GetSlaveDSN()
//
//	// 建池前由调用方导入驱动；本例需 import _ "github.com/go-sql-driver/mysql" 注册 "mysql"。
//	// Driver 必须是实际的 database/sql 注册名；生成 DSN 不注册驱动，也不回填 Driver。
//	pool, err := zcdb.NewPool(zcdb.PoolConfig{
//		DriverName: testDB.Driver,
//		DSN:        masterDSN,
//		SlaveDSNs:  slaveDSNs,
//	})
type DBConfig struct {
	Driver   string          // 调用方实际使用的 database/sql 驱动注册名；DSN 格式别名不代表已注册该驱动
	Dialect  string          // 数据库方言；仅在 Driver 为空时用于选择 DSN 格式，不回填 Driver
	Host     string          // 数据库服务器地址，如 "127.0.0.1" 或 "db.example.com"
	Port     int             // 数据库服务器端口，如 MySQL 默认 3306、PostgreSQL 默认 5432
	Username string          // 数据库登录用户名；MySQL DSN 不支持含冒号的用户名，百分号转义不能解决此限制
	Password string          // 数据库登录密码；MySQL 密码可含 @、:、/，按原文输出，不做 URL 转义
	Database string          // 原始数据库名称（MySQL 生成时做路径转义，不要预编码）；SQLite 为原始 DSN
	Charset  string          // 仅 MySQL：合法字符集名或逗号分隔列表，如 "utf8mb4,utf8"，原样输出；PostgreSQL 忽略
	Loc      string          // 仅 MySQL：原始时区名，如 "Asia/Shanghai"，不要预编码；为空时默认 "Local"
	Slaves   []DBSlaveConfig // 从库配置列表
}

// GetMasterDSN 返回主库的连接字符串（DSN），可传给 zcdb.PoolConfig.DSN；
// 建池时仍需调用方导入驱动并提供实际的 database/sql 注册名。
//
// 按 Driver（为空时回退 Dialect）选择 DSN 格式；postgresql/pgsql 视为 postgres，
// sqlite3 视为 sqlite。这些规则仅选择格式，不注册驱动，也不修改 Driver。
//
//   - mysql：user:pass@tcp(host:port)/database?charset=utf8mb4&parseTime=true&loc=Local
//     （Database 使用 url.PathEscape，Loc 使用 url.QueryEscape，输入均为原文，不识别预编码；
//     Charset 为合法字符集名或逗号分隔列表，原样输出，空时默认 "utf8mb4"；
//     Loc 空时默认 "Local"，parseTime 恒为 true；用户名不能含冒号，密码可含 @、:、/，
//     凭据不做 URL 转义，百分号转义不能用于表达用户名中的冒号）
//   - postgres：host=... port=... user=... password=... dbname=... sslmode=disable
//     （键值对格式；空值或含空白/单引号/反斜杠的值以单引号包裹并按 lib/pq 规则转义；
//     host/port/user 恒输出，空 host/user 使用两个单引号表示；
//     空 password/dbname 省略，仍可受驱动环境或默认值影响；sslmode 恒为 disable，Charset/Loc 不使用）
//   - sqlite：Database 原样返回（":memory:" 或文件路径，如 "file:/path/to.db"）
//
// 未知驱动返回空字符串，由调用方（如 zcdb.NewPool）校验报错。
func (d DBConfig) GetMasterDSN() string {
	return d.buildDSN(d.Host, d.Port, d.Username, d.Password)
}

// GetSlaveDSN 返回从库的连接字符串列表，与 Slaves 一一对应，可直接传给
// zcdb.PoolConfig.SlaveDSNs。从库复用主库的 Database / Charset / Loc 等参数，
// 仅 Host / Port / Username / Password 取自各自的 DBSlaveConfig。
// Slaves 为空时返回 nil。
func (d DBConfig) GetSlaveDSN() []string {
	if len(d.Slaves) == 0 {
		return nil
	}
	dsns := make([]string, 0, len(d.Slaves))
	for _, s := range d.Slaves {
		dsns = append(dsns, d.buildDSN(s.Host, s.Port, s.Username, s.Password))
	}
	return dsns
}

// buildDSN 按驱动名组装单个连接字符串，从库复用主库的 Database / Charset / Loc。
func (d DBConfig) buildDSN(host string, port int, username, password string) string {
	driver := d.Driver
	if driver == "" {
		driver = d.Dialect
	}
	switch driver {
	case "sqlite", "sqlite3":
		// sqlite 的连接串就是数据库文件路径（或 ":memory:"）
		return d.Database
	case "postgres", "postgresql", "pgsql":
		// lib/pq 键值对格式：空值或含空白/单引号/反斜杠时单引号包裹并转义，避免吞掉后续参数
		dsn := fmt.Sprintf("host=%s port=%d user=%s",
			escapePostgresValue(host), port, escapePostgresValue(username))
		if password != "" {
			dsn += " password=" + escapePostgresValue(password)
		}
		if d.Database != "" {
			dsn += " dbname=" + escapePostgresValue(d.Database)
		}
		return dsn + " sslmode=disable"
	case "mysql":
		charset := d.Charset
		if charset == "" {
			charset = "utf8mb4"
		}
		loc := d.Loc
		if loc == "" {
			loc = "Local"
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=true&loc=%s",
			username, password, host, port, url.PathEscape(d.Database), charset, url.QueryEscape(loc))
	default:
		return ""
	}
}

// escapePostgresValue 转义 lib/pq 键值对 DSN 中的单个值。
// 空值输出两个单引号，防止后续 key=value 被吞作当前值；含空白、单引号或反斜杠时
// 以单引号包裹并按 lib/pq 规则转义（\ → \\、' → \'），非空简单值原样返回。
func escapePostgresValue(v string) string {
	if v != "" && !strings.ContainsAny(v, "'\\") && strings.IndexFunc(v, unicode.IsSpace) < 0 {
		return v
	}
	// 顺序敏感：先转义反斜杠，再转义单引号，否则会把刚引入的反斜杠二次转义
	escaped := strings.ReplaceAll(v, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `\'`)
	return "'" + escaped + "'"
}

// DBSlaveConfig 从库配置结构体，用于承载从库连接所需的各项参数。
// 从库复用主库的 Database / Charset / Loc，因此不重复定义这几个字段。
type DBSlaveConfig struct {
	Host     string // 数据库服务器地址，如 "127.0.0.1" 或 "db.example.com"
	Port     int    // 数据库服务器端口，如 MySQL 默认 3306、PostgreSQL 默认 5432
	Username string // 数据库登录用户名；MySQL DSN 不支持含冒号的用户名，百分号转义不能解决此限制
	Password string // 数据库登录密码；MySQL 密码可含 @、:、/，按原文输出，不做 URL 转义
}
