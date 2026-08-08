package zcmodel

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/buexplain/zckg/zcdb"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// ==================== 数据库连接辅助（参考 zcdb 集成测试） ====================

// openSQLiteDAO 打开共享内存 SQLite 数据库，测试结束后自动关闭。
func openSQLiteDAO(t *testing.T) *zcdb.DBDao {
	t.Helper()
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "sqlite",
		DSN:        "file:zcmodel_integ?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	dao, err := zcdb.NewDBDao(pool, "sqlite", nil, "")
	if err != nil {
		t.Fatalf("failed to create dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	return dao
}

// openMySQLDAO 打开本地 MySQL 连接（与 zcdb 集成测试同一 DSN），不可用时跳过测试。
// docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
func openMySQLDAO(t *testing.T) *zcdb.DBDao {
	t.Helper()
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "root:root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=Local",
	})
	if err != nil {
		t.Skipf("mysql 不可用，跳过集成测试: %v", err)
	}
	dao, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		t.Fatalf("failed to create mysql dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	if err := dao.Pool().Ping(context.Background()); err != nil {
		t.Skipf("mysql 不可用，跳过集成测试: %v", err)
	}
	// 创建测试数据库（若不存在）并切换，清理旧表
	mustExec(t, dao, "CREATE DATABASE IF NOT EXISTS `zckg_test_integ` DEFAULT CHARACTER SET utf8mb4")
	mustExec(t, dao, "USE `zckg_test_integ`")
	mustExec(t, dao, "DROP TABLE IF EXISTS `all_types`")
	return dao
}

// openPgDAO 打开本地 PostgreSQL 连接（与 zcdb 集成测试同一 DSN），不可用时跳过测试。
// docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
func openPgDAO(t *testing.T) *zcdb.DBDao {
	t.Helper()
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable",
	})
	if err != nil {
		t.Skipf("postgres 不可用，跳过集成测试: %v", err)
	}
	dao, err := zcdb.NewDBDao(pool, "postgres", nil, "")
	if err != nil {
		t.Fatalf("failed to create postgres dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	if err := dao.Pool().Ping(context.Background()); err != nil {
		t.Skipf("postgres 不可用，跳过集成测试: %v", err)
	}
	// 创建测试数据库（若不存在）
	exists, err := dao.Builder().Table("pg_database").Where("datname", "=", "zckg_test_integ").Exists(context.Background())
	if err != nil {
		t.Fatalf("failed to check database existence: %v", err)
	}
	if !exists {
		mustExec(t, dao, "CREATE DATABASE zckg_test_integ")
	}
	_ = dao.Close()

	// 重新连接到测试数据库
	pool, err = zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable dbname=zckg_test_integ",
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	dao, err = zcdb.NewDBDao(pool, "postgres", nil, "")
	if err != nil {
		t.Fatalf("failed to create postgres dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	mustExec(t, dao, "DROP TABLE IF EXISTS all_types CASCADE")
	return dao
}

// mustExec 执行 SQL，失败则 Fatal。
func mustExec(t *testing.T, dao *zcdb.DBDao, query string, args ...any) {
	t.Helper()
	if _, err := dao.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("exec failed: %s\nerror: %v", query, err)
	}
}

// ==================== 建表 DDL（覆盖各方言映射表支持的全部存储类型） ====================

// mysqlAllTypesDDL MySQL 表，覆盖 makeColumnTypeToGoTypeMap 中 MySQL 的全部存储类型键。
const mysqlAllTypesDDL = `CREATE TABLE all_types (
	c_tinyint     TINYINT COMMENT 'tinyint 整数',
	c_smallint    SMALLINT COMMENT 'smallint 整数',
	c_mediumint   MEDIUMINT COMMENT 'mediumint 整数',
	c_int         INT COMMENT 'int 整数',
	c_integer     INTEGER COMMENT 'integer 整数',
	c_bigint      BIGINT COMMENT 'bigint 大整数',
	c_year        YEAR COMMENT 'year 年份',
	c_float       FLOAT COMMENT 'float 浮点数',
	c_double      DOUBLE COMMENT 'double 浮点数',
	c_decimal     DECIMAL(10,2) COMMENT 'decimal 定点数',
	c_numeric     NUMERIC(10,2) COMMENT 'numeric 定点数',
	c_bool        BOOL COMMENT 'bool 布尔',
	c_boolean     BOOLEAN COMMENT 'boolean 布尔',
	c_char        CHAR(10) COMMENT 'char 定长字符串',
	c_varchar     VARCHAR(255) COMMENT 'varchar 变长字符串',
	c_tinytext    TINYTEXT COMMENT 'tinytext 小文本',
	c_text        TEXT COMMENT 'text 文本',
	c_mediumtext  MEDIUMTEXT COMMENT 'mediumtext 中文本',
	c_longtext    LONGTEXT COMMENT 'longtext 大文本',
	c_enum        ENUM('a','b') COMMENT 'enum 枚举',
	c_set         SET('a','b') COMMENT 'set 集合',
	c_json        JSON COMMENT 'json 文档',
	c_date        DATE COMMENT 'date 日期',
	c_time        TIME COMMENT 'time 时间',
	c_datetime    DATETIME COMMENT 'datetime 日期时间',
	c_timestamp   TIMESTAMP NULL COMMENT 'timestamp 时间戳',
	c_binary      BINARY(16) COMMENT 'binary 定长二进制',
	c_varbinary   VARBINARY(255) COMMENT 'varbinary 变长二进制',
	c_tinyblob    TINYBLOB COMMENT 'tinyblob 小二进制',
	c_blob        BLOB COMMENT 'blob 二进制',
	c_mediumblob  MEDIUMBLOB COMMENT 'mediumblob 中二进制',
	c_longblob    LONGBLOB COMMENT 'longblob 大二进制'
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

// pgAllTypesDDL PostgreSQL 表，覆盖 makeColumnTypeToGoTypeMap 中 Postgres 的全部存储类型键。
// 注意：PostgreSQL 建表语法不支持内联 COMMENT 子句（与 MySQL 不同），
// 字段注释由 addPgColumnComments 在建表后通过 COMMENT ON COLUMN 补充。
const pgAllTypesDDL = `CREATE TABLE all_types (
	c_smallint          SMALLINT,
	c_int2              INT2,
	c_integer           INTEGER,
	c_int4              INT4,
	c_bigint            BIGINT,
	c_int8              INT8,
	c_smallserial       SMALLSERIAL,
	c_serial            SERIAL,
	c_bigserial         BIGSERIAL,
	c_real              REAL,
	c_float4            FLOAT4,
	c_double_precision  DOUBLE PRECISION,
	c_float8            FLOAT8,
	c_numeric           NUMERIC(10,2),
	c_decimal           DECIMAL(10,2),
	c_money             MONEY,
	c_boolean           BOOLEAN,
	c_bool              BOOL,
	c_varchar           VARCHAR(255),
	c_character_varying CHARACTER VARYING(10),
	c_char              CHAR(10),
	c_character         CHARACTER(10),
	c_bpchar            BPCHAR,
	c_text              TEXT,
	c_uuid              UUID,
	c_json              JSON,
	c_jsonb             JSONB,
	c_inet              INET,
	c_interval          INTERVAL,
	c_date              DATE,
	c_time              TIME,
	c_timetz            TIMETZ,
	c_timestamp         TIMESTAMP,
	c_timestamptz       TIMESTAMPTZ,
	c_bytea             BYTEA
)`

// sqliteAllTypesDDL SQLite 表，覆盖 makeColumnTypeToGoTypeMap 中 SQLite 的全部存储类型键。
// 注意：SQLite 不支持字段注释（无 COMMENT 语法，元数据中也不存储注释），
// 因此建表语句无法声明注释，生成代码的 description tag 恒为空。
const sqliteAllTypesDDL = `CREATE TABLE all_types (
	c_tinyint             TINYINT,
	c_smallint            SMALLINT,
	c_mediumint           MEDIUMINT,
	c_int                 INT,
	c_integer             INTEGER,
	c_bigint              BIGINT,
	c_int2                INT2,
	c_int8                INT8,
	c_unsigned_big_int    UNSIGNED BIG INT,
	c_real                REAL,
	c_double              DOUBLE,
	c_double_precision    DOUBLE PRECISION,
	c_float               FLOAT,
	c_numeric             NUMERIC,
	c_decimal             DECIMAL(10,2),
	c_boolean             BOOLEAN,
	c_bool                BOOL,
	c_character           CHARACTER(10),
	c_varchar             VARCHAR(255),
	c_varying_character   VARYING CHARACTER(255),
	c_nchar               NCHAR(10),
	c_native_character    NATIVE CHARACTER(10),
	c_nvarchar            NVARCHAR(255),
	c_text                TEXT,
	c_clob                CLOB,
	c_blob                BLOB,
	c_date                DATE,
	c_datetime            DATETIME,
	c_timestamp           TIMESTAMP,
	c_json                JSON
)`

// ==================== 期望的类型映射（列名 → 生成的 Go 类型） ====================

// colComments 各列的字段注释，MySQL 建表与 PostgreSQL COMMENT ON COLUMN 共用。
// SQLite 不支持字段注释，生成代码中 description tag 始终为空。
var colComments = map[string]string{
	"c_tinyint":           "tinyint 整数",
	"c_smallint":          "smallint 整数",
	"c_mediumint":         "mediumint 整数",
	"c_int":               "int 整数",
	"c_integer":           "integer 整数",
	"c_bigint":            "bigint 大整数",
	"c_year":              "year 年份",
	"c_float":             "float 浮点数",
	"c_double":            "double 浮点数",
	"c_decimal":           "decimal 定点数",
	"c_numeric":           "numeric 定点数",
	"c_bool":              "bool 布尔",
	"c_boolean":           "boolean 布尔",
	"c_char":              "char 定长字符串",
	"c_varchar":           "varchar 变长字符串",
	"c_tinytext":          "tinytext 小文本",
	"c_text":              "text 文本",
	"c_mediumtext":        "mediumtext 中文本",
	"c_longtext":          "longtext 大文本",
	"c_enum":              "enum 枚举",
	"c_set":               "set 集合",
	"c_json":              "json 文档",
	"c_date":              "date 日期",
	"c_time":              "time 时间",
	"c_datetime":          "datetime 日期时间",
	"c_timestamp":         "timestamp 时间戳",
	"c_binary":            "binary 定长二进制",
	"c_varbinary":         "varbinary 变长二进制",
	"c_tinyblob":          "tinyblob 小二进制",
	"c_blob":              "blob 二进制",
	"c_mediumblob":        "mediumblob 中二进制",
	"c_longblob":          "longblob 大二进制",
	"c_int2":              "int2 整数",
	"c_int4":              "int4 整数",
	"c_int8":              "int8 大整数",
	"c_smallserial":       "smallserial 自增整数",
	"c_serial":            "serial 自增整数",
	"c_bigserial":         "bigserial 自增大整数",
	"c_real":              "real 浮点数",
	"c_float4":            "float4 浮点数",
	"c_double_precision":  "double precision 双精度",
	"c_float8":            "float8 双精度",
	"c_money":             "money 货币",
	"c_character_varying": "character varying 变长字符串",
	"c_character":         "character 定长字符串",
	"c_bpchar":            "bpchar 定长字符串",
	"c_uuid":              "uuid 唯一标识",
	"c_jsonb":             "jsonb 文档",
	"c_inet":              "inet 网络地址",
	"c_interval":          "interval 时间间隔",
	"c_timetz":            "timetz 带时区时间",
	"c_timestamptz":       "timestamptz 带时区时间戳",
	"c_bytea":             "bytea 二进制",
	"c_unsigned_big_int":  "unsigned big int 大整数",
	"c_varying_character": "varying character 变长字符串",
	"c_nchar":             "nchar 定长字符串",
	"c_native_character":  "native character 定长字符串",
	"c_nvarchar":          "nvarchar 变长字符串",
	"c_clob":              "clob 大文本",
}

// mysqlWantTypes 期望的 MySQL 各列 Go 类型。
// 注意：MySQL 的 BOOL/BOOLEAN 实际存储类型为 TINYINT(1)，读表结构后归一化为 tinyint，映射为 int。
var mysqlWantTypes = map[string]string{
	"c_tinyint":    "int",
	"c_smallint":   "int",
	"c_mediumint":  "int",
	"c_int":        "int",
	"c_integer":    "int",
	"c_bigint":     "int64",
	"c_year":       "int",
	"c_float":      "float64",
	"c_double":     "float64",
	"c_decimal":    "float64",
	"c_numeric":    "float64",
	"c_bool":       "int",
	"c_boolean":    "int",
	"c_char":       "string",
	"c_varchar":    "string",
	"c_tinytext":   "string",
	"c_text":       "string",
	"c_mediumtext": "string",
	"c_longtext":   "string",
	"c_enum":       "string",
	"c_set":        "string",
	"c_json":       "string",
	"c_date":       "time.Time",
	"c_time":       "time.Time",
	"c_datetime":   "time.Time",
	"c_timestamp":  "time.Time",
	"c_binary":     "[]byte",
	"c_varbinary":  "[]byte",
	"c_tinyblob":   "[]byte",
	"c_blob":       "[]byte",
	"c_mediumblob": "[]byte",
	"c_longblob":   "[]byte",
}

// pgWantTypes 期望的 PostgreSQL 各列 Go 类型。
// format_type 对别名（INT2/INT4/INT8/FLOAT4/FLOAT8/BPCHAR）返回规范名，对时间类型返回带时区后缀的全名，
// normalizeColumnType 会将其归一化为映射表可匹配的键。
var pgWantTypes = map[string]string{
	"c_smallint":          "int",
	"c_int2":              "int",
	"c_integer":           "int",
	"c_int4":              "int",
	"c_bigint":            "int64",
	"c_int8":              "int64",
	"c_smallserial":       "int",
	"c_serial":            "int",
	"c_bigserial":         "int64",
	"c_real":              "float64",
	"c_float4":            "float64",
	"c_double_precision":  "float64",
	"c_float8":            "float64",
	"c_numeric":           "float64",
	"c_decimal":           "float64",
	"c_money":             "string",
	"c_boolean":           "bool",
	"c_bool":              "bool",
	"c_varchar":           "string",
	"c_character_varying": "string",
	"c_char":              "string",
	"c_character":         "string",
	"c_bpchar":            "string",
	"c_text":              "string",
	"c_uuid":              "string",
	"c_json":              "string",
	"c_jsonb":             "string",
	"c_inet":              "string",
	"c_interval":          "string",
	"c_date":              "time.Time",
	"c_time":              "time.Time",
	"c_timetz":            "time.Time",
	"c_timestamp":         "time.Time",
	"c_timestamptz":       "time.Time",
	"c_bytea":             "[]byte",
}

// sqliteWantTypes 期望的 SQLite 各列 Go 类型。
// PRAGMA table_info 原样返回建表时声明的类型名，normalizeColumnType 会做小写与去引号处理。
var sqliteWantTypes = map[string]string{
	"c_tinyint":           "int",
	"c_smallint":          "int",
	"c_mediumint":         "int",
	"c_int":               "int",
	"c_integer":           "int",
	"c_bigint":            "int64",
	"c_int2":              "int",
	"c_int8":              "int64",
	"c_unsigned_big_int":  "int64",
	"c_real":              "float64",
	"c_double":            "float64",
	"c_double_precision":  "float64",
	"c_float":             "float64",
	"c_numeric":           "float64",
	"c_decimal":           "float64",
	"c_boolean":           "bool",
	"c_bool":              "bool",
	"c_character":         "string",
	"c_varchar":           "string",
	"c_varying_character": "string",
	"c_nchar":             "string",
	"c_native_character":  "string",
	"c_nvarchar":          "string",
	"c_text":              "string",
	"c_clob":              "string",
	"c_blob":              "[]byte",
	"c_date":              "time.Time",
	"c_datetime":          "time.Time",
	"c_timestamp":         "time.Time",
	"c_json":              "string",
}

// ==================== 测试用例 ====================

// TestInteg_MySQL_AllTypes 在真实 MySQL 上建一张覆盖全部存储类型的表（含字段注释），生成模型代码并验证类型映射与 description tag。
func TestInteg_MySQL_AllTypes(t *testing.T) {
	dao := openMySQLDAO(t)
	mustExec(t, dao, mysqlAllTypesDDL)
	generateAndVerify(t, dao, DialectMysql, "zckg_test_integ", mysqlWantTypes, colComments)
}

// TestInteg_Postgres_AllTypes 在真实 PostgreSQL 上建一张覆盖全部存储类型的表，
// 建表后通过 COMMENT ON COLUMN 补充字段注释，生成模型代码并验证类型映射与 description tag。
func TestInteg_Postgres_AllTypes(t *testing.T) {
	dao := openPgDAO(t)
	mustExec(t, dao, pgAllTypesDDL)
	addPgColumnComments(t, dao, pgWantTypes)
	generateAndVerify(t, dao, DialectPostgres, "zckg_test_integ", pgWantTypes, colComments)
}

// addPgColumnComments 为 PG 测试表补充字段注释。
// PostgreSQL 建表语法不支持内联 COMMENT 子句（与 MySQL 不同），
// 只能建表后逐列执行 COMMENT ON COLUMN 语句；lib/pq 驱动一次 Exec 仅支持单条语句，故循环执行。
func addPgColumnComments(t *testing.T, dao *zcdb.DBDao, wantTypes map[string]string) {
	t.Helper()
	for colName := range wantTypes {
		mustExec(t, dao, fmt.Sprintf("COMMENT ON COLUMN all_types.%s IS '%s'", colName, colComments[colName]))
	}
}

// TestInteg_SQLite_AllTypes 在内存 SQLite 上建一张覆盖全部存储类型的表，生成模型代码并验证类型映射。
// SQLite 不支持字段注释，description tag 不做验证。
func TestInteg_SQLite_AllTypes(t *testing.T) {
	dao := openSQLiteDAO(t)
	mustExec(t, dao, sqliteAllTypesDDL)
	generateAndVerify(t, dao, DialectSqlite, "sqlite", sqliteWantTypes, nil)
}

// generateAndVerify 从数据库读取 all_types 表结构，组装 Input 调用 Generate 生成模型代码，
// 然后逐列验证生成的字段名、Go 类型、json tag 与 db tag；wantComments 非 nil 时额外验证 description tag。
func generateAndVerify(t *testing.T, dao *zcdb.DBDao, dialect Dialect, database string, wantTypes, wantComments map[string]string) {
	t.Helper()

	// 通过 SchemaInspector 读取真实表结构
	inspector, err := dao.Schema()
	if err != nil {
		t.Fatalf("创建 SchemaInspector 失败: %v", err)
	}
	cols, err := inspector.Columns(context.Background(), "all_types")
	if err != nil {
		t.Fatalf("读取表结构失败: %v", err)
	}
	if len(cols) == 0 {
		t.Fatalf("表 all_types 未读取到字段")
	}

	// 建表 DDL 必须覆盖期望表中的所有列，防止 DDL 漏列导致断言空转
	seen := make(map[string]bool, len(cols))
	for _, c := range cols {
		seen[c.Name] = true
	}
	for colName := range wantTypes {
		if !seen[colName] {
			t.Errorf("建表 DDL 缺少列 %s", colName)
		}
	}

	columns := make([]*Column, 0, len(cols))
	for _, c := range cols {
		columns = append(columns, &Column{Name: c.Name, Type: c.Type, Comment: c.Comment})
	}

	// 输出目录名需为合法 Go 包名（writeOrReplaceStruct 以目录名推导包名）
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         database,
		Dialect:          dialect,
		TableName:        "all_types",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns:          columns,
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "all_types.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)

	// 生成的代码必须能通过 go/parser 解析，确保没有语法错误（含结构体、方法、tag 等全部内容）
	if _, err := parser.ParseFile(token.NewFileSet(), "all_types.go", got, parser.AllErrors); err != nil {
		t.Errorf("生成的模型代码存在语法错误: %v", err)
	}
	// 生成代码引用 time.Time（date/datetime/timestamp 等列）时必须自动引入 time 包，否则无法编译
	if strings.Contains(got, "time.Time") && !strings.Contains(got, `import "time"`) {
		t.Errorf("生成文件包含 time.Time 字段但缺少 import \"time\"")
	}

	for _, s := range []string{"type AllTypesEntity struct {", "type AllTypesDO struct {"} {
		if !strings.Contains(got, s) {
			t.Errorf("生成文件缺少: %s", s)
		}
	}

	// 逐列验证字段名、Go 类型、json tag、db tag，wantComments 非 nil 时验证 description tag
	for colName, goType := range wantTypes {
		fname := fieldNameOf(colName)
		// 字段行格式：字段名 + 空白 + Go 类型 + 空白（buildStruct 按 gofmt 风格对齐）
		if re := regexp.MustCompile(regexp.QuoteMeta(fname) + `\s+` + regexp.QuoteMeta(goType) + `\s+`); !re.MatchString(got) {
			t.Errorf("列 %s 未生成期望类型 %s（字段 %s）", colName, goType, fname)
		}
		if !strings.Contains(got, `db:"`+colName+`"`) {
			t.Errorf("列 %s 缺少 db tag", colName)
		}
		jsonVal := strings.ToLower(fname[:1]) + fname[1:]
		if !strings.Contains(got, `json:"`+jsonVal+`"`) {
			t.Errorf("列 %s 缺少 json tag %s", colName, jsonVal)
		}
		if wantComments != nil {
			if comment, ok := wantComments[colName]; ok && comment != "" {
				if !strings.Contains(got, `description:"`+comment+`"`) {
					t.Errorf("列 %s 缺少 description tag %s", colName, comment)
				}
			}
		}
	}
}

// fieldNameOf 将列名 c_tinyint 转换为生成的字段名 CTinyint（与 toPascalCase 输出一致）。
func fieldNameOf(colName string) string {
	parts := strings.Split(colName, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		b.WriteString(p[1:])
	}
	return b.String()
}
