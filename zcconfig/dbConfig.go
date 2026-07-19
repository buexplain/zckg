package zcconfig

// DBConfig 数据库连接配置结构体，用于承载数据库连接所需的各项参数。
// 通常与 Register / Config 配合使用：通过 Register 注册配置数据后，
// 使用 Config[T] 按路径读取。
//
// 使用示例：
//
//	zcconfig.Register("database", func() map[string]any {
//		config := map[string]any{}
//		config["test_db"] = zcconfig.DBConfig{
//			Host:     zcconfig.Env("DB_HOST", "127.0.0.1"),
//			Port:     zcconfig.Env("DB_PORT", 3306),
//			Username: zcconfig.Env("DB_USERNAME", "root"),
//			Password: zcconfig.Env("DB_PASSWORD", "root"),
//			DBName:   zcconfig.Env("DB_DBNAME", "test_db"),
//			Charset:  zcconfig.Env("DB_CHARSET", "utf8mb4"),
//		}
//		return config
//	})
//
// testDB := zcconfig.Config("database.test_db", zcconfig.DBConfig{})
type DBConfig struct {
	Host     string // 数据库服务器地址，如 "127.0.0.1" 或 "db.example.com"
	Port     int    // 数据库服务器端口，如 MySQL 默认 3306、PostgreSQL 默认 5432
	Username string // 数据库登录用户名
	Password string // 数据库登录密码
	DBName   string // 目标数据库名称
	Charset  string // 连接字符集，如 "utf8mb4"（MySQL）或 "UTF8"（PostgreSQL）
}
