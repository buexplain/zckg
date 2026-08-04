package zcmodel

import "strings"

func formatStructFieldType(dialect Dialect, colType string) string {
	key := normalizeColumnType(dialect, colType)
	columnTypeToGoType := makeColumnTypeToGoTypeMap(dialect)
	return columnTypeToGoType[key]
}

// makeColumnTypeToGoTypeMap 根据不同的数据库类型，生成该数据库类型支持的存储类型与 Go 类型的映射关系。
// 返回的 map 键为不带长度/精度后缀的类型名（如 "varchar"、"decimal"），
// 匹配带长度后缀的列类型（如 "varchar(255)"）时需先去除后缀，未匹配到的类型默认映射为 string。
// 方言不支持或未知时返回 nil。
func makeColumnTypeToGoTypeMap(dialect Dialect) map[string]string {
	switch dialect {
	case DialectMysql:
		return map[string]string{
			"tinyint":    "int",
			"smallint":   "int",
			"mediumint":  "int",
			"int":        "int",
			"integer":    "int",
			"bigint":     "int64",
			"year":       "int",
			"float":      "float64",
			"double":     "float64",
			"decimal":    "float64",
			"numeric":    "float64",
			"bool":       "bool",
			"boolean":    "bool",
			"char":       "string",
			"varchar":    "string",
			"tinytext":   "string",
			"text":       "string",
			"mediumtext": "string",
			"longtext":   "string",
			"enum":       "string",
			"set":        "string",
			"json":       "string",
			"date":       "time.Time",
			"time":       "time.Time",
			"datetime":   "time.Time",
			"timestamp":  "time.Time",
			"binary":     "[]byte",
			"varbinary":  "[]byte",
			"tinyblob":   "[]byte",
			"blob":       "[]byte",
			"mediumblob": "[]byte",
			"longblob":   "[]byte",
		}
	case DialectPostgres:
		return map[string]string{
			"smallint":          "int",
			"int2":              "int",
			"integer":           "int",
			"int":               "int",
			"int4":              "int",
			"bigint":            "int64",
			"int8":              "int64",
			"smallserial":       "int",
			"serial":            "int",
			"bigserial":         "int64",
			"real":              "float64",
			"float4":            "float64",
			"double precision":  "float64",
			"float8":            "float64",
			"numeric":           "float64",
			"decimal":           "float64",
			"money":             "string",
			"boolean":           "bool",
			"bool":              "bool",
			"character varying": "string",
			"varchar":           "string",
			"character":         "string",
			"char":              "string",
			"bpchar":            "string",
			"text":              "string",
			"uuid":              "string",
			"json":              "string",
			"jsonb":             "string",
			"inet":              "string",
			"interval":          "string",
			"date":              "time.Time",
			"time":              "time.Time",
			"timetz":            "time.Time",
			"timestamp":         "time.Time",
			"timestamptz":       "time.Time",
			"bytea":             "[]byte",
		}
	case DialectSqlite:
		return map[string]string{
			"tinyint":           "int",
			"smallint":          "int",
			"mediumint":         "int",
			"int":               "int",
			"integer":           "int",
			"bigint":            "int64",
			"int2":              "int",
			"int8":              "int64",
			"unsigned big int":  "int64",
			"real":              "float64",
			"double":            "float64",
			"double precision":  "float64",
			"float":             "float64",
			"numeric":           "float64",
			"decimal":           "float64",
			"boolean":           "bool",
			"bool":              "bool",
			"character":         "string",
			"varchar":           "string",
			"varying character": "string",
			"nchar":             "string",
			"native character":  "string",
			"nvarchar":          "string",
			"text":              "string",
			"clob":              "string",
			"blob":              "[]byte",
			"date":              "time.Time",
			"datetime":          "time.Time",
			"timestamp":         "time.Time",
			"json":              "string",
		}
	default:
		return nil
	}
}

// normalizeColumnType 将数据库返回的列类型（如 "VARCHAR(255)"、"bigint(20)"、"text"）转换为
// makeColumnTypeToGoTypeMap 返回的 map 中可以匹配的键，即小写的、不含长度/精度后缀的类型名。
// dialect 用于处理各数据库方言特有的类型修饰：
//   - MySQL：去除 unsigned、zerofill 等修饰符（如 "bigint(20) unsigned" → "bigint"）；
//   - PostgreSQL：规范化时区后缀（如 "timestamp with time zone" → "timestamptz"、
//     "timestamp without time zone" → "timestamp"）；
//   - SQLite：去除类型名上的引号。
func normalizeColumnType(dialect Dialect, colType string) string {
	t := strings.ToLower(strings.TrimSpace(colType))
	// 移除括号及其内容（如 "(255)"、"(10,2)"、"(6)" 及枚举值列表 "('a','b')"），
	// 保留括号后的内容（如 "timestamp(6) with time zone" 需保留时区后缀）
	var b strings.Builder
	depth := 0
	for _, r := range t {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	t = b.String()
	// 压缩连续空格（SQLite/PostgreSQL 的多词类型名，如 "unsigned big int"、"double precision"）
	t = strings.Join(strings.Fields(t), " ")
	switch dialect {
	case DialectMysql:
		// 去除 MySQL 的无符号/补零修饰符
		t = strings.ReplaceAll(t, " unsigned", "")
		t = strings.ReplaceAll(t, " zerofill", "")
	case DialectPostgres:
		// 时区后缀规范化：timestamp with time zone → timestamptz、time without time zone → time
		t = strings.ReplaceAll(t, " with time zone", "tz")
		t = strings.ReplaceAll(t, " without time zone", "")
	case DialectSqlite:
		// SQLite 允许类型名带引号声明，去除引号
		t = strings.ReplaceAll(t, "\"", "")
		t = strings.ReplaceAll(t, "'", "")
	}
	return t
}
