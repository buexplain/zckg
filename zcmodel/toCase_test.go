package zcmodel

import (
	"reflect"
	"strings"
	"testing"
	"unicode"
)

// TestFormatJSONTag 验证 formatJSONTag 按六种命名风格转换列名，未知风格返回空串
func TestFormatJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		colName  string
		caseType NameCase
		want     string
	}{
		{"lowerCamel", "user_id", NameCaseLowerCamel, "userId"},
		{"upperCamel", "user_id", NameCaseUpperCamel, "UserId"},
		{"lowerSnake", "userId", NameCaseLowerSnake, "user_id"},
		{"upperSnake", "userId", NameCaseUpperSnake, "USER_ID"},
		{"lowerKebab", "userId", NameCaseLowerKebab, "user-id"},
		{"upperKebab", "user_id", NameCaseUpperKebab, "USER-ID"},
		{"未知风格", "user_id", NameCase("camelCase"), ""},
		{"空列名", "", NameCaseLowerCamel, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatJSONTag(tt.colName, tt.caseType); got != tt.want {
				t.Errorf("formatJSONTag(%q, %q) = %q, want %q", tt.colName, tt.caseType, got, tt.want)
			}
		})
	}
}

// TestToPascalCase 验证 ToPascalCase 对多种输入风格转换为 PascalCase，单词 id 统一转为 ID
func TestToPascalCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"下划线", "user_info", "UserInfo"},
		{"下划线含id结尾", "user_info_id", "UserInfoID"},
		{"lowerCamel输入", "userId", "UserID"},
		{"PascalCase输入", "UserID", "UserID"},
		{"UPPER_SNAKE输入", "USER_ID", "UserID"},
		{"连字符", "user-info", "UserInfo"},
		{"空格分隔", "user id", "UserID"},
		{"连续大写缩写", "HTTPServer", "HttpServer"},
		{"含数字", "user2_name", "User2Name"},
		{"单词id", "id", "ID"},
		{"单字符", "a", "A"},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToPascalCase(tt.in); got != tt.want {
				t.Errorf("ToPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLowerFirst 验证 lowerFirst 按 rune 语义小写首字符（ZCM-02）：
// ASCII 首字符正常小写；多字节字符（如中文）首字符完整保留，不被按字节截断产生残缺字节。
func TestLowerFirst(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"DO后缀", "UserInfoDO", "userInfoDO"},
		{"Entity后缀", "UserInfoEntity", "userInfoEntity"},
		{"全大写缩写", "ID", "iD"},
		{"单字符", "A", "a"},
		{"空串", "", ""},
		// 中文首字符按 rune 完整保留，不产生残缺字节导致非法标识符
		{"中文前缀DO", "订单DO", "订单DO"},
		{"中文前缀Entity", "订单Entity", "订单Entity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerFirst(tt.in); got != tt.want {
				t.Errorf("lowerFirst(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSplitWords 验证 SplitWords 的拆分规则：分隔符、小写到大写边界、连续大写后接小写边界
func TestSplitWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", nil},
		{"下划线", "user_id", []string{"user", "id"}},
		{"lowerCamel", "userId", []string{"user", "Id"}},
		{"PascalCase含缩写", "UserID", []string{"User", "ID"}},
		{"UPPER_SNAKE", "USER_ID", []string{"USER", "ID"}},
		{"连续大写后接小写", "HTTPServer", []string{"HTTP", "Server"}},
		{"连字符", "user-info", []string{"user", "info"}},
		{"空格分隔", "user id", []string{"user", "id"}},
		{"多段下划线", "a_b_c", []string{"a", "b", "c"}},
		{"含数字", "user2_name", []string{"user2", "name"}},
		{"单个单词", "single", []string{"single"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitWords(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SplitWords(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestToCase_各命名风格 验证六个命名风格转换函数对单词列表的输出
func TestToCase_各命名风格(t *testing.T) {
	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"ToLowerCamel", func() string { return ToLowerCamel([]string{"user", "id"}) }, "userId"},
		{"ToLowerCamel_首单词全小写", func() string { return ToLowerCamel([]string{"User", "ID"}) }, "userId"},
		{"ToLowerCamel_单单词", func() string { return ToLowerCamel([]string{"ID"}) }, "id"},
		{"ToLowerCamel_空列表", func() string { return ToLowerCamel(nil) }, ""},
		{"ToUpperCamel", func() string { return ToUpperCamel([]string{"user", "id"}) }, "UserId"},
		{"ToUpperCamel_空列表", func() string { return ToUpperCamel(nil) }, ""},
		{"ToLowerSnake", func() string { return ToLowerSnake([]string{"user", "ID"}) }, "user_id"},
		{"ToLowerSnake_空列表", func() string { return ToLowerSnake(nil) }, ""},
		{"ToUpperSnake", func() string { return ToUpperSnake([]string{"user", "id"}) }, "USER_ID"},
		{"ToUpperSnake_空列表", func() string { return ToUpperSnake(nil) }, ""},
		{"ToLowerKebab", func() string { return ToLowerKebab([]string{"user", "id"}) }, "user-id"},
		{"ToLowerKebab_空列表", func() string { return ToLowerKebab(nil) }, ""},
		{"ToUpperKebab", func() string { return ToUpperKebab([]string{"user", "id"}) }, "USER-ID"},
		{"ToUpperKebab_空列表", func() string { return ToUpperKebab(nil) }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestToCase_EmptyWordSkipped 验证各命名风格转换函数对单词列表中的空串元素按跳过处理
// （覆盖 ToPascalCase/ToLowerCamel/ToUpperCamel/ToLowerSnake/ToUpperSnake/ToLowerKebab/ToUpperKebab
// 内部的 w == "" continue 防御分支）。
func TestToCase_EmptyWordSkipped(t *testing.T) {
	emptyWord := []string{"user", "", "id"}
	leadingEmpty := []string{"", "user"}

	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"ToPascalCase_空单词", func() string { return ToPascalCase("user__id") }, "UserID"},
		{"ToLowerCamel_空单词", func() string { return ToLowerCamel(emptyWord) }, "userId"},
		{"ToLowerCamel_前导空", func() string { return ToLowerCamel(leadingEmpty) }, "User"},
		{"ToUpperCamel_空单词", func() string { return ToUpperCamel(emptyWord) }, "UserId"},
		{"ToUpperCamel_前导空", func() string { return ToUpperCamel(leadingEmpty) }, "User"},
		{"ToLowerSnake_空单词", func() string { return ToLowerSnake(emptyWord) }, "user_id"},
		{"ToUpperSnake_空单词", func() string { return ToUpperSnake(emptyWord) }, "USER_ID"},
		{"ToLowerKebab_空单词", func() string { return ToLowerKebab(emptyWord) }, "user-id"},
		{"ToUpperKebab_空单词", func() string { return ToUpperKebab(emptyWord) }, "USER-ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// fuzzSplitWordsExpectations / fuzzPascalExpectations / fuzzJSONTagExpectations 是独立于实现的
// 字面量期望，用于约束命名转换的语义，而不是用同一组转换函数互相自证。
func fuzzSplitWordsExpectations() map[string][]string {
	return map[string][]string{
		"":           nil,
		"__":         nil,
		"userName":   {"user", "Name"},
		"userId":     {"user", "Id"},
		"HTTPServer": {"HTTP", "Server"},
		"user_id":    {"user", "id"},
		"user-name":  {"user", "name"},
		"user id":    {"user", "id"},
		"USER_ID":    {"USER", "ID"},
		"2fa":        {"2fa"},
		"_2fa":       {"2fa"},
		"用户":         {"用户"},
		"aB":         {"a", "B"},
	}
}

func fuzzPascalExpectations() map[string]string {
	return map[string]string{
		"":           "",
		"user_id":    "UserID",
		"userId":     "UserID",
		"HTTPServer": "HttpServer",
		"USER_ID":    "UserID",
		"user-name":  "UserName",
		"id":         "ID",
		"ID":         "ID",
		"2fa":        "2fa",
		"用户":         "用户",
		"aB":         "AB",
	}
}

type fuzzJSONTagCase struct {
	name string
	c    NameCase
	want string
}

func fuzzJSONTagExpectations() []fuzzJSONTagCase {
	return []fuzzJSONTagCase{
		{"user_id", NameCaseLowerCamel, "userId"},
		{"user_id", NameCaseUpperCamel, "UserId"},
		{"user_id", NameCaseLowerSnake, "user_id"},
		{"user_id", NameCaseUpperSnake, "USER_ID"},
		{"user_id", NameCaseLowerKebab, "user-id"},
		{"user_id", NameCaseUpperKebab, "USER-ID"},

		{"id", NameCaseLowerCamel, "id"},
		{"id", NameCaseUpperCamel, "Id"},
		{"id", NameCaseLowerSnake, "id"},
		{"id", NameCaseUpperSnake, "ID"},
		{"id", NameCaseLowerKebab, "id"},
		{"id", NameCaseUpperKebab, "ID"},

		{"HTTPServer", NameCaseLowerCamel, "httpServer"},
		{"HTTPServer", NameCaseUpperCamel, "HttpServer"},
		{"HTTPServer", NameCaseLowerSnake, "http_server"},
		{"HTTPServer", NameCaseUpperSnake, "HTTP_SERVER"},
		{"HTTPServer", NameCaseLowerKebab, "http-server"},
		{"HTTPServer", NameCaseUpperKebab, "HTTP-SERVER"},

		{"用户", NameCaseLowerCamel, "用户"},
		{"用户", NameCaseUpperSnake, "用户"},
		{"", NameCaseLowerCamel, ""},
		{"", NameCaseUpperKebab, ""},
	}
}

type fuzzNameCase struct {
	name string
	c    NameCase
	fn   func([]string) string
}

// FuzzNameConversion 校验命名拆分的结构不变量、独立字面量期望与六种 JSON case 的一致性：
// SplitWords 不产生空词且命中期望切分；ToPascalCase 去除分隔符、首字符可大写时被大写、
// 单词 id 统一为 ID；formatJSONTag 的结果必须等于对应 ToX(SplitWords(name))，
// 且满足各命名风格的形态约束（首字母大小写、分隔符、整体大小写）；未知/空 case 返回空串。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcmodel -run '^$' -fuzz '^FuzzNameConversion$' -fuzztime=30s
func FuzzNameConversion(f *testing.F) {
	seeds := []string{
		"", "a", "A", "id", "ID", "user_id", "userId", "UserID", "USER_ID",
		"user-id", "user id", "user__id", "HTTPServer", "user2_name", "2fa",
		"中文", "_2fa", "_用户", "user.name", "USER-ID", "  spaced  ", "ֈ",
	}
	for _, seed := range seeds {
		for caseIndex := uint8(0); caseIndex < 8; caseIndex++ {
			f.Add(seed, caseIndex)
		}
	}
	splitWords := fuzzSplitWordsExpectations()
	pascal := fuzzPascalExpectations()
	jsonTags := fuzzJSONTagExpectations()

	cases := []fuzzNameCase{
		{"lowerCamel", NameCaseLowerCamel, ToLowerCamel},
		{"upperCamel", NameCaseUpperCamel, ToUpperCamel},
		{"lowerSnake", NameCaseLowerSnake, ToLowerSnake},
		{"upperSnake", NameCaseUpperSnake, ToUpperSnake},
		{"lowerKebab", NameCaseLowerKebab, ToLowerKebab},
		{"upperKebab", NameCaseUpperKebab, ToUpperKebab},
		{"unknown", NameCase("camelCase"), nil},
		{"empty", "", nil},
	}

	f.Fuzz(func(t *testing.T, name string, caseIndex uint8) {
		if len(name) > 256 {
			return
		}
		words := SplitWords(name)
		for _, word := range words {
			if word == "" {
				t.Fatalf("name=%q: SplitWords returned an empty word: %v", name, words)
			}
		}
		if want, ok := splitWords[name]; ok && !reflect.DeepEqual(words, want) {
			t.Fatalf("name=%q: SplitWords=%v, want %v", name, words, want)
		}

		gotPascal := ToPascalCase(name)
		if want, ok := pascal[name]; ok && gotPascal != want {
			t.Fatalf("name=%q: ToPascalCase=%q, want %q", name, gotPascal, want)
		}
		if len(words) == 1 && strings.EqualFold(words[0], "id") && gotPascal != "ID" {
			t.Fatalf("name=%q: ToPascalCase=%q, want ID for the word id", name, gotPascal)
		}
		if strings.ContainsAny(gotPascal, "_- ") {
			t.Fatalf("name=%q: ToPascalCase left separators: %q", name, gotPascal)
		}
		if gotPascal != "" {
			first := []rune(gotPascal)[0]
			// 某些 Unicode 小写字符没有大写映射（如部分亚美尼亚字符），
			// 此时无法“首字母大写”；只有存在大写映射却未转换才算失败。
			if unicode.IsLower(first) && unicode.ToUpper(first) != first {
				t.Fatalf("name=%q: ToPascalCase starts with lowercase rune: %q", name, gotPascal)
			}
		}

		tc := cases[int(caseIndex)%len(cases)]
		got := formatJSONTag(name, tc.c)
		want := ""
		if tc.fn != nil {
			want = tc.fn(words)
		}
		if got != want {
			t.Fatalf("name=%q case=%s: formatJSONTag=%q, want %q", name, tc.name, got, want)
		}
		for _, ref := range jsonTags {
			if ref.name == name && ref.c == tc.c && got != ref.want {
				t.Fatalf("name=%q case=%s: formatJSONTag=%q, want literal %q", name, tc.name, got, ref.want)
			}
		}
		fuzzAssertJSONTagShape(t, name, tc, got)
	})
}

// fuzzAssertJSONTagShape 校验各命名风格的形态不变量：分隔符、整体大小写与首字符大小写。
// 未知/空 case 只允许空串；首字符大小写检查同样跳过没有对应大小写映射的 Unicode 字符。
func fuzzAssertJSONTagShape(t *testing.T, name string, tc fuzzNameCase, got string) {
	t.Helper()
	switch tc.c {
	case NameCaseLowerCamel:
		assertNoSeparators(t, name, tc.name, got, "_- ")
		assertFirstRuneNot(t, name, tc.name, got, true)
	case NameCaseUpperCamel:
		assertNoSeparators(t, name, tc.name, got, "_- ")
		assertFirstRuneNot(t, name, tc.name, got, false)
	case NameCaseLowerSnake:
		assertNoSeparators(t, name, tc.name, got, "- ")
		if strings.ToLower(got) != got {
			t.Fatalf("name=%q case=%s: result is not lowercase: %q", name, tc.name, got)
		}
	case NameCaseLowerKebab:
		assertNoSeparators(t, name, tc.name, got, "_ ")
		if strings.ToLower(got) != got {
			t.Fatalf("name=%q case=%s: result is not lowercase: %q", name, tc.name, got)
		}
	case NameCaseUpperSnake:
		assertNoSeparators(t, name, tc.name, got, "- ")
		if strings.ToUpper(got) != got {
			t.Fatalf("name=%q case=%s: result is not uppercase: %q", name, tc.name, got)
		}
	case NameCaseUpperKebab:
		assertNoSeparators(t, name, tc.name, got, "_ ")
		if strings.ToUpper(got) != got {
			t.Fatalf("name=%q case=%s: result is not uppercase: %q", name, tc.name, got)
		}
	default:
		if got != "" {
			t.Fatalf("name=%q case=%s: unknown case returned %q", name, tc.name, got)
		}
	}
}

func assertNoSeparators(t *testing.T, name, caseName, got, separators string) {
	t.Helper()
	if strings.ContainsAny(got, separators) {
		t.Fatalf("name=%q case=%s: result contains separators %q: %q", name, caseName, separators, got)
	}
}

// assertFirstRuneNot 在首个 rune 存在可转换的大小写映射时断言其不是 wantLower 指定的形态。
func assertFirstRuneNot(t *testing.T, name, caseName, got string, wantLower bool) {
	t.Helper()
	if got == "" {
		return
	}
	first := []rune(got)[0]
	if wantLower {
		if unicode.IsUpper(first) && unicode.ToLower(first) != first {
			t.Fatalf("name=%q case=%s: result starts with uppercase rune: %q", name, caseName, got)
		}
		return
	}
	if unicode.IsLower(first) && unicode.ToUpper(first) != first {
		t.Fatalf("name=%q case=%s: result starts with lowercase rune: %q", name, caseName, got)
	}
}
