package zcconfig

import "fmt"

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
//			Driver:   zcconfig.Env("DB_DRIVER", "mysql"),
//			Dialect:  zcconfig.Env("DB_DIALECT", "mysql"),
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
//	pool, err := zcdb.NewPool(zcdb.PoolConfig{
//		DriverName: testDB.Driver,
//		DSN:        masterDSN,
//		SlaveDSNs:  slaveDSNs,
//	})
type DBConfig struct {
	Driver   string          // 驱动名（"mysql"、"postgres"、"sqlite"）
	Dialect  string          // 数据库方言（"mysql"、"postgres"、"sqlite"）
	Host     string          // 数据库服务器地址，如 "127.0.0.1" 或 "db.example.com"
	Port     int             // 数据库服务器端口，如 MySQL 默认 3306、PostgreSQL 默认 5432
	Username string          // 数据库登录用户名
	Password string          // 数据库登录密码
	Database string          // 目标数据库名称
	Charset  string          // 连接字符集，如 "utf8mb4"（MySQL）或 "UTF8"（PostgreSQL）
	Loc      string          // 时区，默认是：Local
	Slaves   []DBSlaveConfig // 从库配置列表
}

// GetMasterDSN 返回主库的连接字符串（DSN），可直接传给 zcdb.PoolConfig.DSN。
//
// 按 Driver（为空时回退 Dialect）选择 DSN 格式：
//
//   - mysql：user:pass@tcp(host:port)/database?charset=utf8mb4&parseTime=true&loc=Local
//     （Charset 为空时默认 "utf8mb4"，Loc 为空时默认 "Local"，parseTime 恒为 true）
//   - postgres：host=... port=... user=... password=... dbname=... sslmode=disable
//     （键值对格式，密码无需转义；sslmode 恒为 disable）
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
		// lib/pq 键值对格式：避免 URL 格式中密码特殊字符的转义问题
		dsn := fmt.Sprintf("host=%s port=%d user=%s", host, port, username)
		if password != "" {
			dsn += " password=" + password
		}
		if d.Database != "" {
			dsn += " dbname=" + d.Database
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
			username, password, host, port, d.Database, charset, loc)
	default:
		return ""
	}
}

// DBSlaveConfig 从库配置结构体，用于承载从库连接所需的各项参数。
// 从库复用主库的 Database / Charset / Loc，因此不重复定义这几个字段。
type DBSlaveConfig struct {
	Host     string // 数据库服务器地址，如 "127.0.0.1" 或 "db.example.com"
	Port     int    // 数据库服务器端口，如 MySQL 默认 3306、PostgreSQL 默认 5432
	Username string // 数据库登录用户名
	Password string // 数据库登录密码
}
