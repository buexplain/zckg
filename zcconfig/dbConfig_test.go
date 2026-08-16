package zcconfig

import "testing"

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
			want: "root:@tcp(127.0.0.1:3306)/test_db?charset=gbk&parseTime=true&loc=Asia/Shanghai",
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

// TestDBConfig_GetSlaveDSN 验证从库 DSN 列表组装：复用主库的 Database / Charset / Loc，
// 仅 Host / Port / Username / Password 取自各自的 DBSlaveConfig。
func TestDBConfig_GetSlaveDSN(t *testing.T) {
	cfg := DBConfig{
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
	}

	want := []string{
		"root:root@tcp(127.0.0.1:3307)/test_db?charset=utf8mb4&parseTime=true&loc=Local",
		"reader:rpass@tcp(10.0.0.2:3306)/test_db?charset=utf8mb4&parseTime=true&loc=Local",
	}
	got := cfg.GetSlaveDSN()
	if len(got) != len(want) {
		t.Fatalf("期望 %d 个从库 DSN，实际 %d：%v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个从库 DSN 期望 %q，实际 %q", i, want[i], got[i])
		}
	}
}

// TestDBConfig_GetSlaveDSN_Empty 验证无从库时返回 nil。
func TestDBConfig_GetSlaveDSN_Empty(t *testing.T) {
	cfg := DBConfig{Driver: "mysql", Database: "test_db"}
	if got := cfg.GetSlaveDSN(); got != nil {
		t.Errorf("Slaves 为空时期望 nil，实际 %v", got)
	}
}
