package zcdb

import "strings"

// sqlScanSyntax 描述方言在「字符串、引号标识符、注释、美元引用」上的语法差异，
// 供扫描器把 SQL 文本切成代码区与非代码区，使占位符识别与数据库引擎的解析一致。
//
// 已知未覆盖的语法（这些位置会被当作代码区处理）：SQLite 的 [...] 引号标识符、
// MySQL 的 /*! ... */ 可执行注释（引擎会执行其内容）、PostgreSQL 的 U&'...' / U&"..."。
type sqlScanSyntax struct {
	// plainStringBackslash 普通字符串（'...'，MySQL 下还有 "..."）内反斜杠转义下一个字符。
	// MySQL 默认 sql_mode 如此；开启 NO_BACKSLASH_ESCAPES 时不成立。
	plainStringBackslash bool
	// escapeStringPrefix 支持 E'...' 前缀字符串，其中的反斜杠是转义符（PostgreSQL）。
	escapeStringPrefix bool
	// backtickIdentifier 支持 `...` 引号标识符（MySQL / SQLite）。
	backtickIdentifier bool
	// dollarQuote 支持 $$...$$ 与 $tag$...$tag$ 美元引用（PostgreSQL）。
	dollarQuote bool
	// hashComment 支持 # 行注释（MySQL）。
	hashComment bool
	// dashCommentNeedsSpace -- 后必须紧跟空白或行尾才算注释（MySQL）；标准 SQL 无此要求。
	dashCommentNeedsSpace bool
	// nestedBlockComment 块注释可嵌套（PostgreSQL）。
	nestedBlockComment bool
}

var (
	mysqlScanSyntax = sqlScanSyntax{
		plainStringBackslash:  true,
		backtickIdentifier:    true,
		hashComment:           true,
		dashCommentNeedsSpace: true,
	}
	sqliteScanSyntax = sqlScanSyntax{
		backtickIdentifier: true,
	}
	pgScanSyntax = sqlScanSyntax{
		escapeStringPrefix: true,
		dollarQuote:        true,
		nestedBlockComment: true,
	}
)

// sqlSegment 是 SQL 文本的一段：code 为 true 表示代码区（其中的 ? 或 $N 是占位符候选），
// false 表示字符串字面量、引号标识符、注释或美元引用——内容原样保留，不识别占位符。
type sqlSegment struct {
	text string
	code bool
}

// splitSQLCode 按方言语法把 SQL 切成代码区与非代码区交替的片段；
// 拼接所有片段即原文，调用方只需对 code 片段做占位符处理。
func splitSQLCode(sqlStr string, syntax sqlScanSyntax) []sqlSegment {
	var segments []sqlSegment
	codeStart := 0
	for i := 0; i < len(sqlStr); {
		length := nonCodeLen(sqlStr, i, syntax)
		if length == 0 {
			i++
			continue
		}
		if i > codeStart {
			segments = append(segments, sqlSegment{text: sqlStr[codeStart:i], code: true})
		}
		segments = append(segments, sqlSegment{text: sqlStr[i : i+length], code: false})
		i += length
		codeStart = i
	}
	if codeStart < len(sqlStr) {
		segments = append(segments, sqlSegment{text: sqlStr[codeStart:], code: true})
	}
	return segments
}

// nonCodeLen 返回 sqlStr[at] 处开始的非代码区长度；该位置不属于非代码区时返回 0。
func nonCodeLen(sqlStr string, at int, syntax sqlScanSyntax) int {
	switch sqlStr[at] {
	case '\'':
		return stringLiteralLen(sqlStr, at, '\'', syntax.plainStringBackslash ||
			syntax.escapeStringPrefix && hasEscapeStringPrefix(sqlStr, at))
	case '"':
		// MySQL 默认模式下 " 是字符串（同样按反斜杠转义）；PostgreSQL/SQLite 是引号标识符。
		return stringLiteralLen(sqlStr, at, '"', syntax.plainStringBackslash)
	case '`':
		if syntax.backtickIdentifier {
			return stringLiteralLen(sqlStr, at, '`', false)
		}
	case '-':
		if strings.HasPrefix(sqlStr[at:], "--") &&
			(!syntax.dashCommentNeedsSpace || at+2 == len(sqlStr) || isSpaceByte(sqlStr[at+2])) {
			return toLineEnd(sqlStr, at)
		}
	case '#':
		if syntax.hashComment {
			return toLineEnd(sqlStr, at)
		}
	case '/':
		if strings.HasPrefix(sqlStr[at:], "/*") {
			return blockCommentLen(sqlStr, at, syntax.nestedBlockComment)
		}
	case '$':
		if syntax.dollarQuote {
			return dollarQuoteLen(sqlStr, at)
		}
	}
	return 0
}

// stringLiteralLen 返回 at 处引号开始的字符串字面量或引号标识符长度：
// 同种引号双写为字面引号；escapeBackslash 为 true 时反斜杠转义下一个字符。
// 未闭合时吞到文本结尾——引擎在此处报语法错误，这里只在文本层面收尾。
func stringLiteralLen(sqlStr string, at int, quote byte, escapeBackslash bool) int {
	for i := at + 1; i < len(sqlStr); i++ {
		switch sqlStr[i] {
		case '\\':
			if escapeBackslash {
				i++
			}
		case quote:
			if i+1 < len(sqlStr) && sqlStr[i+1] == quote {
				i++
				continue
			}
			return i + 1 - at
		}
	}
	return len(sqlStr) - at
}

// hasEscapeStringPrefix 报告 at 处的引号是否是 E'...'：E 必须紧贴引号且自身不在标识符中。
func hasEscapeStringPrefix(sqlStr string, at int) bool {
	if at == 0 {
		return false
	}
	if c := sqlStr[at-1]; c != 'e' && c != 'E' {
		return false
	}
	return at < 2 || !isIdentPartByte(sqlStr[at-2])
}

// blockCommentLen 返回 /* ... */ 块注释长度；nested 为 true 时按 PostgreSQL 规则支持嵌套。
func blockCommentLen(sqlStr string, at int, nested bool) int {
	depth := 1
	for i := at + 2; i < len(sqlStr); i++ {
		switch {
		case nested && strings.HasPrefix(sqlStr[i:], "/*"):
			depth++
			i++
		case strings.HasPrefix(sqlStr[i:], "*/"):
			depth--
			if depth == 0 {
				return i + 2 - at
			}
			i++
		}
	}
	return len(sqlStr) - at
}

// toLineEnd 返回 at 处到本行结尾（不含换行符）的长度。
func toLineEnd(sqlStr string, at int) int {
	if idx := strings.IndexByte(sqlStr[at:], '\n'); idx >= 0 {
		return idx
	}
	return len(sqlStr) - at
}

// dollarQuoteLen 返回 $$...$$ 或 $tag$...$tag$ 美元引用长度；不是美元引用时返回 0。
// 标签与未引用标识符同规则，且不能含 $，因此 $1 这类占位符不会被误认成引用开始。
func dollarQuoteLen(sqlStr string, at int) int {
	end := at + 1
	if end < len(sqlStr) && sqlStr[end] == '$' {
		end++
	} else {
		if end >= len(sqlStr) || !isIdentStartByte(sqlStr[end]) {
			return 0
		}
		for end < len(sqlStr) && isIdentPartByte(sqlStr[end]) {
			end++
		}
		if end >= len(sqlStr) || sqlStr[end] != '$' {
			return 0
		}
		end++
	}
	delim := sqlStr[at:end]
	if idx := strings.Index(sqlStr[end:], delim); idx >= 0 {
		return end + idx + len(delim) - at
	}
	return len(sqlStr) - at
}

// isSpaceByte 报告字节是否为空白（MySQL 的 -- 注释判定用）。
func isSpaceByte(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\v', '\f':
		return true
	}
	return false
}

// isIdentStartByte 报告字节是否可作为标识符首字符（ASCII 字母或下划线）。
func isIdentStartByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isIdentPartByte 报告字节是否可作为标识符后续字符（ASCII 字母、数字或下划线）。
func isIdentPartByte(c byte) bool {
	return isIdentStartByte(c) || c >= '0' && c <= '9'
}
