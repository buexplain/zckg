package zcmodel

import "testing"

// TestNormalizeColumnType 验证列类型归一化的三方言代表规则：
// 去除长度/精度与枚举值列表、MySQL 的 unsigned/zerofill 修饰、
// PostgreSQL 的时区后缀规范化、SQLite 的引号剥离，
// 以及空字符串与未知方言（走通用处理）两个边界。
func TestNormalizeColumnType(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		colType string
		want    string
	}{
		// MySQL
		{name: "MySQL varchar 带长度", dialect: DialectMysql, colType: "VARCHAR(255)", want: "varchar"},
		{name: "MySQL bigint 带长度", dialect: DialectMysql, colType: "bigint(20)", want: "bigint"},
		{name: "MySQL bigint unsigned 带长度", dialect: DialectMysql, colType: "bigint(20) unsigned", want: "bigint"},
		{name: "MySQL int unsigned 无长度", dialect: DialectMysql, colType: "int unsigned", want: "int"},
		{name: "MySQL float zerofill", dialect: DialectMysql, colType: "float zerofill", want: "float"},
		{name: "MySQL decimal 精度", dialect: DialectMysql, colType: "decimal(10,2)", want: "decimal"},
		{name: "MySQL enum 值列表", dialect: DialectMysql, colType: "ENUM('a','b')", want: "enum"},
		{name: "MySQL 前后空格", dialect: DialectMysql, colType: "  datetime  ", want: "datetime"},
		{name: "MySQL 无修饰", dialect: DialectMysql, colType: "text", want: "text"},

		// PostgreSQL
		{name: "Postgres character varying 带长度", dialect: DialectPostgres, colType: "character varying(255)", want: "character varying"},
		{name: "Postgres 大写 character varying", dialect: DialectPostgres, colType: "CHARACTER VARYING(255)", want: "character varying"},
		{name: "Postgres character 带长度", dialect: DialectPostgres, colType: "character(10)", want: "character"},
		{name: "Postgres double precision", dialect: DialectPostgres, colType: "double precision", want: "double precision"},
		{name: "Postgres numeric 精度", dialect: DialectPostgres, colType: "numeric(10,2)", want: "numeric"},
		{name: "Postgres timestamp with time zone", dialect: DialectPostgres, colType: "timestamp with time zone", want: "timestamptz"},
		{name: "Postgres timestamp without time zone", dialect: DialectPostgres, colType: "timestamp without time zone", want: "timestamp"},
		{name: "Postgres time with time zone", dialect: DialectPostgres, colType: "time with time zone", want: "timetz"},
		{name: "Postgres time without time zone", dialect: DialectPostgres, colType: "time without time zone", want: "time"},
		{name: "Postgres timestamp 带精度带时区", dialect: DialectPostgres, colType: "timestamp(6) with time zone", want: "timestamptz"},
		{name: "Postgres uuid", dialect: DialectPostgres, colType: "uuid", want: "uuid"},
		{name: "Postgres bigserial", dialect: DialectPostgres, colType: "bigserial", want: "bigserial"},

		// SQLite
		{name: "SQLite varchar 带长度", dialect: DialectSqlite, colType: "VARCHAR(255)", want: "varchar"},
		{name: "SQLite integer", dialect: DialectSqlite, colType: "INTEGER", want: "integer"},
		{name: "SQLite unsigned big int", dialect: DialectSqlite, colType: "UNSIGNED BIG INT", want: "unsigned big int"},
		{name: "SQLite double precision", dialect: DialectSqlite, colType: "DOUBLE PRECISION", want: "double precision"},
		{name: "SQLite varying character 带长度", dialect: DialectSqlite, colType: "VARYING CHARACTER(10)", want: "varying character"},
		{name: "SQLite native character 带长度", dialect: DialectSqlite, colType: "NATIVE CHARACTER(70)", want: "native character"},
		{name: "SQLite bigint 带长度", dialect: DialectSqlite, colType: "BIGINT(20)", want: "bigint"},
		{name: "SQLite 带引号类型名", dialect: DialectSqlite, colType: `"text"`, want: "text"},
		{name: "SQLite clob", dialect: DialectSqlite, colType: "CLOB", want: "clob"},

		// 边界情况
		{name: "空字符串", dialect: DialectMysql, colType: "", want: ""},
		{name: "未知方言走通用处理", dialect: Dialect("unknown"), colType: "VARCHAR(255)", want: "varchar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeColumnType(tt.dialect, tt.colType); got != tt.want {
				t.Errorf("normalizeColumnType(%q, %s) = %q, want %q", tt.colType, tt.dialect, got, tt.want)
			}
		})
	}
}

// TestNormalizeColumnTypeMatchesMap 验证 normalizeColumnType 的输出能命中 makeColumnTypeToGoTypeMap 的键
func TestNormalizeColumnTypeMatchesMap(t *testing.T) {
	// 覆盖三种方言映射表里的部分代表键：转换后必须在 map 中可匹配
	tests := []struct {
		name    string
		dialect Dialect
		colType string
	}{
		// MySQL
		{name: "MySQL varchar", dialect: DialectMysql, colType: "varchar(255)"},
		{name: "MySQL bigint", dialect: DialectMysql, colType: "bigint(20)"},
		{name: "MySQL timestamp", dialect: DialectMysql, colType: "timestamp"},
		{name: "MySQL blob", dialect: DialectMysql, colType: "BLOB"},
		// PostgreSQL
		{name: "Postgres character varying", dialect: DialectPostgres, colType: "character varying(255)"},
		{name: "Postgres timestamptz", dialect: DialectPostgres, colType: "timestamp with time zone"},
		{name: "Postgres double precision", dialect: DialectPostgres, colType: "double precision"},
		{name: "Postgres bytea", dialect: DialectPostgres, colType: "bytea"},
		// SQLite
		{name: "SQLite unsigned big int", dialect: DialectSqlite, colType: "UNSIGNED BIG INT"},
		{name: "SQLite varying character", dialect: DialectSqlite, colType: "VARYING CHARACTER(10)"},
		{name: "SQLite blob", dialect: DialectSqlite, colType: "blob"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := normalizeColumnType(tt.dialect, tt.colType)
			columnTypeToGoType := makeColumnTypeToGoTypeMap(tt.dialect)
			if _, ok := columnTypeToGoType[key]; !ok {
				t.Errorf("normalizeColumnType(%q, %s) = %q, 未命中 makeColumnTypeToGoTypeMap 的键", tt.colType, tt.dialect, key)
			}
		})
	}
}

// TestFormatStructFieldType 验证列类型映射入口：三方言分别覆盖全部 Go 目标类型类别
// （int / int64 / float64 / bool / string / time.Time / []byte），
// 未知方言无映射表返回空串（由 Generate 兜底为 string）。
func TestFormatStructFieldType(t *testing.T) {
	tests := []struct {
		name    string
		dialect Dialect
		colType string
		want    string
	}{
		// MySQL：覆盖全部 Go 目标类型
		{"MySQL int", DialectMysql, "int(11)", "int"},
		{"MySQL int64", DialectMysql, "bigint(20)", "int64"},
		{"MySQL float64", DialectMysql, "decimal(10,2)", "float64"},
		{"MySQL bool", DialectMysql, "boolean", "bool"},
		{"MySQL string", DialectMysql, "varchar(255)", "string"},
		{"MySQL time.Time", DialectMysql, "datetime", "time.Time"},
		{"MySQL []byte", DialectMysql, "blob", "[]byte"},

		// PostgreSQL：覆盖全部 Go 目标类型
		{"Postgres int", DialectPostgres, "integer", "int"},
		{"Postgres int64", DialectPostgres, "bigint", "int64"},
		{"Postgres float64", DialectPostgres, "double precision", "float64"},
		{"Postgres bool", DialectPostgres, "boolean", "bool"},
		{"Postgres string", DialectPostgres, "character varying(255)", "string"},
		{"Postgres time.Time", DialectPostgres, "timestamp with time zone", "time.Time"},
		{"Postgres []byte", DialectPostgres, "bytea", "[]byte"},

		// SQLite：覆盖全部 Go 目标类型
		{"SQLite int", DialectSqlite, "INTEGER", "int"},
		{"SQLite int64", DialectSqlite, "UNSIGNED BIG INT", "int64"},
		{"SQLite float64", DialectSqlite, "DOUBLE PRECISION", "float64"},
		{"SQLite bool", DialectSqlite, "boolean", "bool"},
		{"SQLite string", DialectSqlite, "VARYING CHARACTER(10)", "string"},
		{"SQLite time.Time", DialectSqlite, "datetime", "time.Time"},
		{"SQLite []byte", DialectSqlite, "blob", "[]byte"},

		{"未知方言无映射表", Dialect("oracle"), "bigint", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatStructFieldType(tt.dialect, tt.colType); got != tt.want {
				t.Errorf("formatStructFieldType(%q, %q) = %q, want %q", tt.dialect, tt.colType, got, tt.want)
			}
		})
	}
}
