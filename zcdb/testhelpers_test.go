// 本文件为三方言共享的测试基础设施：方言名与扫描器语法映射、驱动返回值归一、
// 占位符模板渲染，以及"引擎参数个数 oracle"相关断言（与具体用例无关，供多个测试文件复用）。

package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var (
	reMySQLExpectedCount = regexp.MustCompile(`expected (\d+) arguments`)
	rePgRequiredCount    = regexp.MustCompile(`requires (\d+)`)
	reSQLiteMissingArg   = regexp.MustCompile(`missing argument with index`)
)

// dialectNameOf 返回 DAO 的方言名，供跨方言共享用例按方言取语料与期望值。
func dialectNameOf(t *testing.T, dao *DBDao) string {
	t.Helper()
	switch dao.grammar.(type) {
	case *MySQLGrammar:
		return "MySQL"
	case *PostgresGrammar:
		return "PostgreSQL"
	case *SQLiteGrammar:
		return "SQLite"
	}
	t.Fatalf("未知的 Grammar 类型：%T", dao.grammar)
	return ""
}

// scanSyntaxOf 返回方言对应的扫描器语法配置（与各 Grammar 实际使用的配置一致）。
func scanSyntaxOf(t *testing.T, dialect string) sqlScanSyntax {
	t.Helper()
	switch dialect {
	case "MySQL":
		return mysqlScanSyntax
	case "PostgreSQL":
		return pgScanSyntax
	case "SQLite":
		return sqliteScanSyntax
	}
	t.Fatalf("未知方言：%q", dialect)
	return sqlScanSyntax{}
}

// renderRawPlaceholderTemplate 把 {N} 按方言渲染为占位符：MySQL/SQLite 为 ?，PostgreSQL 为 $N。
func renderRawPlaceholderTemplate(tmpl, dialect string) string {
	for n := 1; strings.Contains(tmpl, fmt.Sprintf("{%d}", n)); n++ {
		placeholder := "?"
		if dialect == "PostgreSQL" {
			placeholder = fmt.Sprintf("$%d", n)
		}
		tmpl = strings.ReplaceAll(tmpl, fmt.Sprintf("{%d}", n), placeholder)
	}
	return tmpl
}

// scanRawRow 读取首行并返回归一后的逐列文本；无行时返回 false 并终止用例。
func scanRawRow(t *testing.T, rows *sql.Rows) ([]string, bool) {
	t.Helper()
	if !rows.Next() {
		t.Fatalf("未读到结果行: %v", rows.Err())
	}
	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("取列名失败: %v", err)
	}
	values := make([]any, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := rows.Scan(targets...); err != nil {
		t.Fatalf("scan 失败: %v", err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows 迭代错误: %v", err)
	}
	texts := make([]string, len(values))
	for i, value := range values {
		texts[i] = driverValueText(t, value)
	}
	return texts, true
}

// driverValueText 把驱动返回值归一为可比文本：NULL → "NULL"、[]byte/string → 原字符串
// （字节按原始字节比对）、int64 → 十进制、float64 → 最短表示、bool → "true"/"false"。
// 其它类型视为未覆盖并直接失败，避免出现"悄悄比较不到"的假绿。
func driverValueText(t *testing.T, value any) string {
	t.Helper()
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	}
	t.Fatalf("未预期的驱动返回类型 %T，请补充归一规则", value)
	return ""
}

// containsNumberedPlaceholder 报告文本中是否含 $ 紧跟数字的编号占位符。
func containsNumberedPlaceholder(sqlStr string) bool {
	for i := 0; i+1 < len(sqlStr); i++ {
		if sqlStr[i] == '$' && sqlStr[i+1] >= '0' && sqlStr[i+1] <= '9' {
			return true
		}
	}
	return false
}

// enginePlaceholderCount 返回引擎解析该语句后要求的参数个数；无法判定时终止用例。
func enginePlaceholderCount(t *testing.T, dao *DBDao, dialect, sqlStr string) int {
	t.Helper()
	ctx := context.Background()
	if dialect == "SQLite" {
		// SQLite 忽略多余参数、缺参数报 "missing argument with index K"，故取最小可执行参数个数。
		for k := 0; k <= 32; k++ {
			rows, err := dao.Query(ctx, sqlStr, rawTestArgs(k)...)
			if err != nil {
				if reSQLiteMissingArg.MatchString(err.Error()) {
					continue
				}
				t.Fatalf("引擎报出非计数类错误（无法比较）: sql=%s err=%v", sqlStr, err)
			}
			if err := rows.Close(); err != nil {
				t.Errorf("close rows: %v", err)
			}
			return k
		}
		t.Fatalf("引擎计数超出上限: sql=%s", sqlStr)
		return 0
	}
	// MySQL / PostgreSQL：只传 1 个参数即可判定——通过则计数 1，否则从报错里读出要求个数。
	rows, err := dao.Query(ctx, sqlStr, 1)
	if err == nil {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
		return 1
	}
	var pattern *regexp.Regexp
	switch dialect {
	case "MySQL":
		pattern = reMySQLExpectedCount
	case "PostgreSQL":
		pattern = rePgRequiredCount
	}
	if match := pattern.FindStringSubmatch(err.Error()); match != nil {
		count, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			t.Fatalf("解析引擎计数失败: err=%v", convErr)
		}
		return count
	}
	t.Fatalf("引擎报出非计数类错误（无法比较）: sql=%s err=%v", sqlStr, err)
	return 0
}

// assertEngineRejects 断言引擎把该语句当语法错误直接拒绝（与参数个数无关）。
func assertEngineRejects(t *testing.T, dao *DBDao, dialect, sqlStr string) {
	t.Helper()
	rows, err := dao.Query(context.Background(), sqlStr, rawTestArgs(2)...)
	if err == nil {
		if closeErr := rows.Close(); closeErr != nil {
			t.Errorf("close rows: %v", closeErr)
		}
		t.Fatalf("引擎本应拒绝该语句（它不是合法占位符形态）: sql=%s", sqlStr)
	}
	if reMySQLExpectedCount.MatchString(err.Error()) || rePgRequiredCount.MatchString(err.Error()) || reSQLiteMissingArg.MatchString(err.Error()) {
		t.Fatalf("引擎的拒绝理由应是语法错误而非参数个数不符: sql=%s err=%v", sqlStr, err)
	}
}

// assertCompileRawArgCount 交叉验证 CompileRaw 的校验与引擎计数一致：
// 按引擎计数传参必须成功；多一个或少一个必须返回 ErrRawPlaceholderMismatch。
func assertCompileRawArgCount(t *testing.T, grammar Grammar, sqlStr string, engineCount int) {
	t.Helper()
	if _, err := grammar.CompileRaw(sqlStr, rawTestArgs(engineCount)); err != nil {
		t.Fatalf("按引擎计数（%d）传参应当编译成功: sql=%s err=%v", engineCount, sqlStr, err)
	}
	if engineCount > 0 {
		if _, err := grammar.CompileRaw(sqlStr, rawTestArgs(engineCount-1)); !isRawPlaceholderMismatch(err) {
			t.Errorf("少传一个参数应报占位符数量不匹配: sql=%s err=%v", sqlStr, err)
		}
	}
	if _, err := grammar.CompileRaw(sqlStr, rawTestArgs(engineCount+1)); !isRawPlaceholderMismatch(err) {
		t.Errorf("多传一个参数应报占位符数量不匹配: sql=%s err=%v", sqlStr, err)
	}
}

// rawTestArgs 生成 n 个占位参数（值不参与断言，只需个数正确）。
func rawTestArgs(n int) []any {
	args := make([]any, n)
	for i := range args {
		args[i] = i + 1
	}
	return args
}

// isRawPlaceholderMismatch 报告错误是否为占位符数量/编号不匹配（哨兵匹配，不依赖错误文案）。
func isRawPlaceholderMismatch(err error) bool {
	return errors.Is(err, ErrRawPlaceholderMismatch)
}
