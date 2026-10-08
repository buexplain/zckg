package zcmodel

import (
	"context"
	"fmt"
	"go/format"
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
	// 连接池创建成功即注册清理，避免后续 NewDBDao 失败时泄漏；Pool.Close 幂等，可与 dao.Close 共存
	t.Cleanup(func() { _ = pool.Close() })
	dao, err := zcdb.NewDBDao(pool, "sqlite", nil, "", "")
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
	// 连接池创建成功即注册清理，避免后续 NewDBDao 失败时泄漏；Pool.Close 幂等，可与 dao.Close 共存
	t.Cleanup(func() { _ = pool.Close() })
	dao, err := zcdb.NewDBDao(pool, "mysql", nil, "", "")
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
	adminPool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable",
	})
	if err != nil {
		t.Skipf("postgres 不可用，跳过集成测试: %v", err)
	}
	// 连接池创建成功即注册清理，避免后续 NewDBDao 失败时泄漏；Pool.Close 幂等，可与 dao.Close 共存
	t.Cleanup(func() { _ = adminPool.Close() })
	dao, err := zcdb.NewDBDao(adminPool, "postgres", nil, "", "")
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
	testPool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable dbname=zckg_test_integ",
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	t.Cleanup(func() { _ = testPool.Close() })
	dao, err = zcdb.NewDBDao(testPool, "postgres", nil, "", "")
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

	columns := bridgeColumns(cols)
	indexes, err := inspector.Indexes(context.Background(), "all_types")
	if err != nil {
		t.Fatalf("读取表索引失败: %v", err)
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
		Indexes:          bridgeIndexes(indexes),
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
		jsonVal := formatJSONTag(colName, NameCaseLowerCamel)
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

	// ddl 片段必须取方言元数据原样（类型即锚点），且 NULL 标记与元数据一致：
	// 桥接显式传入 Nullable，故真实库路径恒渲染 NULL / NOT NULL（nil 未知只出现在手工构造场景）。
	for _, c := range cols {
		marker := "NULL"
		if !c.Nullable {
			marker = "NOT NULL"
		}
		// 标记之后可能是片段结束、DEFAULT 段或 EXTRA 白名单项，故只断言到标记之后的分隔处
		re := regexp.MustCompile(`ddl:"` + regexp.QuoteMeta(c.Type) + " " + marker + `[ "]`)
		if !re.MatchString(got) {
			t.Errorf("列 %s 的 ddl 片段应含类型 %q 与标记 %s", c.Name, c.Type, marker)
		}
	}
}

// fieldNameOf 将列名 c_tinyint 转换为生成的字段名 CTinyint。
// 直接委托被测的 toPascalCase，避免 helper 自行实现一套命名规则后与实现分叉
// （如 id → ID 的特例、连字符/驼峰混合输入）。
func fieldNameOf(colName string) string {
	return toPascalCase(colName)
}

// ==================== §3.4 样板：真实元数据一键生成 ====================

// sampleUserOrderDDL 是端到端验收用的样板表（MySQL 方言）：覆盖自增主键、双唯一索引、
// 联合索引、decimal 精度、enum 值域、默认值（含表达式默认值）与可空列。
// 与该表对应的期望产物见 TestGenerate_SampleWithMetadataGolden（期望字段由
// sampleFieldExpectations 统一提供，两处用例共用）。
const sampleUserOrderDDL = "CREATE TABLE `user_order` (\n" +
	"  `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT       COMMENT '主键',\n" +
	"  `order_no`   VARCHAR(32)     NOT NULL                      COMMENT '订单号',\n" +
	"  `user_id`    BIGINT          NOT NULL                      COMMENT '用户ID',\n" +
	"  `email`      VARCHAR(64)     NOT NULL                      COMMENT '用户邮箱',\n" +
	"  `amount`     DECIMAL(10,2)   NOT NULL DEFAULT 0.00         COMMENT '订单金额（保留两位小数）',\n" +
	"  `status`     ENUM('pending','paid','refunded') NOT NULL DEFAULT 'pending' COMMENT '订单状态',\n" +
	"  `remark`     VARCHAR(255)    NULL                          COMMENT '备注（可为空）',\n" +
	"  `created_at` DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  UNIQUE KEY `uk_order_no` (`order_no`),\n" +
	"  UNIQUE KEY `uk_email` (`email`),\n" +
	"  KEY `idx_user_status` (`user_id`, `status`)\n" +
	") ENGINE=InnoDB COMMENT = '订单表'"

// bridgeColumns 把 zcdb 的列元数据桥接为 zcmodel 的 Column 列表（文档「配合 zcdb Schema」示例的落地形态）：
// Nullable 取副本地址（Column.Nullable 为 *bool，nil 表示未知，而 SchemaInspector 给出的可空性恒已知），
// PrimaryKey 与 Extra 原样透传。
func bridgeColumns(cols []zcdb.ColumnInfo) []*Column {
	columns := make([]*Column, 0, len(cols))
	for _, c := range cols {
		nullable := c.Nullable
		columns = append(columns, &Column{
			Name: c.Name, Type: c.Type, Comment: c.Comment,
			Nullable: &nullable, Default: c.Default, PrimaryKey: c.PrimaryKey, Extra: c.Extra,
		})
	}
	return columns
}

// bridgeIndexes 把 zcdb 的索引元数据桥接为 zcmodel 的 IndexInfo 列表。
func bridgeIndexes(indexes []zcdb.IndexInfo) []IndexInfo {
	bridged := make([]IndexInfo, 0, len(indexes))
	for _, idx := range indexes {
		bridged = append(bridged, IndexInfo{
			Name: idx.Name, Columns: idx.Columns, Unique: idx.Unique, Primary: idx.Primary,
		})
	}
	return bridged
}

// TestInteg_MySQL_SampleBridge 是阶段三的端到端验收：用 §3.4 样板表在真实 MySQL 上
// 一键生成（SchemaInspector → Input → Generate），逐列核对 ddl/primary_key/description tag
// 与索引块，证明「从真实库一键生成即得完整面貌样板」。
func TestInteg_MySQL_SampleBridge(t *testing.T) {
	dao := openMySQLDAO(t)
	mustExec(t, dao, "DROP TABLE IF EXISTS `user_order`")
	mustExec(t, dao, sampleUserOrderDDL)
	t.Cleanup(func() { mustExec(t, dao, "DROP TABLE IF EXISTS `user_order`") })

	inspector, err := dao.Schema()
	if err != nil {
		t.Fatalf("Schema() 失败: %v", err)
	}
	ctx := context.Background()
	cols, err := inspector.Columns(ctx, "user_order")
	if err != nil {
		t.Fatalf("Columns() 失败: %v", err)
	}
	indexes, err := inspector.Indexes(ctx, "user_order")
	if err != nil {
		t.Fatalf("Indexes() 失败: %v", err)
	}

	dir := filepath.Join(t.TempDir(), "model")
	if err := Generate(Input{
		OutputDir:        dir,
		Database:         "zckg_test_integ",
		Dialect:          DialectMysql,
		TableName:        "user_order",
		TableComment:     "订单表",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns:          bridgeColumns(cols),
		Indexes:          bridgeIndexes(indexes),
	}); err != nil {
		t.Fatalf("Generate() 失败: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_order.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	// 索引块（Entity 与 DO 各一次，顺序固定）
	assertContainsAll(t, got, []string{
		"package model\n",
		"// 索引:",
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_email (email)",
		"//   - UNIQUE KEY uk_order_no (order_no)",
		"//   - KEY idx_user_status (user_id, status)",
	}, "§3.4 样板产物（头部与索引块）")
	if n := strings.Count(got, "//   - PRIMARY KEY (id)"); n != 2 {
		t.Errorf("索引块应在 Entity 与 DO 各出现一次，实际 %d 次:\n%s", n, got)
	}

	// 逐列核对完整字段行：tag 顺序 json → db → primary_key → ddl → description，
	// 类型与 ddl 片段取值必须与真实元数据逐字一致（期望事实源与单元 golden 用例共用
	// sampleFieldExpectations；字面量经 QuoteMeta：含括号与单引号）
	for _, tc := range sampleFieldExpectations() {
		entityRe := regexp.MustCompile(regexp.QuoteMeta(tc.Field) + `\s+` + regexp.QuoteMeta(tc.Type) + `\s+` + regexp.QuoteMeta(tc.Tag))
		if !entityRe.MatchString(got) {
			t.Errorf("Entity 字段 %s（%s）未匹配 §3.4 期望 tag: %s", tc.Field, tc.Type, tc.Tag)
		}
		// DO 侧类型为 any，tag 与 Entity 一致
		doRe := regexp.MustCompile(regexp.QuoteMeta(tc.Field) + `\s+any\s+` + regexp.QuoteMeta(tc.Tag))
		if !doRe.MatchString(got) {
			t.Errorf("DO 字段 %s 未匹配 §3.4 期望 tag: %s", tc.Field, tc.Tag)
		}
	}

	// 产物与 gofmt 输出逐字节一致（索引块的三空格清单形态与 gofmt 规范一致）
	formatted, err := format.Source(content)
	if err != nil {
		t.Fatalf("生成产物无法通过 gofmt: %v", err)
	}
	if string(formatted) != got {
		t.Errorf("生成产物与 gofmt 输出不一致\ngot:\n%s\nwant:\n%s", got, formatted)
	}
}

// TestInteg_SQLite_IndexChangeRegeneration 验证索引变化后再次生成时索引块的正确性
// （「增量再生成 × 索引块」这一核心组合）：索引块位于 Entity/DO 的 doc 注释内，
// 随结构体整体重建，因此必须「有则更新、无则消失」，不得残留旧索引行。
//
// 逐步覆盖：无索引 → 新增主键与唯一索引 → 唯一索引改为联合普通索引 → 撤销二级索引 →
// 撤销主键 → 更换主键列（主键行与 primary_key tag 同步迁移）。
//
// 方言适配说明：SQLite 的 ALTER TABLE 不支持增删主键与唯一约束，故主键相关步骤改为
// 重建表后再生成——被测行为是「依据最新元数据重建索引块」，与索引经由何种 DDL 变更无关；
// SQLite 用内嵌内存库，无需外部环境，本用例在无数据库环境下同样实跑（非 Skip）。
//
// 组织形式说明：各步**依赖前一步的库状态**（如第 3 步 DROP 第 2 步创建的索引），
// 子用例无法单独运行，故按 AGENTS.md「长流程回归测试保留独立函数」的要求写成线性流程，
// 而非 t.Run 大表驱动；每处断言的说明中带步骤名以便定位失败阶段。
func TestInteg_SQLite_IndexChangeRegeneration(t *testing.T) {
	dao := openSQLiteDAO(t)
	ctx := context.Background()
	inspector, err := dao.Schema()
	if err != nil {
		t.Fatalf("Schema() 失败: %v", err)
	}
	const table = "index_change_probe"
	dir := filepath.Join(t.TempDir(), "model")
	t.Cleanup(func() { mustExec(t, dao, "DROP TABLE IF EXISTS "+table) })

	steps := []struct {
		name    string
		setup   []string // 本步的 DDL（在生成之前执行）
		want    []string // 产物中必须出现的片段
		notWant []string // 产物中必须不出现的片段（旧索引残留）
	}{
		{
			name: "无索引表不生成索引块",
			setup: []string{
				"DROP TABLE IF EXISTS " + table,
				"CREATE TABLE " + table + " (id INTEGER NOT NULL, name TEXT NOT NULL)",
			},
			notWant: []string{"索引:", "primary_key:", "PRIMARY KEY ("},
		},
		{
			name: "新增主键与唯一索引后索引块出现",
			setup: []string{
				"DROP TABLE IF EXISTS " + table,
				"CREATE TABLE " + table + " (id INTEGER NOT NULL PRIMARY KEY, name TEXT NOT NULL)",
				"CREATE UNIQUE INDEX uk_name ON " + table + " (name)",
			},
			want: []string{
				"// 索引:",
				"//   - PRIMARY KEY (id)",
				"//   - UNIQUE KEY uk_name (name)",
				`json:"id" db:"id" primary_key:"true"`,
			},
		},
		{
			name: "唯一索引改为联合普通索引后旧块被替换",
			setup: []string{
				"DROP INDEX uk_name",
				"CREATE INDEX idx_name_id ON " + table + " (name, id)",
			},
			want: []string{
				"//   - PRIMARY KEY (id)",
				"//   - KEY idx_name_id (name, id)",
			},
			notWant: []string{"uk_name", "//   - UNIQUE KEY "},
		},
		{
			name:  "撤销二级索引后只剩主键行",
			setup: []string{"DROP INDEX idx_name_id"},
			want: []string{
				"// 索引:",
				"//   - PRIMARY KEY (id)",
			},
			notWant: []string{"idx_name_id", "//   - KEY ", "//   - UNIQUE KEY "},
		},
		{
			name: "撤销主键后索引块整体消失",
			setup: []string{
				"DROP TABLE IF EXISTS " + table,
				"CREATE TABLE " + table + " (id INTEGER NOT NULL, name TEXT NOT NULL)",
			},
			notWant: []string{"索引:", "PRIMARY KEY (", "primary_key:"},
		},
		{
			name: "更换主键列后主键行与 primary_key tag 同步迁移",
			setup: []string{
				"DROP TABLE IF EXISTS " + table,
				"CREATE TABLE " + table + " (id INTEGER NOT NULL, name TEXT NOT NULL PRIMARY KEY)",
			},
			want: []string{
				"//   - PRIMARY KEY (name)",
				`json:"name" db:"name" primary_key:"true"`,
			},
			notWant: []string{"PRIMARY KEY (id)", `json:"id" db:"id" primary_key`},
		},
	}
	for _, step := range steps {
		// 每步：改库 → 重新读取元数据 → 生成到同一文件 → 断言产物
		for _, sql := range step.setup {
			mustExec(t, dao, sql)
		}
		cols, err := inspector.Columns(ctx, table)
		if err != nil {
			t.Fatalf("步骤 %q: Columns() 失败: %v", step.name, err)
		}
		indexes, err := inspector.Indexes(ctx, table)
		if err != nil {
			t.Fatalf("步骤 %q: Indexes() 失败: %v", step.name, err)
		}
		if err := Generate(Input{
			OutputDir:        dir,
			Database:         "sqlite",
			Dialect:          DialectSqlite,
			TableName:        table,
			TableComment:     "索引变更探针表",
			ColumnTagName:    "db",
			JsonTagValueCase: NameCaseLowerCamel,
			Columns:          bridgeColumns(cols),
			Indexes:          bridgeIndexes(indexes),
		}); err != nil {
			t.Fatalf("步骤 %q: Generate() 失败: %v", step.name, err)
		}
		content, err := os.ReadFile(filepath.Join(dir, table+".go"))
		if err != nil {
			t.Fatalf("步骤 %q: 读取生成文件失败: %v", step.name, err)
		}
		got := string(content)

		for _, want := range step.want {
			assertContains(t, got, want, "步骤 "+step.name+"：应包含")
		}
		for _, notWant := range step.notWant {
			assertNotContains(t, got, notWant, "步骤 "+step.name+"：不应残留")
		}
		// 索引块只应出现在 Entity 与 DO 两处；无索引时完全不出现
		if n := strings.Count(got, "// 索引:"); n != 0 && n != 2 {
			t.Errorf("步骤 %q: 索引块应出现 0 或 2 次（Entity/DO），实际 %d 次:\n%s", step.name, n, got)
		}
		// 产物必须可解析且与 gofmt 输出逐字节一致（索引块清单形态遵循 gofmt 规范）
		if _, err := parser.ParseFile(token.NewFileSet(), table+".go", got, parser.AllErrors); err != nil {
			t.Errorf("步骤 %q: 产物存在语法错误: %v", step.name, err)
		}
		formatted, err := format.Source(content)
		if err != nil {
			t.Fatalf("步骤 %q: 产物无法通过 gofmt: %v", step.name, err)
		}
		if string(formatted) != got {
			t.Errorf("步骤 %q: 产物与 gofmt 输出不一致\ngot:\n%s\nwant:\n%s", step.name, got, formatted)
		}
	}
}
