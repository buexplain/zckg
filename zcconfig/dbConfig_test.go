package zcconfig

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	_ "time/tzdata"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
)

// TestDBConfig_GetMasterDSN 验证各驱动下的主库 DSN 组装格式。
func TestDBConfig_GetMasterDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  DBConfig
		want string
	}{
		{
			name: "mysql 完整配置",
			cfg: DBConfig{
				Driver:   "mysql",
				Dialect:  "mysql",
				Host:     "127.0.0.1",
				Port:     3306,
				Username: "root",
				Password: "root",
				Database: "test_db",
				Charset:  "utf8mb4",
				Loc:      "Local",
			},
			want: "root:root@tcp(127.0.0.1:3306)/test_db?charset=utf8mb4&parseTime=true&loc=Local",
		},
		{
			name: "mysql Charset 与 Loc 为空时取默认值",
			cfg: DBConfig{
				Driver:   "mysql",
				Host:     "db.example.com",
				Port:     3307,
				Username: "app",
				Password: "secret",
				Database: "app_db",
			},
			want: "app:secret@tcp(db.example.com:3307)/app_db?charset=utf8mb4&parseTime=true&loc=Local",
		},
		{
			name: "mysql 自定义 Charset 与 Loc",
			cfg: DBConfig{
				Driver:   "mysql",
				Host:     "127.0.0.1",
				Port:     3306,
				Username: "root",
				Database: "test_db",
				Charset:  "gbk",
				Loc:      "Asia/Shanghai",
			},
			want: "root:@tcp(127.0.0.1:3306)/test_db?charset=gbk&parseTime=true&loc=Asia%2FShanghai",
		},
		{
			name: "postgres 键值对格式",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Password: "root",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres password=root dbname=test_db sslmode=disable",
		},
		{
			name: "postgres 空密码与空库名时省略对应键",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
			},
			want: "host=127.0.0.1 port=5432 user=postgres sslmode=disable",
		},
		{
			name: "postgres 空主机与用户名输出引号且省略空密码库名",
			cfg:  DBConfig{Driver: "postgres", Port: 5433},
			want: "host='' port=5433 user='' sslmode=disable",
		},
		{
			name: "sqlite 内存库",
			cfg: DBConfig{
				Driver:   "sqlite",
				Database: ":memory:",
			},
			want: ":memory:",
		},
		{
			name: "sqlite 文件路径",
			cfg: DBConfig{
				Driver:   "sqlite",
				Database: "file:/tmp/app.db",
			},
			want: "file:/tmp/app.db",
		},
		{
			name: "Driver 为空时回退 Dialect",
			cfg: DBConfig{
				Dialect:  "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres dbname=test_db sslmode=disable",
		},
		{
			name: "Dialect 别名 postgresql 按 postgres 组装",
			cfg: DBConfig{
				Driver:   "",
				Dialect:  "postgresql",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres dbname=test_db sslmode=disable",
		},
		{
			name: "pgsql 别名按 postgres 组装",
			cfg: DBConfig{
				Driver:   "pgsql",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres dbname=test_db sslmode=disable",
		},
		{
			name: "sqlite3 别名按 sqlite 组装",
			cfg: DBConfig{
				Driver:   "sqlite3",
				Database: ":memory:",
			},
			want: ":memory:",
		},
		{
			name: "未知驱动返回空字符串",
			cfg: DBConfig{
				Driver:   "oracle",
				Host:     "127.0.0.1",
				Port:     1521,
				Username: "root",
				Database: "test_db",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetMasterDSN(); got != tt.want {
				t.Errorf("期望 %q，实际 %q", tt.want, got)
			}
		})
	}
}

// TestDBConfig_PostgresDSNEscape 锁死 postgres 键值对 DSN 中特殊字符的转义行为：
// 值含空白/单引号/反斜杠时以单引号包裹并按 lib/pq 规则转义，简单值原样输出。
func TestDBConfig_PostgresDSNEscape(t *testing.T) {
	tests := []struct {
		name string
		cfg  DBConfig
		want string
	}{
		{
			name: "密码含空格",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Password: "pa ss",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres password='pa ss' dbname=test_db sslmode=disable",
		},
		{
			name: "密码含单引号与反斜杠",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Password: `p'a\ss`,
				Database: "test_db",
			},
			want: `host=127.0.0.1 port=5432 user=postgres password='p\'a\\ss' dbname=test_db sslmode=disable`,
		},
		{
			name: "用户名与库名含空格",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "app user",
				Password: "root",
				Database: "my db",
			},
			want: "host=127.0.0.1 port=5432 user='app user' password=root dbname='my db' sslmode=disable",
		},
		{
			name: "简单值不受影响（与既有输出一致）",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Password: "root",
				Database: "test_db",
			},
			want: "host=127.0.0.1 port=5432 user=postgres password=root dbname=test_db sslmode=disable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetMasterDSN(); got != tt.want {
				t.Errorf("期望 %q，实际 %q", tt.want, got)
			}
		})
	}
}

// TestDBConfig_GetSlaveDSN 验证从库 DSN 列表组装：复用主库的 Database / Charset / Loc，
// 仅 Host / Port / Username / Password 取自各自的 DBSlaveConfig，并覆盖三种驱动的从库场景。
func TestDBConfig_GetSlaveDSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  DBConfig
		want []string
	}{
		{
			name: "mysql 多从库复用主库 Database/Charset/Loc",
			cfg: DBConfig{
				Driver:   "mysql",
				Host:     "127.0.0.1",
				Port:     3306,
				Username: "root",
				Password: "root",
				Database: "test_db",
				Charset:  "utf8mb4",
				Loc:      "Local",
				Slaves: []DBSlaveConfig{
					{Host: "127.0.0.1", Port: 3307, Username: "root", Password: "root"},
					{Host: "10.0.0.2", Port: 3306, Username: "reader", Password: "rpass"},
				},
			},
			want: []string{
				"root:root@tcp(127.0.0.1:3307)/test_db?charset=utf8mb4&parseTime=true&loc=Local",
				"reader:rpass@tcp(10.0.0.2:3306)/test_db?charset=utf8mb4&parseTime=true&loc=Local",
			},
		},
		{
			name: "postgres 从库键值对格式与特殊字符转义",
			cfg: DBConfig{
				Driver:   "postgres",
				Host:     "127.0.0.1",
				Port:     5432,
				Username: "postgres",
				Password: "root",
				Database: "test_db",
				Slaves: []DBSlaveConfig{
					{Host: "127.0.0.1", Port: 5433, Username: "rep user", Password: "p ass"},
				},
			},
			want: []string{
				"host=127.0.0.1 port=5433 user='rep user' password='p ass' dbname=test_db sslmode=disable",
			},
		},
		{
			name: "sqlite 从库复用主库 Database",
			cfg: DBConfig{
				Driver:   "sqlite",
				Database: "file:/tmp/app.db",
				Slaves: []DBSlaveConfig{
					{Host: "127.0.0.1", Port: 3306, Username: "root", Password: "root"},
				},
			},
			want: []string{
				"file:/tmp/app.db",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetSlaveDSN()
			if len(got) != len(tt.want) {
				t.Fatalf("期望 %d 个从库 DSN，实际 %d：%v", len(tt.want), len(got), got)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("第 %d 个从库 DSN 期望 %q，实际 %q", i, tt.want[i], got[i])
				}
			}
		})
	}
}

// TestDBConfig_GetSlaveDSN_Empty 验证无从库时返回 nil。
func TestDBConfig_GetSlaveDSN_Empty(t *testing.T) {
	cfg := DBConfig{Driver: "mysql", Database: "test_db"}
	if got := cfg.GetSlaveDSN(); got != nil {
		t.Errorf("Slaves 为空时期望 nil，实际 %v", got)
	}
}

// isolateDBConfigPostgresEnv 清除 pq 读取的环境变量，避免本机配置、service 文件或不支持的 PG* 变量干扰解析。
func isolateDBConfigPostgresEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "PG") || strings.HasPrefix(upper, "SSLPG") {
			// Setenv 注册恢复并禁止并行；必须真正删除，pq 对部分空环境变量也会报错。
			t.Setenv(key, value)
			if err := os.Unsetenv(key); err != nil {
				t.Fatalf("清除环境变量 %s: %v", key, err)
			}
		}
	}
}

// checkDBConfigDSNs 对主从分别回读，校验数量及地址凭据的对应关系，并确认生成 DSN 不修改配置。
func checkDBConfigDSNs(t *testing.T, cfg DBConfig, check func(*testing.T, string, DBSlaveConfig)) {
	t.Helper()
	before := cfg
	before.Slaves = append([]DBSlaveConfig(nil), cfg.Slaves...)
	t.Run("master", func(t *testing.T) {
		check(t, cfg.GetMasterDSN(), DBSlaveConfig{
			Host: before.Host, Port: before.Port, Username: before.Username, Password: before.Password,
		})
	})
	dsns := cfg.GetSlaveDSN()
	if len(dsns) != len(before.Slaves) {
		t.Fatalf("从库数量期望 %d，实际 %d", len(before.Slaves), len(dsns))
	}
	for i, slave := range before.Slaves {
		t.Run(fmt.Sprintf("slave_%d", i), func(t *testing.T) {
			check(t, dsns[i], slave)
		})
	}
	if !reflect.DeepEqual(cfg, before) {
		t.Errorf("DSN 生成不应修改配置：期望 %+v，实际 %+v", before, cfg)
	}
}

// TestDBConfig_PostgresDSNRoundTrip 验证主从空字段的全部组合及非空转义经 pq 回读不吞 port/password/dbname/sslmode，不连接数据库。
func TestDBConfig_PostgresDSNRoundTrip(t *testing.T) {
	tests := []struct {
		name, host, user, password, database string
	}{
		{"space", "h ost", "u ser", "pa ss", "my db"},
		{"tab", "h\tost", "u\tser", "pa\tss", "my\tdb"},
		{"newline", "h\nost", "u\nser", "pa\nss", "my\ndb"},
		{"carriage_return", "h\rost", "u\rser", "pa\rss", "my\rdb"},
		{"unicode_space", "h\u2003ost", "u\u2003ser", "pa\u2003ss", "my\u2003db"},
		{"quote", "h'ost", "u'ser", "pa'ss", "my'db"},
		{"backslash", `h\ost`, `u\ser`, `pa\ss`, `my\db`},
		{"trailing_backslash", `host\`, `user\`, `pass\`, `db\`},
		{"mixed", `h '\ost`, `u '\ser`, `p '\ass`, `my '\db`},
		{"whitespace_only", " ", "\t", "\n", "\r"},
		{"key_like", "h port=1", "u password=x", "p dbname=x", "db sslmode=require"},
		{"symbols", "h@/:?%&=", "u@/:?%&=", "p@/:?%&=", "db@/:?%&="},
	}
	for mask := 0; mask < 16; mask++ {
		values := [4]string{"host", "user", "password", "database"}
		for i := range values {
			if mask&(1<<i) != 0 {
				values[i] = ""
			}
		}
		tests = append(tests, struct{ name, host, user, password, database string }{
			fmt.Sprintf("empty_mask_%04b", mask), values[0], values[1], values[2], values[3],
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateDBConfigPostgresEnv(t)
			cfg := DBConfig{
				Driver: "postgres", Host: tt.host, Port: 5433,
				Username: tt.user, Password: tt.password, Database: tt.database,
				Slaves: []DBSlaveConfig{
					{Host: tt.host, Port: 5434, Username: tt.user, Password: tt.password},
					{Host: "reader.host", Port: 5435, Username: "reader", Password: "read pass"},
				},
			}
			checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, endpoint DBSlaveConfig) {
				got, err := pq.NewConfig(dsn)
				if err != nil {
					t.Fatalf("pq.NewConfig(%q): %v", dsn, err)
				}
				for _, field := range []struct{ name, got, want string }{
					{"Host", got.Host, endpoint.Host}, {"User", got.User, endpoint.Username},
					{"Password", got.Password, endpoint.Password}, {"Database", got.Database, cfg.Database},
					{"SSLMode", string(got.SSLMode), "disable"},
				} {
					if field.got != field.want {
						t.Errorf("%s 期望 %q，实际 %q；DSN=%q", field.name, field.want, field.got, dsn)
					}
				}
				if got.Port != uint16(endpoint.Port) {
					t.Errorf("Port 期望 %d，实际 %d；DSN=%q", endpoint.Port, got.Port, dsn)
				}
				if len(got.Runtime) != 0 || len(got.Multi) != 0 {
					t.Errorf("不应产生额外参数或主机：Runtime=%v，Multi=%v", got.Runtime, got.Multi)
				}
				if endpoint.Password == "" && strings.Contains(dsn, " password=") {
					t.Errorf("空密码应省略 password 键：%q", dsn)
				}
				if cfg.Database == "" && strings.Contains(dsn, " dbname=") {
					t.Errorf("空库名应省略 dbname 键：%q", dsn)
				}
			})
		})
	}
}

// TestDBConfig_MySQLDSNRoundTrip 验证原始库名和常见时区经 mysql 回读保持原值，主从共享库名及时区且保留各自凭据，不连接数据库。
func TestDBConfig_MySQLDSNRoundTrip(t *testing.T) {
	for _, database := range []struct{ name, value string }{
		{"plain", "app"}, {"empty", ""}, {"question", "a?b"}, {"percent", "a%b"},
		{"ampersand", "a&b"}, {"equals", "a=b"}, {"slash", "a/b"},
		{"encoded_literal", "a%2Fb"}, {"mixed", "库 ?%&=/名"},
	} {
		for _, loc := range []struct{ name, value, want string }{
			{"default", "", "Local"}, {"local", "Local", "Local"}, {"utc", "UTC", "UTC"},
			{"shanghai", "Asia/Shanghai", "Asia/Shanghai"},
			{"new_york", "America/New_York", "America/New_York"},
			{"offset", "Etc/GMT+8", "Etc/GMT+8"},
		} {
			t.Run(database.name+"/"+loc.name, func(t *testing.T) {
				t.Setenv("ZONEINFO", "")
				cfg := DBConfig{
					Driver: "mysql", Host: "master.host", Port: 3307,
					Username: "master", Password: "master pass", Database: database.value, Loc: loc.value,
					Slaves: []DBSlaveConfig{
						{Host: "reader.host", Port: 3308, Username: "reader", Password: "reader pass"},
						{Host: "backup.host", Port: 3309, Username: "backup", Password: "backup pass"},
					},
				}
				checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, endpoint DBSlaveConfig) {
					got, err := mysql.ParseDSN(dsn)
					if err != nil {
						t.Fatalf("mysql.ParseDSN(%q): %v", dsn, err)
					}
					for _, field := range []struct{ name, got, want string }{
						{"DBName", got.DBName, database.value}, {"Loc", got.Loc.String(), loc.want},
						{"User", got.User, endpoint.Username}, {"Passwd", got.Passwd, endpoint.Password},
						{"Net", got.Net, "tcp"}, {"Addr", got.Addr, fmt.Sprintf("%s:%d", endpoint.Host, endpoint.Port)},
					} {
						if field.got != field.want {
							t.Errorf("%s 期望 %q，实际 %q；DSN=%q", field.name, field.want, field.got, dsn)
						}
					}
					if !got.ParseTime || len(got.Params) != 0 {
						t.Errorf("参数边界被改变：ParseTime=%v，Params=%v", got.ParseTime, got.Params)
					}
				})
			})
		}
	}
}

// TestDBConfig_MySQLPreencodedLoc 验证预编码时区按字面值编码一次，驱动报告该原文的未知时区而非兼容解码。
func TestDBConfig_MySQLPreencodedLoc(t *testing.T) {
	for _, loc := range []string{"Asia%2FShanghai", "Etc%2FGMT%2B8"} {
		t.Run(loc, func(t *testing.T) {
			t.Setenv("ZONEINFO", "")
			cfg := DBConfig{
				Driver: "mysql", Host: "master.host", Port: 3306, Username: "master", Database: "app", Loc: loc,
				Slaves: []DBSlaveConfig{{Host: "reader.host", Port: 3307, Username: "reader"}},
			}
			checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, _ DBSlaveConfig) {
				_, query, found := strings.Cut(dsn, "?")
				if !found {
					t.Fatalf("DSN 缺少参数：%q", dsn)
				}
				params, err := url.ParseQuery(query)
				if err != nil {
					t.Fatalf("解析查询参数 %q: %v", query, err)
				}
				if got := params["loc"]; !reflect.DeepEqual(got, []string{loc}) {
					t.Errorf("loc 解码一次应保留原文 %q，实际 %v", loc, got)
				}
				if _, err := mysql.ParseDSN(dsn); err == nil || err.Error() != "unknown time zone "+loc {
					t.Errorf("期望未知原文时区 %q，实际错误 %v", loc, err)
				}
			})
		})
	}
}

// TestDBConfig_MySQLCredentials 验证凭据允许字符无损回读，用户名冒号会分隔密码且百分号文本不会被 URL 解码。
func TestDBConfig_MySQLCredentials(t *testing.T) {
	for _, tt := range []struct{ name, user, password, wantUser, wantPassword string }{
		{"allowed_symbols", "u@/x", "p@/:x", "u@/x", "p@/:x"},
		{"literal_percent", "u%3Ax", "p%40%2F%3A", "u%3Ax", "p%40%2F%3A"},
		{"raw_percent", "u%x", "p%x", "u%x", "p%x"},
		{"colon_unsupported", "u:x", "p", "u", "x:p"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DBConfig{
				Driver: "mysql", Host: "master.host", Port: 3306, Username: tt.user, Password: tt.password, Database: "app",
				Slaves: []DBSlaveConfig{{Host: "reader.host", Port: 3307, Username: tt.user, Password: tt.password}},
			}
			checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, endpoint DBSlaveConfig) {
				want := fmt.Sprintf("%s:%s@tcp(%s:%d)/app?charset=utf8mb4&parseTime=true&loc=Local",
					tt.user, tt.password, endpoint.Host, endpoint.Port)
				if dsn != want {
					t.Errorf("凭据应原样输出，期望 %q，实际 %q", want, dsn)
				}
				got, err := mysql.ParseDSN(dsn)
				if err != nil {
					t.Fatalf("mysql.ParseDSN(%q): %v", dsn, err)
				}
				if got.User != tt.wantUser || got.Passwd != tt.wantPassword {
					t.Errorf("凭据期望 (%q, %q)，实际 (%q, %q)", tt.wantUser, tt.wantPassword, got.User, got.Passwd)
				}
			})
		})
	}
}

// TestDBConfig_DriverFormatOnly 验证别名及回退只选择 DSN 格式，不修改字段或注册驱动，空及未注册名称仍在连接前报错。
func TestDBConfig_DriverFormatOnly(t *testing.T) {
	for _, tt := range []struct{ driver, dialect, format string }{
		{"mysql", "postgres", "mysql"}, {"postgres", "mysql", "postgres"},
		{"pgsql", "mysql", "postgres"}, {"postgresql", "mysql", "postgres"},
		{"sqlite", "mysql", "sqlite"}, {"sqlite3", "mysql", "sqlite"},
		{"", "mysql", "mysql"}, {"", "postgres", "postgres"},
		{"", "pgsql", "postgres"}, {"", "postgresql", "postgres"},
		{"", "sqlite", "sqlite"}, {"", "sqlite3", "sqlite"},
		{"oracle", "mysql", ""}, {"", "oracle", ""}, {"", "", ""},
	} {
		t.Run("driver="+tt.driver+"/dialect="+tt.dialect, func(t *testing.T) {
			cfg := DBConfig{
				Driver: tt.driver, Dialect: tt.dialect, Host: "master.host", Port: 1234,
				Username: "master", Password: "pass", Database: "app",
				Slaves: []DBSlaveConfig{{Host: "reader.host", Port: 1235, Username: "reader", Password: "read"}},
			}
			drivers := sql.Drivers()
			checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, endpoint DBSlaveConfig) {
				var want string
				switch tt.format {
				case "mysql":
					want = fmt.Sprintf("%s:%s@tcp(%s:%d)/app?charset=utf8mb4&parseTime=true&loc=Local",
						endpoint.Username, endpoint.Password, endpoint.Host, endpoint.Port)
				case "postgres":
					want = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=app sslmode=disable",
						endpoint.Host, endpoint.Port, endpoint.Username, endpoint.Password)
				case "sqlite":
					want = "app"
				}
				if dsn != want {
					t.Errorf("DSN 期望 %q，实际 %q", want, dsn)
				}
			})
			if got := sql.Drivers(); !reflect.DeepEqual(got, drivers) {
				t.Errorf("DSN 生成不应注册驱动：之前 %v，之后 %v", drivers, got)
			}
			if !slices.Contains(drivers, cfg.Driver) {
				db, err := sql.Open(cfg.Driver, cfg.GetMasterDSN())
				if db != nil {
					t.Cleanup(func() {
						if err := db.Close(); err != nil {
							t.Errorf("关闭意外创建的连接池: %v", err)
						}
					})
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("unknown driver %q", cfg.Driver)) {
					t.Errorf("空或未注册的 Driver %q 应在连接前报未知驱动，实际 %v", cfg.Driver, err)
				}
			}
		})
	}
}

// TestDBConfig_Charset 验证 MySQL 默认值及合法字符集列表原样传递，PostgreSQL 主从完全忽略 Charset 并维持 UTF8。
func TestDBConfig_Charset(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		for _, charset := range []string{"", "utf8mb4", "gbk", "utf8mb4,utf8", "LATIN1"} {
			t.Run(driver+"/charset="+charset, func(t *testing.T) {
				isolateDBConfigPostgresEnv(t)
				cfg := DBConfig{
					Driver: driver, Host: "master.host", Port: 1234, Username: "master", Password: "pass",
					Database: "app", Charset: charset,
					Slaves: []DBSlaveConfig{{Host: "reader.host", Port: 1235, Username: "reader", Password: "read"}},
				}
				checkDBConfigDSNs(t, cfg, func(t *testing.T, dsn string, endpoint DBSlaveConfig) {
					if driver == "postgres" {
						want := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=app sslmode=disable",
							endpoint.Host, endpoint.Port, endpoint.Username, endpoint.Password)
						if dsn != want {
							t.Errorf("PostgreSQL 应忽略 Charset，期望 %q，实际 %q", want, dsn)
						}
						got, err := pq.NewConfig(dsn)
						if err != nil {
							t.Fatalf("pq.NewConfig(%q): %v", dsn, err)
						}
						if got.ClientEncoding != "UTF8" || len(got.Runtime) != 0 {
							t.Errorf("Charset 不应影响 PostgreSQL：ClientEncoding=%q，Runtime=%v", got.ClientEncoding, got.Runtime)
						}
						return
					}
					wantCharset := charset
					if wantCharset == "" {
						wantCharset = "utf8mb4"
					}
					want := fmt.Sprintf("%s:%s@tcp(%s:%d)/app?charset=%s&parseTime=true&loc=Local",
						endpoint.Username, endpoint.Password, endpoint.Host, endpoint.Port, wantCharset)
					if dsn != want {
						t.Errorf("字符集列表应原样输出，期望 %q，实际 %q", want, dsn)
					}
					got, err := mysql.ParseDSN(dsn)
					if err != nil {
						t.Fatalf("mysql.ParseDSN(%q): %v", dsn, err)
					}
					wantFormatted := fmt.Sprintf("%s:%s@tcp(%s:%d)/app?charset=%s&loc=Local&parseTime=true",
						endpoint.Username, endpoint.Password, endpoint.Host, endpoint.Port, wantCharset)
					if formatted := got.FormatDSN(); formatted != wantFormatted {
						t.Errorf("驱动回读字符集后期望 %q，实际 %q", wantFormatted, formatted)
					}
				})
			})
		}
	}
}
