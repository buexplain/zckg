package zcmodel

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// testColumns 构造测试用列信息
func testColumns() []Column {
	return []Column{
		{Name: "id", Comment: "主键", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64", JsonTagValue: "id"}},
		{Name: "user_name", Comment: "用户名", StructFieldInfo: StructFieldInfo{Name: "UserName", Type: "string", JsonTagValue: "userName"}},
		{Name: "created_at", StructFieldInfo: StructFieldInfo{Name: "CreatedAt", Type: "time.Time", JsonTagValue: "createdAt"}},
	}
}

// TestBuildStruct_Entity 验证 Entity 结构体生成：具体类型、tag 顺序为 json/db/description
func TestBuildStruct_Entity(t *testing.T) {
	got := buildStruct("UserInfoEntity", testColumns(), false, "UserInfoEntity 用户表", "db", nil)
	want := []string{
		"// UserInfoEntity 用户表",
		"type UserInfoEntity struct {",
		"`json:\"id\" db:\"id\" description:\"主键\"`",
		"`json:\"userName\" db:\"user_name\" description:\"用户名\"`",
		"`json:\"createdAt\" db:\"created_at\"`",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("buildStruct 输出缺少: %s\n输出:\n%s", s, got)
		}
	}
	// 字段类型按 gofmt 对齐，用正则匹配字段行
	wantRegex := []*regexp.Regexp{
		regexp.MustCompile(`ID\s+int64\s+` + "`json:\"id\" db:\"id\" description:\"主键\"`"),
		regexp.MustCompile(`UserName\s+string\s+` + "`json:\"userName\" db:\"user_name\" description:\"用户名\"`"),
		regexp.MustCompile(`CreatedAt\s+time\.Time\s+` + "`json:\"createdAt\" db:\"created_at\"`"),
	}
	for _, re := range wantRegex {
		if !re.MatchString(got) {
			t.Errorf("buildStruct 输出未匹配: %v\n输出:\n%s", re, got)
		}
	}
}

// TestBuildStruct_DO 验证 DO 结构体生成：字段类型统一为 any
func TestBuildStruct_DO(t *testing.T) {
	got := buildStruct("UserInfoDO", testColumns(), true, "", "db", nil)
	if !strings.Contains(got, "type UserInfoDO struct {") {
		t.Errorf("buildStruct 输出缺少结构体声明:\n%s", got)
	}
	if !regexp.MustCompile(`ID\s+any\s+` + "`json:\"id\" db:\"id\" description:\"主键\"`").MatchString(got) {
		t.Errorf("buildStruct useAny 输出缺少 any 类型字段:\n%s", got)
	}
	if strings.Contains(got, "int64") {
		t.Errorf("buildStruct useAny 输出不应包含具体类型 int64:\n%s", got)
	}
}

// TestBuildStruct_CustomTagName 验证自定义 tag 名称（如 "column"）
func TestBuildStruct_CustomTagName(t *testing.T) {
	got := buildStruct("UserInfoEntity", testColumns(), false, "", "column", nil)
	if !strings.Contains(got, "`json:\"id\" column:\"id\" description:\"主键\"`") {
		t.Errorf("buildStruct 自定义 tag 名称输出错误:\n%s", got)
	}
}

// TestBuildStruct_EmptyComment 验证无注释字段不生成 description tag
func TestBuildStruct_EmptyComment(t *testing.T) {
	cols := []Column{
		{Name: "id", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64"}},
	}
	got := buildStruct("Entity", cols, false, "", "db", nil)
	if strings.Contains(got, "description:") {
		t.Errorf("无注释字段不应生成 description tag:\n%s", got)
	}
	// 无 JsonTagValue 时不应生成 json tag
	if !regexp.MustCompile(`ID\s+int64\s+` + "`db:\"id\"`").MatchString(got) {
		t.Errorf("无 JsonTagValue 字段只应包含 db tag:\n%s", got)
	}
}

// TestBuildToDOMethod 验证 ToDO 方法生成：复用参数、直接赋值不取指针
func TestBuildToDOMethod(t *testing.T) {
	got := buildToDOMethod("UserInfoEntity", "UserInfoDO", testColumns())
	want := []string{
		"func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {",
		"if len(userInfoDO) > 0 && userInfoDO[0] != nil {",
		"d = &UserInfoDO{}",
		"d.ID = e.ID",
		"d.UserName = e.UserName",
		"d.CreatedAt = e.CreatedAt",
		"return d",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("buildToDOMethod 输出缺少: %s\n输出:\n%s", s, got)
		}
	}
	if strings.Contains(got, "&e.") {
		t.Errorf("buildToDOMethod 不应取地址赋值:\n%s", got)
	}
}

// TestBuildToEntityMethod 验证 ToEntity 方法生成：值类型断言还原
func TestBuildToEntityMethod(t *testing.T) {
	got := buildToEntityMethod("UserInfoEntity", "UserInfoDO", testColumns())
	want := []string{
		"func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {",
		"if len(userInfoEntity) > 0 && userInfoEntity[0] != nil {",
		"e = &UserInfoEntity{}",
		"if v, ok := d.ID.(int64); ok {",
		"e.ID = v",
		"if v, ok := d.UserName.(string); ok {",
		"if v, ok := d.CreatedAt.(time.Time); ok {",
		"return e",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("buildToEntityMethod 输出缺少: %s\n输出:\n%s", s, got)
		}
	}
}

// TestSanitizeTagValue 验证 tag 值净化：控制字符/双引号/反斜杠转义为标准转义序列，反引号替换为单引号
func TestSanitizeTagValue(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "普通文本", in: "用户名", want: "用户名"},
		{name: "含双引号", in: "列表封面，示例：[{\"url\":\"https://a.c.cc/1.jpg\"}]", want: "列表封面，示例：[{\\\"url\\\":\\\"https://a.c.cc/1.jpg\\\"}]"},
		{name: "含反引号", in: "列表`封面`", want: "列表'封面'"},
		{name: "含换行", in: "第一行\n第二行", want: "第一行\\n第二行"},
		{name: "含回车换行", in: "第一行\r\n第二行", want: "第一行\\r\\n第二行"},
		{name: "含反斜杠", in: "C:\\temp\\a", want: "C:\\\\temp\\\\a"},
		{name: "含空格", in: "list cover", want: "list cover"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeTagValue(tt.in); got != tt.want {
				t.Errorf("sanitizeTagValue() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSanitizeTagValue_ReflectRestore 验证净化后的 tag 值经 reflect.StructTag.Get 完整还原原注释（含换行与双引号）
func TestSanitizeTagValue_ReflectRestore(t *testing.T) {
	comment := "列表封面，示例：[{\"url\":\"https://a.c.cc/1.jpg\"}]\n第二行说明"
	sanitized := sanitizeTagValue(comment)
	tag := reflect.StructTag(`json:"listCover" db:"list_cover" description:"` + sanitized + `"`)
	if got := tag.Get("description"); got != comment {
		t.Errorf("reflect 还原失败\ngot:  %q\nwant: %q", got, comment)
	}
}

// TestBuildStruct_CommentWithSpecialChars 验证含双引号/反引号/换行的注释生成的代码可解析且反射可完整读取
func TestBuildStruct_CommentWithSpecialChars(t *testing.T) {
	comment := "列表封面，示例：[{\"url\":\"https://a.c.cc/1.jpg\"}]\n第二行"
	cols := []Column{
		{Name: "list_cover", Comment: comment, StructFieldInfo: StructFieldInfo{Name: "ListCover", Type: "string", JsonTagValue: "listCover"}},
	}
	got := buildStruct("TEntity", cols, false, "", "db", nil)
	// 语法可解析（反引号字符串不会被提前终止、tag 为单行）
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", "package model\n"+got, parser.AllErrors); err != nil {
		t.Fatalf("生成代码存在语法错误: %v\n%s", err, got)
	}
	// 换行/双引号已转义为转义序列，不会破坏 tag 解析
	if !regexp.MustCompile(`description:"列表封面，示例：\[\{\\"url\\":\\"https://a\.c\.cc/1\.jpg\\"\}\]\\n第二行"`).MatchString(got) {
		t.Errorf("description tag 未正确转义:\n%s", got)
	}
	// 提取 tag 经 reflect 验证可完整还原
	m := regexp.MustCompile(`db:"list_cover" description:"(.*)"`).FindStringSubmatch(got)
	if len(m) != 2 {
		t.Fatalf("无法提取 description tag:\n%s", got)
	}
	if gotDesc := reflect.StructTag(`description:"` + m[1] + `"`).Get("description"); gotDesc != comment {
		t.Errorf("reflect 还原失败\ngot:  %q\nwant: %q", gotDesc, comment)
	}
}

// fieldTagOf 从解析后的文件中提取指定结构体字段的 tag 内容（去除外层反引号），找不到返回空串。
func fieldTagOf(file *ast.File, fieldName string) string {
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok || st.Fields == nil {
				continue
			}
			for _, f := range st.Fields.List {
				for _, name := range f.Names {
					if name.Name == fieldName && f.Tag != nil {
						// f.Tag.Value 为含反引号的 raw string 字面量，reflect.StructTag 需要无引号的内容
						return strings.Trim(f.Tag.Value, "`")
					}
				}
			}
		}
	}
	return ""
}

// TestBuildStruct_JsonTagValueWithSpecialChars 验证含双引号/换行/反斜杠的 JsonTagValue
// 生成的代码可解析且 reflect.StructTag 可完整还原（与 Comment 的净化行为对齐）。
func TestBuildStruct_JsonTagValueWithSpecialChars(t *testing.T) {
	jsonTag := "列表[\"a\"]\n第二行\\x"
	cols := []Column{
		{Name: "list_cover", StructFieldInfo: StructFieldInfo{Name: "ListCover", Type: "string", JsonTagValue: jsonTag}},
	}
	got := buildStruct("TEntity", cols, false, "", "db", nil)
	// 语法可解析（双引号/换行/反斜杠均被转义，反引号字符串不会被提前终止）
	file, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors)
	if err != nil {
		t.Fatalf("生成代码存在语法错误: %v\n%s", err, got)
	}
	tag := fieldTagOf(file, "ListCover")
	if tag == "" {
		t.Fatalf("未提取到字段 tag:\n%s", got)
	}
	if gotJSON := reflect.StructTag(tag).Get("json"); gotJSON != jsonTag {
		t.Errorf("json tag 还原失败\ngot:  %q\nwant: %q", gotJSON, jsonTag)
	}
}

// TestBuildStruct_JsonTagValueWithBacktick 验证含反引号的 JsonTagValue 被替换为单引号，代码可解析。
func TestBuildStruct_JsonTagValueWithBacktick(t *testing.T) {
	cols := []Column{
		{Name: "list_cover", StructFieldInfo: StructFieldInfo{Name: "ListCover", Type: "string", JsonTagValue: "列表`封面`"}},
	}
	got := buildStruct("TEntity", cols, false, "", "db", nil)
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors); err != nil {
		t.Fatalf("生成代码存在语法错误: %v\n%s", err, got)
	}
	if !strings.Contains(got, "json:\"列表'封面'\"") {
		t.Errorf("反引号应替换为单引号:\n%s", got)
	}
}

// TestBuildStruct_ColumnTagValueWithSpecialChars 验证列映射 tag 的值（列名）与 json/ddl 一样经 sanitizeTagValue 转义：
// PG/SQLite 引号标识符可含双引号、反斜杠、换行，未转义时 Lookup 会静默读出截断值或失败并连带吞掉后续 tag；
// 转义后从 AST 提取的 tag 经 reflect.StructTag.Lookup 完整还原列名，且后续 ddl tag 不受影响。
// 反引号列名按 sanitizeTagValue 的既有规则替换为单引号（保证语法合法）。
func TestBuildStruct_ColumnTagValueWithSpecialChars(t *testing.T) {
	cases := []struct{ name, col, want string }{
		{"双引号", `a"b`, `a"b`},
		{"反斜杠", `a\b`, `a\b`},
		{"换行", "a\nb", "a\nb"},
		{"反引号替换为单引号", "a`b", "a'b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cols := []Column{{Name: tc.col, Type: "text", StructFieldInfo: StructFieldInfo{Name: "F", Type: "string"}}}
			got := buildStruct("TEntity", cols, false, "", "db", nil)
			file, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors)
			if err != nil {
				t.Fatalf("生成代码存在语法错误: %v\n%s", err, got)
			}
			tag := reflect.StructTag(fieldTagOf(file, "F"))
			if v, ok := tag.Lookup("db"); !ok || v != tc.want {
				t.Errorf("db tag 还原失败: Lookup = (%q, %v), want %q\n%s", v, ok, tc.want, got)
			}
			if v, ok := tag.Lookup("ddl"); !ok || v != "text" {
				t.Errorf("后续 ddl tag 应完整可读: Lookup = (%q, %v)\n%s", v, ok, got)
			}
		})
	}
}

// TestWriteOrReplaceStruct_NewFile 验证文件不存在时创建新文件
func TestWriteOrReplaceStruct_NewFile(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, "", entityCode, doCode, nil)
	want := "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
	if got != want {
		t.Errorf("新建文件内容错误\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestWriteOrReplaceStruct_EmptyFile 验证空文件按新建处理
func TestWriteOrReplaceStruct_EmptyFile(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, "  \n", entityCode, doCode, nil)
	want := "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
	if got != want {
		t.Errorf("空文件重建内容错误\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestWriteOrReplaceStruct_KeepUserCode 验证重新生成时移除旧生成代码、保留用户代码（含方法体）与 import
func TestWriteOrReplaceStruct_KeepUserCode(t *testing.T) {
	// 模拟第一次生成后的文件：生成代码 + import + 用户自定义代码
	orig := `package model

import "fmt"

type UserEntity struct {
	ID   int
	Name string
}

func (e *UserEntity) ToDO() { _ = e }

type UserDO struct {
	ID   any
	Name any
}

func (d *UserDO) ToEntity() { _ = d }

// 用户自定义方法
func (e *UserEntity) Hello() string {
	return fmt.Sprintf("hi %d", e.ID)
}

// 用户自定义方法
func (d *UserDO) World() string {
	return "world"
}

var Extra = 1
`
	// 第二次生成：结构体字段变化（移除 Name，新增 Age）
	entityCode := "type UserEntity struct {\n\tID   int64\n\tAge  int\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tID   any\n\tAge  any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 用户代码与 import 保留
	assertContainsAll(t, got, []string{
		"import \"fmt\"",
		"func (e *UserEntity) Hello() string {",
		"func (d *UserDO) World() string {",
		"var Extra = 1",
	}, "重新生成后用户代码与 import 应保留")
	// 方法体也必须完整保留：验证按 AST 偏移截取用户代码时不会截断函数体
	assertContainsAll(t, got, []string{
		"return fmt.Sprintf(\"hi %d\", e.ID)",
		"return \"world\"",
	}, "重新生成后用户方法体应完整保留")
	// 新生成代码生效、旧字段被移除（产物经 gofmt 格式化，字段对齐按 gofmt 标准）
	if !regexp.MustCompile(`Age\s+int\b`).MatchString(got) {
		t.Errorf("重新生成后缺少新字段 Age:\n%s", got)
	}
	assertNotContains(t, got, "Name string", "重新生成后旧字段 Name 应被移除")
	assertNotContains(t, got, "Name any", "重新生成后旧字段 Name 应被移除")
	// 生成代码各只保留一份
	if strings.Count(got, "type UserEntity struct {") != 1 || strings.Count(got, "type UserDO struct {") != 1 {
		t.Errorf("重新生成后生成代码出现多次:\n%s", got)
	}
	// 骨架保持式：声明顺序与原文件完全一致（用户自定义方法原位保留，不再归位到生成代码之后）
	order := []string{
		"type UserEntity struct {",
		"func (e *UserEntity) ToDO()",
		"type UserDO struct {",
		"func (d *UserDO) ToEntity()",
		"func (e *UserEntity) Hello() string {",
		"func (d *UserDO) World() string {",
		"var Extra = 1",
	}
	last := -1
	for _, s := range order {
		idx := strings.Index(got, s)
		if idx < 0 {
			t.Errorf("输出缺少: %s", s)
			continue
		}
		if idx < last {
			t.Errorf("骨架顺序错误: %s 应位于 %s 之后", s, order[0])
		}
		last = idx
	}
}

// TestWriteOrReplaceStruct_ExistingFile_UserTypeBlock 验证存量文件中仅含用户类型的 type 声明块
// （不含任何生成类型）再生成时整体原样保留。
func TestWriteOrReplaceStruct_ExistingFile_UserTypeBlock(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}

// 用户自定义类型块
type (
	Status int
	Level  int
)
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertContainsAll(t, got, []string{"type (", "Status int", "Level  int", "// 用户自定义类型块"}, "用户类型块应完整保留")
	if !regexp.MustCompile(`ID\s+int64`).MatchString(got) {
		t.Errorf("新生成代码未生效:\n%s", got)
	}
}

// TestWriteGeneratedFile_SyntaxError 验证落盘前语法自校验：非法产物直接报错且不写出任何文件，
// 锁定 fail-safe 红线（用户代码保护与落盘安全的最后一道防线）。
func TestWriteGeneratedFile_SyntaxError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "broken.go")
	err := writeGeneratedFile(filePath, []byte("package model\n\nfunc broken( {\n"))
	if err == nil {
		t.Fatalf("语法非法内容应报错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "生成代码存在语法错误") {
		t.Errorf("错误信息应为语法自校验报错，实际: %v", err)
	}
	if _, statErr := os.Stat(filePath); !os.IsNotExist(statErr) {
		t.Errorf("语法非法产物不应落盘: %v", statErr)
	}
}

// TestWriteOrReplaceStruct_NewFile_NeededImports 验证生成代码需要 import 时，新建文件自动引入 import "time"
func TestWriteOrReplaceStruct_NewFile_NeededImports(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	got := writeAndVerify(t, "", entityCode, doCode, []string{"time"})
	// import "time" 自动引入，且位于 package 与生成代码之间
	assertContains(t, got, "import \"time\"", "需要 time 包时新建文件应自动引入 import")
	if !strings.HasPrefix(got, "package model\n\nimport \"time\"\n\ntype UserEntity struct {") {
		t.Errorf("import 位置错误:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_ExistingFile_AutoAddTimeImport 验证已存在文件缺少 time import 时自动补上，且保留用户 import
func TestWriteOrReplaceStruct_ExistingFile_AutoAddTimeImport(t *testing.T) {
	// 模拟用户已有文件：无 time import，含用户自定义方法与用户 import
	orig := `package model

import "fmt"

type UserEntity struct {
	ID int
}

func (e *UserEntity) Hello() string {
	return fmt.Sprint(e.ID)
}
`
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	got := writeAndVerify(t, orig, entityCode, doCode, []string{"time"})
	// 缺失的 time 合并进原 import 块（单一 import 块，避免形成两个 import 块）
	assertContains(t, got, "import (\n\t\"fmt\"\n\t\"time\"\n)", "缺失的 time import 应合并进原 import 块")
	assertContains(t, got, "func (e *UserEntity) Hello() string {", "用户自定义方法应保留")
}

// TestWriteOrReplaceStruct_ExistingFile_TimeImportExists 验证文件已导入 time 时不重复添加
func TestWriteOrReplaceStruct_ExistingFile_TimeImportExists(t *testing.T) {
	orig := `package model

import "time"

type UserEntity struct {
	ID int
}
`
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	got := writeAndVerify(t, orig, entityCode, doCode, []string{"time"})
	if strings.Count(got, `import "time"`) != 1 {
		t.Errorf("已导入 time 时不应重复添加:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_ExistingFile_AliasImport 验证存量文件以别名/空白导入所需包时（ZCM-03），
// 判重不视为已存在：补充标准导入使生成代码（引用默认包名）可编译，别名导入与用户代码完整保留。
func TestWriteOrReplaceStruct_ExistingFile_AliasImport(t *testing.T) {
	tests := []struct {
		name       string
		orig       string
		keepImport string
	}{
		{
			name: "别名导入",
			orig: `package model

import mytime "time"

type UserEntity struct {
	ID int
}

func (e *UserEntity) Now() mytime.Time {
	return mytime.Now()
}
`,
			keepImport: `mytime "time"`,
		},
		{
			name: "空白导入",
			orig: `package model

import _ "time"

type UserEntity struct {
	ID int
}
`,
			keepImport: `_ "time"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
			doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
			got := writeAndVerify(t, tt.orig, entityCode, doCode, []string{"time"})
			// 别名/空白导入不满足生成代码对默认包名的引用，必须补充标准导入（合并进同一 import 块）
			assertContains(t, got, "\t\"time\"\n", "存量文件为"+tt.name+"时不应视为已导入，应补充标准导入 \"time\"")
			// 用户原有的别名/空白导入完整保留（与标准导入合法共存于同一 import 块）
			assertContains(t, got, tt.keepImport, "用户原有导入应保留")
			// 缺失导入合并进原 import 块，不形成两个 import 块
			if strings.Count(got, "import (") != 1 {
				t.Errorf("应只有一个 import 块:\n%s", got)
			}
			assertContains(t, got, "type UserEntity struct {", "生成代码应写入")
		})
	}
}

// TestWriteOrReplaceStruct_MultipleNeededImports 验证需要多个包时生成 import (…) 块，且缺失的包逐个补齐
func TestWriteOrReplaceStruct_MultipleNeededImports(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	needed := []string{"time", "github.com/foo/bar"}

	// 新建文件：多个 import 组装为排序后的 import (…) 块
	got := writeAndVerify(t, "", entityCode, doCode, needed)
	want := "package model\n\nimport (\n\t\"github.com/foo/bar\"\n\t\"time\"\n)\n\n"
	if !strings.HasPrefix(got, want) {
		t.Errorf("多 import 块格式错误\nwant prefix:\n%s\ngot:\n%s", want, got)
	}

	// 已存在文件：原有 import 保留，缺失的包合并为块补充
	orig := `package model

import "time"

type UserEntity struct {
	ID int
}
`
	got = writeAndVerify(t, orig, entityCode, doCode, needed)
	// 缺失的包合并进原 import 块（"time" 已存在不重复，仅补 "github.com/foo/bar"；
	// 最终经 gofmt 排序，"github.com/foo/bar" 排在前）
	assertContains(t, got, "import (\n\t\"github.com/foo/bar\"\n\t\"time\"\n)", "缺失的 import 应合并进原 import 块")
	if strings.Count(got, `"time"`) != 1 {
		t.Errorf("已存在的 import 不应重复添加:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_KeepBuildTagsAndPackageComment 验证再生成保留文件头 build tags、
// package 文档注释与原 package 行（逐字节），且已有文件尊重原包名而非输出目录推导名。
func TestWriteOrReplaceStruct_KeepBuildTagsAndPackageComment(t *testing.T) {
	// 原文件包名与输出目录推导名不一致（custompkg != model），应保留原包名
	orig := `//go:build ignore
// +build ignore

// Package custompkg 包文档注释。
package custompkg

import "fmt"

type UserEntity struct {
	ID int
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}

var Extra = 1
`
	entityCode := "type UserEntity struct {\n\tID   int64\n\tAge  int\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID  any\n\tAge any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 文件头（build tags + package 注释 + 原 package 行）逐字节保留，且不追加任何生成声明
	wantHeader := "//go:build ignore\n// +build ignore\n\n// Package custompkg 包文档注释。\npackage custompkg\n"
	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("文件头 build tags/package 注释未逐字节保留\nwant prefix:\n%s\ngot:\n%s", wantHeader, got)
	}
	// 包名尊重原文件，不被输出目录推导名 model 覆盖
	assertNotContains(t, got, "\npackage model\n", "已有文件的包名不应被改为输出目录推导名")
	// 用户代码保留
	assertContains(t, got, "var Extra = 1", "用户代码应保留")
	// 生成代码各只保留一份
	if strings.Count(got, "type UserEntity struct {") != 1 || strings.Count(got, "type UserDO struct {") != 1 {
		t.Errorf("重新生成后生成代码出现多次:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_KeepUserTypeInMixedBlock 验证 type 块中混有用户类型时，
// 再生成按 Spec 粒度就地替换生成的结构体（块形态保留），用户类型及其注释完整保留。
func TestWriteOrReplaceStruct_KeepUserTypeInMixedBlock(t *testing.T) {
	orig := `package model

type (
	// UserEntity 用户实体
	UserEntity struct {
		ID int
	}

	// MyHelper 用户手写类型
	MyHelper struct {
		X int
	}
)

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}

// MyFunc 用户函数
func MyFunc() {}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 混合 type 块中的用户类型（含注释）与用户函数保留
	assertContainsAll(t, got, []string{
		"// MyHelper 用户手写类型",
		"MyHelper struct {",
		"func MyFunc() {",
	}, "混合 type 块中的用户代码应保留")
	// 骨架保持式：生成类型各只保留一份——块内的 Entity Spec 就地替换（块形态保留，故不带 type 关键字），
	// 块外的 DO 为独立声明
	if strings.Count(got, "UserEntity struct {") != 1 || strings.Count(got, "type UserDO struct {") != 1 {
		t.Errorf("生成类型应各只保留一份:\n%s", got)
	}
	if !strings.Contains(got, "type (") {
		t.Errorf("混合 type 块形态应保留:\n%s", got)
	}
	// 生成文件必须可解析
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("生成文件解析失败: %v\n%s", err, got)
	}
}

// TestWriteOrReplaceStruct_RemoveValueReceiverToDO 验证用户手写的值接收者 ToDO 与生成的
// 指针接收者 ToDO 同名共存时，再生成移除值接收者版本，避免方法集冲突导致编译失败。
func TestWriteOrReplaceStruct_RemoveValueReceiverToDO(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}

// ToDO 用户手写的值接收者方法
func (e UserEntity) ToDO() int {
	return e.ID
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 值接收者 ToDO 被移除，仅保留生成的指针接收者版本
	assertNotContains(t, got, "func (e UserEntity) ToDO()", "值接收者 ToDO 应被移除")
	if strings.Count(got, "ToDO()") != 1 {
		t.Errorf("ToDO 方法应只保留一份:\n%s", got)
	}
	assertContains(t, got, "func (e *UserEntity) ToDO()", "生成的指针接收者 ToDO 应保留")
	// 生成文件必须可解析
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("生成文件解析失败: %v\n%s", err, got)
	}
}

// TestWriteOrReplaceStruct_RemoveParenthesizedValueReceiverToDO 锁定括号包裹的值接收者
// （func (e (UserEntity)) ToDO()）同样被识别为生成方法：再生成后原位替换为指针接收者版本，
// 不产生同名方法共存。Go 语法允许括号接收者，且落盘语法自校验发现不了方法集冲突，
// 必须由接收者识别逻辑保证产物可编译。
func TestWriteOrReplaceStruct_RemoveParenthesizedValueReceiverToDO(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	for _, recv := range []string{"(UserEntity)", "((UserEntity))", "*(UserEntity)"} {
		t.Run(recv, func(t *testing.T) {
			orig := "package model\n\ntype UserEntity struct {\n\tID int\n}\n\nfunc (e " + recv + ") ToDO() int {\n\treturn e.ID\n}\n\ntype UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}\n"
			got := writeAndVerify(t, orig, entityCode, doCode, nil)
			if n := strings.Count(got, "ToDO()"); n != 1 {
				t.Fatalf("括号值接收者 ToDO 应被替换为唯一一份生成方法，实际 %d 份:\n%s", n, got)
			}
			assertNotContains(t, got, "ToDO() int", "括号值接收者 ToDO 应被移除")
			assertContains(t, got, "func (e *UserEntity) ToDO()", "生成的指针接收者 ToDO 应保留")
			typeCheckGoSource(t, got)
		})
	}
}

// TestBuildStruct_EmptyColumnTagName 验证 ColumnTagName 为空时跳过列名 tag，仅保留 json/description，
// 且无任何 tag 的字段不输出空反引号。
func TestBuildStruct_EmptyColumnTagName(t *testing.T) {
	cols := []Column{
		{Name: "id", Comment: "主键", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64", JsonTagValue: "id"}},
		{Name: "user_name", StructFieldInfo: StructFieldInfo{Name: "UserName", Type: "string"}},
	}
	got := buildStruct("UserInfoEntity", cols, false, "", "", nil)
	// 不生成空 tag 名（反引号后直接跟冒号的 `:"id"` 模式）
	if strings.Contains(got, "`:\"") {
		t.Errorf("ColumnTagName 为空时不应生成空 tag 名:\n%s", got)
	}
	// json/description tag 仍保留
	if !strings.Contains(got, "`json:\"id\" description:\"主键\"`") {
		t.Errorf("json/description tag 应保留:\n%s", got)
	}
	// 无任何 tag 的字段不输出空反引号
	if strings.Contains(got, "``") {
		t.Errorf("无 tag 字段不应输出空 tag:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_PreservePermissions 验证再生成保留原文件权限位（如 0600），不被硬编码 0644 覆盖。
func TestWriteOrReplaceStruct_PreservePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持 POSIX 权限位，保留语义仅在 Unix 上可验证")
	}
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	orig := "package model\n\ntype UserEntity struct {\n\tID int\n}\n"
	if err := os.WriteFile(filePath, []byte(orig), 0600); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("权限位未保持: got %o, want 600", got)
	}
}

// TestWriteOrReplaceStruct_ReadOnlyDir 验证目标目录只读时写入失败、错误可理解、原文件未被破坏、
// 且无临时文件残留（原子写的失败路径）。
func TestWriteOrReplaceStruct_ReadOnlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 目录只读属性不阻止文件创建，该失败路径仅在 Unix 上可验证")
	}
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	orig := "package model\n\ntype UserEntity struct {\n\tID int\n}\n"
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("设置目录只读失败: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err == nil {
		t.Fatalf("目标目录只读时应返回错误，实际为 nil")
	}
	// 原文件内容未被破坏（先写临时文件，失败不影响目标）
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取原文件失败: %v", err)
	}
	if string(content) != orig {
		t.Errorf("写入失败后原文件被破坏:\n%s", content)
	}
	// 无临时文件残留
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("失败后应无临时文件残留，目录内容: %v", entries)
	}
}

// TestWriteOrReplaceStruct_ValueReceiverCustomMethod 验证值接收者自定义方法
// （如 func (e UserEntity) Validate() error）与指针接收者一样被识别为用户声明，并在骨架中原地保留，
// 既不被当作生成方法移除，也不被归位到生成代码之后。
func TestWriteOrReplaceStruct_ValueReceiverCustomMethod(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}

// Validate 值接收者自定义方法
func (e UserEntity) Validate() error {
	return nil
}

// Tag 值接收者自定义方法
func (d UserDO) Tag() string {
	return "do"
}

// FreeFunc 无接收者函数
func FreeFunc() {}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 值接收者自定义方法与无接收者函数均保留
	assertContainsAll(t, got, []string{
		"func (e UserEntity) Validate() error {",
		"func (d UserDO) Tag() string {",
		"func FreeFunc() {",
	}, "值接收者自定义方法与无接收者函数应保留")
	// 骨架保持式：声明顺序与原文件一致（值接收者自定义方法/无接收者函数均原位保留，不再归位）
	order := []string{
		"type UserEntity struct {",
		"func (e *UserEntity) ToDO()",
		"type UserDO struct {",
		"func (d *UserDO) ToEntity()",
		"func (e UserEntity) Validate() error {",
		"func (d UserDO) Tag() string {",
		"func FreeFunc() {",
	}
	last := -1
	for _, s := range order {
		idx := strings.Index(got, s)
		if idx < 0 {
			t.Errorf("输出缺少: %s", s)
			continue
		}
		if idx < last {
			t.Errorf("骨架顺序错误: %s 位置不正确\n输出:\n%s", s, got)
		}
		last = idx
	}
}

// TestWriteOrReplaceStruct_KeepFreeFloatingCommentInMixedBlock 验证混合 type 块中
// 不附着于任何 Spec 的游离注释（前后均有空行）在再生成时原样保留：
// 按源码偏移重建而非经 go/printer 重印，游离注释不会丢失。
func TestWriteOrReplaceStruct_KeepFreeFloatingCommentInMixedBlock(t *testing.T) {
	orig := `package model

type (
	UserEntity struct {
		ID int
	}

	// 游离注释：不与任何类型绑定

	MyHelper struct {
		X int
	}
)

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 游离注释与用户类型均保留
	assertContains(t, got, "// 游离注释：不与任何类型绑定", "混合 type 块中的游离注释应保留")
	assertContains(t, got, "MyHelper struct {", "混合 type 块中的用户类型应保留")
	if strings.Count(got, "UserEntity struct {") != 1 {
		t.Errorf("生成类型应只保留一份:\n%s", got)
	}
	// 生成文件必须可解析
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("生成文件解析失败: %v\n%s", err, got)
	}
}

// TestWriteOrReplaceStruct_KeepDeclLevelComments 验证再生成保留不附着于任何声明 Doc 的用户注释：
// 声明之间的游离注释、//go:generate 指令、位于声明 End() 之后的行尾注释（var/type/func 及 package 行）、
// 文件尾注释，以及位于被移除生成代码（类型与方法）之前的游离注释（顺延给下一个保留声明）均不得丢失，行尾注释仍在原行；
// 被移除生成声明自身的 Doc 注释随之丢弃。产物须通过编译级类型检查，且再生成幂等（二次生成逐字节相同）。
func TestWriteOrReplaceStruct_KeepDeclLevelComments(t *testing.T) {
	orig := `package model // PKG-TRAILING

import "fmt"

// FREE-BEFORE-ENTITY: 位于生成代码之前的游离注释

// OLD-ENTITY-DOC
type UserEntity struct {
	ID int
}

func (e *UserEntity) ToDO() {}

//go:generate echo hello

// FREE: 游离注释

var X = 1 // TRAILING: 行尾注释

type Y int // TRAILING2

func F() { fmt.Println() } // TRAILING3

type UserDO struct {
	ID any
}

// FREE-BEFORE-METHOD: 位于生成方法之前的游离注释

func (d *UserDO) ToEntity() {}

// EOF: 文件尾注释
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	assertContainsAll(t, got, []string{
		"// FREE-BEFORE-ENTITY: 位于生成代码之前的游离注释",
		"// FREE-BEFORE-METHOD: 位于生成方法之前的游离注释",
		"// FREE: 游离注释",
	}, "声明级游离注释应保留")
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^package model // PKG-TRAILING$`),
		regexp.MustCompile(`(?m)^//go:generate echo hello$`),
		regexp.MustCompile(`(?m)^var X = 1 // TRAILING: 行尾注释$`),
		regexp.MustCompile(`(?m)^type Y int // TRAILING2$`),
		regexp.MustCompile(`(?m)^func F\(\) \{ fmt\.Println\(\) \} // TRAILING3$`),
	} {
		if !re.MatchString(got) {
			t.Errorf("指令/行尾注释应保留在原行，未匹配 %v\n实际内容:\n%s", re, truncateForLog(got))
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "// EOF: 文件尾注释") {
		t.Errorf("文件尾注释应保留在产物末尾，实际内容:\n%s", truncateForLog(got))
	}
	assertNotContains(t, got, "// OLD-ENTITY-DOC", "被移除生成声明自身的 Doc 注释应随之丢弃")
	typeCheckGoSource(t, got)

	if again := writeAndVerify(t, got, entityCode, doCode, nil); again != got {
		t.Errorf("再生成应幂等\n第一次:\n%s\n第二次:\n%s", got, again)
	}
}

// TestWriteOrReplaceStruct_RemoveUnusedImports 验证再生成时移除旧生成代码引用过、
// 而新生成代码与保留的用户代码都不再引用的 import spec（如删去最后一个时间列后的 time）：
// 单行 import 整体移除；import 块只移除该 spec 所在整行（连同其 Doc 与尾注释，CRLF 文件同样不留空行）、
// 保留其他 spec；与其他 spec 同行时只剔除该 spec（连同其后的分号，否则残留空 spec 导致语法错误）；
// 用户代码仍引用该包时保留。每个场景的产物都须通过编译级类型检查（排除 imported and not used）。
func TestWriteOrReplaceStruct_RemoveUnusedImports(t *testing.T) {
	// oldGenerated 为旧生成代码：Entity 含 time.Time 字段，ToEntity 中对 time.Time 做类型断言
	const oldGenerated = `type UserEntity struct {
	ID        int64
	CreatedAt time.Time
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID        any
	CreatedAt any
}

func (d *UserDO) ToEntity() {
	if _, ok := d.CreatedAt.(time.Time); ok {
	}
}
`
	// 新生成代码已不含时间列，也不再需要任何 import
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	tests := []struct {
		name        string
		orig        string
		wantImports []string
	}{
		{
			name:        "单行 import 整体移除",
			orig:        "package model\n\nimport \"time\"\n\n" + oldGenerated,
			wantImports: nil,
		},
		{
			name:        "import 块只移除不再引用的 spec",
			orig:        "package model\n\nimport (\n\t\"fmt\"\n\t\"time\" // 时间\n)\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\n",
			wantImports: []string{"fmt"},
		},
		{
			name:        "用户代码仍引用时保留",
			orig:        "package model\n\nimport \"time\"\n\n" + oldGenerated + "\nvar Timeout = time.Second\n",
			wantImports: []string{"time"},
		},
		{
			name:        "被移除 spec 的 Doc 注释随之移除",
			orig:        "package model\n\nimport (\n\t\"fmt\"\n\t// 时间包\n\t\"time\"\n)\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\n",
			wantImports: []string{"fmt"},
		},
		{
			// 被移除 spec 置于块中间：残留空行若紧贴左右括号会被 gofmt 吞掉，夹在两个 spec 之间才可观测
			name:        "CRLF 文件移除整行不留空行",
			orig:        strings.ReplaceAll("package model\n\nimport (\n\t\"fmt\"\n\t\"time\" // 时间\n\t\"strings\"\n)\n\n"+oldGenerated+"\nvar _ = fmt.Sprint\nvar _ = strings.ToUpper\n", "\n", "\r\n"),
			wantImports: []string{"fmt", "strings"},
		},
		{
			name:        "同行多个 spec 时剔除位于行尾的 spec",
			orig:        "package model\n\nimport (\n\t\"fmt\"; \"time\"\n)\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\n",
			wantImports: []string{"fmt"},
		},
		{
			// 被移除 spec 后紧跟分号时须连同分号剔除，残留的 ; 会构成缺少路径的空 spec
			name:        "同行多个 spec 时剔除位于行首的 spec",
			orig:        "package model\n\nimport (\n\t\"time\"; \"fmt\"\n)\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\n",
			wantImports: []string{"fmt"},
		},
		{
			name:        "同行多个 spec 时剔除位于中间的 spec",
			orig:        "package model\n\nimport (\n\t\"fmt\"; \"time\"; \"strings\"\n)\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\nvar _ = strings.ToUpper\n",
			wantImports: []string{"fmt", "strings"},
		},
		{
			name:        "单行括号块中剔除首个 spec",
			orig:        "package model\n\nimport (\"time\"; \"fmt\")\n\n" + oldGenerated + "\nvar _ = fmt.Sprint\n",
			wantImports: []string{"fmt"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := writeAndVerify(t, tt.orig, entityCode, doCode, nil)
			if gotImports := importPathsOf(t, got); !reflect.DeepEqual(gotImports, tt.wantImports) {
				t.Errorf("import = %v, want %v\n实际内容:\n%s", gotImports, tt.wantImports, truncateForLog(got))
			}
			assertNotContains(t, got, "// 时间", "被移除 spec 的尾注释应随整行移除")
			if regexp.MustCompile(`import \([^)]*\n[ \t]*\n[^)]*\)`).MatchString(got) {
				t.Errorf("移除 spec 后 import 块内不应残留空行\n实际内容:\n%s", truncateForLog(got))
			}
			typeCheckGoSource(t, got)
		})
	}
}

// TestImportLocalName 验证 import spec 绑定包名的判定：具名别名取别名、无别名取路径末段；
// _ 与 . 导入、"C"、末段非合法标识符（yaml.v3）与主版本后缀（/v2）返回空串——
// 空串意味着包名不可知，再生成时这些 import 不会被当作未引用而移除。
func TestImportLocalName(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"无别名取末段", `"net/http"`, "http"},
		{"具名别名", `t "time"`, "t"},
		{"空白导入", `_ "time"`, ""},
		{"点导入", `. "time"`, ""},
		{"cgo", `"C"`, ""},
		{"末段非合法标识符", `"gopkg.in/yaml.v3"`, ""},
		{"主版本后缀", `"github.com/a/b/v2"`, ""},
		{"以 v 开头的普通包名", `"example.com/vendorx"`, "vendorx"},
		{"单字母 v", `"example.com/v"`, "v"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := parser.ParseFile(token.NewFileSet(), "", "package p\n\nimport "+tt.src+"\n", parser.ImportsOnly)
			if err != nil {
				t.Fatalf("解析 import 失败: %v", err)
			}
			if got := importLocalName(f.Imports[0]); got != tt.want {
				t.Errorf("importLocalName(%s) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestWriteOrReplaceStruct_PkgNameFromAbsDir 验证新建文件的包名按输出目录绝对路径的末级目录名推导：
// 相对路径（无目录部分，Dir 为 "."）也解析为当前工作目录的实际名称，而不是回退为 main。
// 用 t.Chdir 把工作目录切到临时目录下的 model 目录，既保持"无目录部分"的相对路径语义，
// 又避免在仓库目录下生成文件（残留与并行 go test 竞争）。
func TestWriteOrReplaceStruct_PkgNameFromAbsDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	t.Chdir(dir)
	const filePath = "zcmodel_pkgname_probe_output.go"

	entityCode := "type FallBackEntity struct {\n\tID int\n}"
	doCode := "type FallBackDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "FallBackEntity", entityCode, "FallBackDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.HasPrefix(string(content), "package model\n") {
		t.Fatalf("无目录部分的相对路径应取当前目录名 model 作包名，实际:\n%s", content)
	}
}

// TestPackageNameOf 验证包名推导：普通目录取末级目录名；位于根目录（末级名为路径分隔符）时回退为 main。
func TestPackageNameOf(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	root := filepath.VolumeName(wd) + string(filepath.Separator)
	tests := []struct {
		name     string
		filePath string
		want     string
	}{
		{"普通目录", filepath.Join(t.TempDir(), "model", "user.go"), "model"},
		{"根目录回退 main", root + "user.go", "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := packageNameOf(tt.filePath); got != tt.want {
				t.Errorf("packageNameOf(%q) = %q, want %q", tt.filePath, got, tt.want)
			}
		})
	}
}

// TestWriteOrReplaceStruct_ReadFileFails 覆盖现有文件读取失败的分支：
// 目标路径已存在但为目录时，os.Stat 成功而 ReadFile 失败。
func TestWriteOrReplaceStruct_ReadFileFails(t *testing.T) {
	target := filepath.Join(t.TempDir(), "adir.go")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	err := writeOrReplaceStruct(target, "E", "type E struct{}", "D", "type D struct{}", nil)
	if err == nil {
		t.Fatal("目标路径为目录时 ReadFile 应失败")
	}
}

// TestWriteOrReplaceStruct_SingleImportDocAndComment 覆盖单行 import 声明
// （无括号）合并缺失 import 的路径：ImportSpec 的 Doc/尾注释经 specSpan 保留，
// import 声明自身的 Doc 经 renderImport 展开分支保留，并展开为块形式。
func TestWriteOrReplaceStruct_SingleImportDocAndComment(t *testing.T) {
	orig := `package model

// import block doc
import "fmt" // trailing comment

var _ = fmt.Sprintf
`
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, []string{"time"})
	assertContainsAll(t, got, []string{"// import block doc", "import (", `"fmt" // trailing comment`, `"time"`},
		"单行 import 声明的 Doc/尾注释应保留并展开为块形式")
}

// TestWriteOrReplaceStruct_MixedBlockDocAndTrailing 覆盖混合 type 块剔除生成类型时
// GenDecl 自身 Doc 注释保留（renderTypeBlock 的 d.Doc 分支），
// 以及生成类型 Spec 带尾注释时的区间剔除（specSpan 的 TypeSpec.Comment 分支）。
func TestWriteOrReplaceStruct_MixedBlockDocAndTrailing(t *testing.T) {
	orig := `package model

// block doc
type (
	// user type doc
	UserType struct {
		Name string
	}
	UserEntity struct {
		ID int
	} // generated trailing
)
`
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertContains(t, got, "// block doc", "混合 type 块的块级 Doc 注释应保留")
	assertContainsAll(t, got, []string{"UserType", "// user type doc"}, "用户类型及其注释应保留")
	assertNotContains(t, got, "generated trailing", "旧生成类型的尾注释应随 Spec 一并剔除")
	assertContains(t, got, "int64", "新生成的 Entity 代码应写入")
}

// TestWriteOrReplaceStruct_NonIdentReceiverKept 覆盖 receiverTypeName 对
// 非 Ident/指针接收者（如泛型实例化 Box[int]）返回空串的分支：
// 此类方法既非生成方法也不归属 Entity/DO，原样保留为其他用户代码。
func TestWriteOrReplaceStruct_NonIdentReceiverKept(t *testing.T) {
	orig := `package model

type Box[T any] struct{ V T }

func (b Box[int]) Marker() int { return 0 }

type UserEntity struct {
	ID int
}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertContains(t, got, "func (b Box[int]) Marker()", "泛型实例化接收者的用户方法应保留")
}

// TestWriteOrReplaceStruct_NoImportDeclAddMissing 覆盖文件完全没有 import 声明、
// 但 neededImports 非空时新建 import 声明前置的分支。
func TestWriteOrReplaceStruct_NoImportDeclAddMissing(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}
`
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, []string{"time"})
	assertContains(t, got, `import "time"`, "无 import 声明时应新建 import 声明")
}

// TestWriteOrReplaceStruct_NamedImportWithDoc 覆盖 specSpan 的 ImportSpec.Doc 分支
// 与已有导入统计跳过命名导入（Name 非空）的分支：块内带文档注释的别名导入
// 应完整保留，缺失的标准导入仍可追加。
func TestWriteOrReplaceStruct_NamedImportWithDoc(t *testing.T) {
	orig := `package model

import (
	// fmt alias doc
	f "fmt"
)

type UserEntity struct {
	ID int
}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, []string{"time"})
	assertContainsAll(t, got, []string{"// fmt alias doc", `f "fmt"`, `"time"`},
		"带文档注释的别名导入应保留且缺失的标准导入应追加")
}

// TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition 锁定「骨架保持式合并」的核心契约：
// 用户声明（含 Entity/DO 上的自定义方法）在原位保留，不因再生成被搬动到生成代码之后。
// 用例把用户方法刻意插在 Entity 结构体与 ToDO 方法之间，再生成后它仍应留在该位置。
func TestWriteOrReplaceStruct_SkeletonKeepsUserDeclPosition(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}

// 用户方法：插在 Entity 结构体与 ToDO 之间
func (e *UserEntity) Between() int {
	return e.ID
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}

var Extra = 1
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)

	// 顺序必须与原文件一致：Entity 结构体 < Between < ToDO < DO 结构体 < ToEntity < Extra
	assertDeclOrder(t, got, []string{
		"type UserEntity struct {",
		"func (e *UserEntity) Between() int {",
		"func (e *UserEntity) ToDO()",
		"type UserDO struct {",
		"func (d *UserDO) ToEntity()",
		"var Extra = 1",
	}, "用户声明应原位保留")
	assertContains(t, got, "return e.ID", "用户方法体应完整保留")
}

// TestWriteOrReplaceStruct_MissingGenDeclsPairedInsertion 锁定缺失生成声明「紧跟配对声明」的落点：
// Entity 结构体↔ToDO、DO 结构体↔ToEntity 互为配对，结构体缺失补在其方法之前、方法缺失补在其结构体之后。
func TestWriteOrReplaceStruct_MissingGenDeclsPairedInsertion(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	cases := []struct {
		name  string
		orig  string
		order []string
	}{
		{
			name: "Entity 结构体缺失补在其 ToDO 方法前",
			orig: "package model\n\nfunc (e *UserEntity) ToDO() {}\n\nvar Extra = 1\n",
			order: []string{
				"type UserEntity struct {",
				"func (e *UserEntity) ToDO()",
				"type UserDO struct {",
				"func (d *UserDO) ToEntity()",
				"var Extra = 1",
			},
		},
		{
			name: "DO 结构体缺失补在其 ToEntity 方法前",
			orig: "package model\n\ntype UserEntity struct {\n\tID int\n}\n\nfunc (e *UserEntity) ToDO() {}\n\nfunc (d *UserDO) ToEntity() {}\n",
			order: []string{
				"type UserEntity struct {",
				"func (e *UserEntity) ToDO()",
				"type UserDO struct {",
				"func (d *UserDO) ToEntity()",
			},
		},
		{
			name: "ToEntity 方法缺失补在其 DO 结构体后",
			orig: "package model\n\ntype UserEntity struct {\n\tID int\n}\n\nfunc (e *UserEntity) ToDO() {}\n\ntype UserDO struct {\n\tID any\n}\n",
			order: []string{
				"type UserEntity struct {",
				"func (e *UserEntity) ToDO()",
				"type UserDO struct {",
				"func (d *UserDO) ToEntity()",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := writeAndVerify(t, tc.orig, entityCode, doCode, nil)
			assertDeclOrder(t, got, tc.order, tc.name)
			typeCheckGoSource(t, got)
		})
	}
}

// TestWriteOrReplaceStruct_MissingGenDeclsUnpairedFallback 锁定无配对时的落点：
// 生成声明追加到原文件最后一个生成声明之后；原文件没有任何生成声明时，按标准顺序
// （Entity 结构体 → ToDO → DO 结构体 → ToEntity）置于首个用户声明之前。
func TestWriteOrReplaceStruct_MissingGenDeclsUnpairedFallback(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	want := []string{
		"type UserEntity struct {",
		"func (e *UserEntity) ToDO()",
		"type UserDO struct {",
		"func (d *UserDO) ToEntity()",
		"var Extra = 1",
	}
	for _, tc := range []struct{ name, orig string }{
		{"首次引入 DO 块追加到生成区末尾",
			"package model\n\ntype UserEntity struct {\n\tID int\n}\n\nfunc (e *UserEntity) ToDO() {}\n\nvar Extra = 1\n"},
		{"无任何生成声明时置于首个用户声明之前",
			"package model\n\nvar Extra = 1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := writeAndVerify(t, tc.orig, entityCode, doCode, nil)
			assertDeclOrder(t, got, want, tc.name)
			typeCheckGoSource(t, got)
		})
	}
}

// TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized 锁定仅含一个生成 Spec 的 type 括号块
// 以独立声明形态输出（不保留括号块），与首次生成的形态一致。
func TestWriteOrReplaceStruct_SingleSpecTypeBlockNormalized(t *testing.T) {
	orig := `package model

type (
	UserEntity struct {
		ID int
	}
)

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}
`
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertNotContains(t, got, "type (", "单 spec 生成类型应展开为独立声明、不保留括号块")
	assertDeclOrder(t, got, []string{
		"type UserEntity struct {",
		"func (e *UserEntity) ToDO()",
		"type UserDO struct {",
		"func (d *UserDO) ToEntity()",
	}, "单 spec 括号块展开后的声明顺序")
	typeCheckGoSource(t, got)
}

// TestWriteOrReplaceStruct_EmptyGeneratedSegmentRemovesSlot 锁定生成代码不含某声明（该段为空）时，
// 原文件中对应的旧槽位被移除，其余声明照常原位替换。
func TestWriteOrReplaceStruct_EmptyGeneratedSegmentRemovesSlot(t *testing.T) {
	orig := `package model

type UserEntity struct {
	ID int
}

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}
`
	// 生成代码只含结构体、不含互转方法
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertNotContains(t, got, "func (e *UserEntity) ToDO()", "生成段为空时旧 ToDO 槽位应被移除")
	assertNotContains(t, got, "func (d *UserDO) ToEntity()", "生成段为空时旧 ToEntity 槽位应被移除")
	assertContains(t, got, "type UserEntity struct {", "结构体照常原位替换")
	assertContains(t, got, "int64", "新结构体内容应生效")
	typeCheckGoSource(t, got)
}

// TestWriteOrReplaceStruct_MixedBlockGeneratedSpecKeepsNewDoc 锁定混合 type 块内生成 Spec 就地替换为
// 「Spec 形态」时保留新生成的 doc 注释（含索引块），旧 doc 随旧 Spec 一并丢弃。
func TestWriteOrReplaceStruct_MixedBlockGeneratedSpecKeepsNewDoc(t *testing.T) {
	orig := `package model

type (
	// OLD-DOC 旧注释
	UserEntity struct {
		ID int
	}

	// MyHelper 用户类型
	MyHelper struct {
		X int
	}
)

func (e *UserEntity) ToDO() {}

type UserDO struct {
	ID any
}

func (d *UserDO) ToEntity() {}
`
	entityCode := "// UserEntity 新表注释\n//\n// 索引:\n//   - PRIMARY KEY (id)\ntype UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	got := writeAndVerify(t, orig, entityCode, doCode, nil)
	assertContainsAll(t, got, []string{
		"// UserEntity 新表注释",
		"//   - PRIMARY KEY (id)",
		"MyHelper struct {",
	}, "混合块内生成 Spec 应携带新 doc，用户类型保留")
	assertNotContains(t, got, "OLD-DOC", "旧生成 Spec 的 doc 应随 Spec 一并丢弃")
	assertContains(t, got, "type (", "混合 type 块形态应保留")
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("产物解析失败: %v\n%s", err, got)
	}
}

// ==================== ddl 片段组装（assembleDDLFragment） ====================

// TestAssembleDDLFragment_NullableTriState 验证 NULL 标记的三态语义：
// true → NULL、false → NOT NULL、nil（未知）→ 整段省略。
// nil 省略是有意为之——手工构造 Input 未设置该字段时，若缺省渲染 NOT NULL 会把可空列静默误标。
func TestAssembleDDLFragment_NullableTriState(t *testing.T) {
	cases := []struct {
		name     string
		nullable *bool
		want     string
	}{
		{"未知省略标记", nil, "varchar(255)"},
		{"可空渲染 NULL", boolPtr(true), "varchar(255) NULL"},
		{"非空渲染 NOT NULL", boolPtr(false), "varchar(255) NOT NULL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assembleDDLFragment(Column{Type: "varchar(255)", Nullable: tc.nullable})
			if got != tc.want {
				t.Errorf("assembleDDLFragment() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAssembleDDLFragment_Default 验证 DEFAULT 段的在场判定与渲染：
// 判定只看指针不看值——nil 无段、空串补一对单引号还原字面量、其余值方言原样（裸值/表达式/带引号字面量）。
func TestAssembleDDLFragment_Default(t *testing.T) {
	cases := []struct {
		name string
		col  Column
		want string
	}{
		{"无默认值", Column{Type: "varchar(10)", Nullable: boolPtr(false)}, "varchar(10) NOT NULL"},
		{"空串默认值补引号还原字面量", Column{Type: "varchar(10)", Nullable: boolPtr(false), Default: strPtr("")}, "varchar(10) NOT NULL DEFAULT ''"},
		{"MySQL 裸值原样", Column{Type: "varchar(10)", Nullable: boolPtr(false), Default: strPtr("pending")}, "varchar(10) NOT NULL DEFAULT pending"},
		{"数值原样", Column{Type: "decimal(10,2)", Nullable: boolPtr(false), Default: strPtr("0.00")}, "decimal(10,2) NOT NULL DEFAULT 0.00"},
		{"PG 表达式原样", Column{Type: "character varying(20)", Nullable: boolPtr(false), Default: strPtr("'pending'::character varying")}, "character varying(20) NOT NULL DEFAULT 'pending'::character varying"},
		{"SQLite 带引号字面量原样", Column{Type: "text", Nullable: boolPtr(true), Default: strPtr("'x'")}, "text NULL DEFAULT 'x'"},
		{"可空性未知时默认值仍追加", Column{Type: "text", Default: strPtr("1.5")}, "text DEFAULT 1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := assembleDDLFragment(tc.col); got != tc.want {
				t.Errorf("assembleDDLFragment() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAssembleDDLFragment_ExtraWhiteList 验证 MySQL EXTRA 的白名单过滤与固定输出顺序：
// 仅 auto_increment、on update CURRENT_TIMESTAMP、VIRTUAL GENERATED、STORED GENERATED 四项保留，
// 按白名单顺序以大写形态输出，ON UPDATE 项保留紧随其后的 (n) 精度（括号内须为数字）；
// DEFAULT_GENERATED 是噪音、其余标记（如 NDB 的 STORAGE DISK）不在白名单，一律过滤。
func TestAssembleDDLFragment_ExtraWhiteList(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		extra string
		want  string
	}{
		{"自增小写转大写", "bigint", "auto_increment", "bigint NOT NULL AUTO_INCREMENT"},
		{"DEFAULT_GENERATED 过滤（默认值已由 DEFAULT 段呈现）", "datetime", "DEFAULT_GENERATED", "datetime NOT NULL"},
		{"虚拟生成列保留（该列不可写）", "int", "VIRTUAL GENERATED", "int NOT NULL VIRTUAL GENERATED"},
		{"存储生成列保留", "int", "STORED GENERATED", "int NOT NULL STORED GENERATED"},
		{"多项按白名单顺序而非输入顺序", "timestamp", "DEFAULT_GENERATED on update CURRENT_TIMESTAMP", "timestamp NOT NULL ON UPDATE CURRENT_TIMESTAMP"},
		{"非白名单标记过滤", "varchar(10)", "STORAGE DISK", "varchar(10) NOT NULL"},
		{"匹配大小写不敏感", "bigint", "AUTO_INCREMENT", "bigint NOT NULL AUTO_INCREMENT"},
		// MySQL 8.4 实测：datetime(3)/timestamp(6) 的 EXTRA 形如 on update CURRENT_TIMESTAMP(3)，精度须随 ON UPDATE 保留
		{"ON UPDATE 保留精度", "datetime(3)", "DEFAULT_GENERATED on update CURRENT_TIMESTAMP(3)", "datetime(3) NOT NULL ON UPDATE CURRENT_TIMESTAMP(3)"},
		{"ON UPDATE 保留多位精度", "timestamp(6)", "on update CURRENT_TIMESTAMP(6)", "timestamp(6) NOT NULL ON UPDATE CURRENT_TIMESTAMP(6)"},
		{"括号内非数字不视为精度", "datetime", "on update CURRENT_TIMESTAMP(x)", "datetime NOT NULL ON UPDATE CURRENT_TIMESTAMP"},
		{"空括号不视为精度", "datetime", "on update CURRENT_TIMESTAMP()", "datetime NOT NULL ON UPDATE CURRENT_TIMESTAMP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			col := Column{Type: tc.typ, Nullable: boolPtr(false), Extra: tc.extra}
			if got := assembleDDLFragment(col); got != tc.want {
				t.Errorf("assembleDDLFragment() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAssembleDDLFragment_TypePreservedVerbatim 验证类型原样透传：
// enum 值域的单引号、括号与 unsigned 修饰符不被改写（片段是重组的类 DDL 文本，不做归一化）。
func TestAssembleDDLFragment_TypePreservedVerbatim(t *testing.T) {
	col := Column{Type: "enum('pending','paid')", Nullable: boolPtr(false), Extra: "auto_increment"}
	want := "enum('pending','paid') NOT NULL AUTO_INCREMENT"
	if got := assembleDDLFragment(col); got != want {
		t.Errorf("assembleDDLFragment() = %q, want %q", got, want)
	}
}

// ==================== 索引注释块（buildIndexCommentBlock） ====================

// TestBuildIndexCommentBlock_Ordering 验证排序固定为 PRIMARY → UNIQUE（索引名字典序）→ 普通（索引名字典序），
// 输出稳定以利于 git diff 与 golden 断言；并验证排序在副本上进行，不改动调用方传入的切片。
func TestBuildIndexCommentBlock_Ordering(t *testing.T) {
	indexes := []IndexInfo{
		{Name: "idx_b", Columns: []string{"c"}},
		{Name: "uk_zzz", Columns: []string{"b"}, Unique: true},
		{Name: "user_order_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "idx_a", Columns: []string{"d"}},
		{Name: "uk_aaa", Columns: []string{"a"}, Unique: true},
	}
	want := []string{
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_aaa (a)",
		"//   - UNIQUE KEY uk_zzz (b)",
		"//   - KEY idx_a (d)",
		"//   - KEY idx_b (c)",
	}
	got := buildIndexCommentBlock(indexes)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("索引块排序错误\ngot:  %q\nwant: %q", got, want)
	}
	// 渲染不得改动调用方传入的切片（generate.go 直接透传 Input.Indexes）
	if indexes[0].Name != "idx_b" || indexes[2].Name != "user_order_pkey" {
		t.Errorf("渲染改动了调用方切片顺序: %v", indexes)
	}
}

// TestBuildIndexCommentBlock_Format 验证渲染格式：主键行不显示方言内部物理索引名
// （渲染端只依据 Primary 标记判定，不依赖索引名的命名形态——PG 的 xxx_pkey 与 SQLite 风格的
// sqlite_autoindex_* 均被隐藏；后者实践中由 zcdb 合成为 PRIMARY，此处按单元输入锁定该规则）、
// 多列按定义顺序以逗号加空格连接、非主键索引名原样呈现、表达式列占位符 #expr 透传；
// 索引名与列文本中的换行净化为空格，防止注释行断裂。
// 排序键为净化前的原始索引名（真实索引名不含换行，此处 "idx\nnewline" 因 '\n' 的字节序小于 '_' 而排在前面）。
func TestBuildIndexCommentBlock_Format(t *testing.T) {
	got := buildIndexCommentBlock([]IndexInfo{
		{Name: "user_order_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "sqlite_autoindex_probe_1", Columns: []string{"tenant_id"}, Unique: true, Primary: true},
		{Name: "uk_multi", Columns: []string{"user_id", "status"}, Unique: true},
		{Name: "idx_expr", Columns: []string{"#expr"}},
		{Name: "idx\nnewline", Columns: []string{"a\nb"}},
	})
	want := []string{
		// 两个主键行（单元输入刻意同时给两种方言命名形态）之间仍按索引名字典序稳定排列
		"//   - PRIMARY KEY (tenant_id)",
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_multi (user_id, status)",
		"//   - KEY idx newline (a b)",
		"//   - KEY idx_expr (#expr)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("索引块渲染错误\ngot:  %q\nwant: %q", got, want)
	}
	joined := strings.Join(got, "\n")
	assertNotContains(t, joined, "user_order_pkey", "主键行不应显示 PG 的物理索引名")
	assertNotContains(t, joined, "sqlite_autoindex_probe_1", "主键行不应显示 SQLite 风格的自动索引名")
}

// TestBuildIndexCommentBlock_Empty 验证无索引时整块省略：nil 与空切片均返回空结果，
// 供 buildStructDocComment 只渲染首行（数据驱动，无数据自然省略）。
func TestBuildIndexCommentBlock_Empty(t *testing.T) {
	if got := buildIndexCommentBlock(nil); len(got) != 0 {
		t.Errorf("nil 索引应返回空结果，实际: %q", got)
	}
	if got := buildIndexCommentBlock([]IndexInfo{}); len(got) != 0 {
		t.Errorf("空索引切片应返回空结果，实际: %q", got)
	}
}

// ==================== doc 注释组装（buildStructDocComment） ====================

// TestBuildStructDocComment 验证注释组装规则：首行为既有 comment，索引块前插空注释行与「索引:」标题；
// comment 与索引均为空时不生成注释；仅有索引（comment 为空）时只渲染索引块。
func TestBuildStructDocComment(t *testing.T) {
	indexes := []IndexInfo{{Name: "user_order_pkey", Columns: []string{"id"}, Unique: true, Primary: true}}
	cases := []struct {
		name    string
		comment string
		indexes []IndexInfo
		want    string
	}{
		{"两者皆空不生成注释", "", nil, ""},
		{"仅索引块", "", indexes, "//\n// 索引:\n//   - PRIMARY KEY (id)\n"},
		{"首行加索引块", "T 表", indexes, "// T 表\n//\n// 索引:\n//   - PRIMARY KEY (id)\n"},
		{"仅首行", "T 表", nil, "// T 表\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildStructDocComment(tc.comment, tc.indexes); got != tc.want {
				t.Errorf("buildStructDocComment() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSanitizeCommentLine 验证换行净化：LF、CRLF、CR 均替换为单个空格（CRLF 不被拆成两个空格），
// 无换行的文本原样返回；连续两个单引号/反引号之间插入零宽空格 U+200B（三连及以上逐对插入，
// 结果中不再残留任何相邻引号对）。
func TestSanitizeCommentLine(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"LF", "a\nb", "a b"},
		{"CRLF 只替换为一个空格", "a\r\nb", "a b"},
		{"CR", "a\rb", "a b"},
		{"无换行原样", "正常 文本", "正常 文本"},
		{"空串", "", ""},
		{"单个单引号原样", "it's", "it's"},
		{"连续两个单引号", "默认''", "默认'\u200b'"},
		{"连续三个单引号", "'''", "'\u200b'\u200b'"},
		{"连续两个反引号", "``x``", "`\u200b`x`\u200b`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeCommentLine(tc.in); got != tc.want {
				t.Errorf("sanitizeCommentLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestBuildStruct_TableCommentNewlineRegression 回归锁死：TableComment 含换行/回车时
// doc 注释被净化为单行，不再生成裸行（裸行会让落盘语法自校验报错且根因难定位）。
func TestBuildStruct_TableCommentNewlineRegression(t *testing.T) {
	cols := []Column{{Name: "id", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64"}}}
	got := buildStruct("TEntity", cols, false, "订单表\n第二行\r\n第三行", "db", nil)
	if !strings.HasPrefix(got, "// 订单表 第二行 第三行\ntype TEntity struct {") {
		t.Errorf("TableComment 换行应净化为空格并保持单行注释:\n%s", got)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors); err != nil {
		t.Errorf("净化后的产物应可解析: %v\n%s", err, got)
	}
}

// TestBuildStruct_DocCommentQuotesSurviveGofmt 回归锁死：表注释、索引名与索引列文本中的连续单引号/反引号
// 经 gofmt（go/format 对声明级 doc 注释做智能引号规范化）后不得被改写为弯引号 ” / “；
// 断言 gofmt 前后 doc 注释逐字节一致，且去掉零宽空格后仍是原文。
func TestBuildStruct_DocCommentQuotesSurviveGofmt(t *testing.T) {
	cols := []Column{{Name: "id", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64"}}}
	indexes := []IndexInfo{{Name: "idx_''a", Columns: []string{"lower(''x'')"}}}
	got := buildStruct("TEntity", cols, false, "默认值为'' 或 ``x``", "db", indexes)
	formatted, err := format.Source([]byte("package model\n\n" + got + "\n"))
	if err != nil {
		t.Fatalf("产物无法通过 gofmt: %v\n%s", err, got)
	}
	out := string(formatted)
	assertNotContains(t, out, "”", "doc 注释不应出现右弯引号")
	assertNotContains(t, out, "“", "doc 注释不应出现左弯引号")
	plain := strings.ReplaceAll(out, "\u200b", "")
	assertContainsAll(t, plain, []string{
		"// 默认值为'' 或 ``x``\n",
		"//   - KEY idx_''a (lower(''x''))\n",
	}, "去掉零宽空格后应还原原文")
}

// ==================== 原子写入（writeFileAtomic） ====================

// TestWriteFileAtomic_StatErrorNonNotExist 覆盖 writeFileAtomic 中
// os.Stat 返回「非 NotExist」错误时直接上报的分支（如权限错误/非法文件名）。
func TestWriteFileAtomic_StatErrorNonNotExist(t *testing.T) {
	dir := t.TempDir()

	var filePath string
	if runtime.GOOS == "windows" {
		// Windows：非法文件名字符使 CreateFile 返回 ERROR_INVALID_NAME（非 NotExist）
		filePath = filepath.Join(dir, "bad|name.go")
	} else {
		// Unix：无权限目录使 Stat 返回 EACCES（非 NotExist）
		blocked := filepath.Join(dir, "noperm")
		if err := os.MkdirAll(blocked, 0755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if err := os.Chmod(blocked, 0); err != nil {
			t.Fatalf("修改目录权限失败: %v", err)
		}
		// Cleanup 中故意忽略错误：仅恢复临时目录权限，失败不影响断言结果（测试结束即随 TempDir 删除）
		t.Cleanup(func() { _ = os.Chmod(blocked, 0755) })
		filePath = filepath.Join(blocked, "inner.go")
	}

	err := writeFileAtomic(filePath, []byte("package x"))
	if err == nil {
		t.Fatal("期望 Stat 返回非 NotExist 错误并上报，实际成功")
	}
}

// TestWriteFileAtomic_CreateTempFails 覆盖目标目录不存在时
// os.Stat 返回 NotExist（使用默认 0644）后 CreateTemp 失败的分支。
func TestWriteFileAtomic_CreateTempFails(t *testing.T) {
	root := t.TempDir()
	missingDir := filepath.Join(root, "no_such_dir")
	filePath := filepath.Join(missingDir, "a.go")
	err := writeFileAtomic(filePath, []byte("package x"))
	if err == nil || !strings.Contains(err.Error(), "创建临时文件失败") {
		t.Fatalf("期望创建临时文件失败错误，实际: %v", err)
	}
	// 不存在的父目录不应被隐式创建（writeFileAtomic 不负责建目录）
	if _, statErr := os.Stat(missingDir); !os.IsNotExist(statErr) {
		t.Fatalf("父目录 no_such_dir 不应被隐式创建，os.Stat 返回: %v", statErr)
	}
}

// TestWriteFileAtomic_RenameFails 覆盖 rename 替换目标失败的分支：
// 目标路径已存在且为目录时，各平台的覆盖式 rename 均失败。
func TestWriteFileAtomic_RenameFails(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "adir")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	err := writeFileAtomic(target, []byte("package x"))
	if err == nil || !strings.Contains(err.Error(), "替换目标文件失败") {
		t.Fatalf("期望替换目标文件失败错误，实际: %v", err)
	}

	// rename 失败后 defer 应清理临时文件：父目录内只应剩下目标条目
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取父目录失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "adir" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("rename 失败后不应有临时文件残留，父目录实际条目: %v", names)
	}
	// 目标未被破坏：仍是原来的目录
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("目标路径应仍存在: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("rename 失败后目标路径应仍为目录，不应被替换为文件")
	}
}

// TestBuildStruct_CompositePrimaryKey 验证联合主键：多列均生成 primary_key:"true"
// （主键在 DDL 中是表级约束而非列内联属性，故不并入 ddl 片段，仅由该 tag 与索引块呈现）。
func TestBuildStruct_CompositePrimaryKey(t *testing.T) {
	cols := []Column{
		{Name: "user_id", Type: "bigint", Nullable: boolPtr(false), PrimaryKey: true, StructFieldInfo: StructFieldInfo{Name: "UserID", Type: "int64"}},
		{Name: "role_id", Type: "bigint", Nullable: boolPtr(false), PrimaryKey: true, StructFieldInfo: StructFieldInfo{Name: "RoleID", Type: "int64"}},
		{Name: "granted_at", Type: "datetime", Nullable: boolPtr(false), StructFieldInfo: StructFieldInfo{Name: "GrantedAt", Type: "time.Time"}},
	}
	got := buildStruct("UserRoleEntity", cols, false, "", "db", nil)
	if n := strings.Count(got, `primary_key:"true"`); n != 2 {
		t.Errorf("联合主键应标记两列，实际标记 %d 处:\n%s", n, got)
	}
	if strings.Contains(got, "granted_at\" primary_key") {
		t.Errorf("非主键列不应带 primary_key tag:\n%s", got)
	}
	// 主键标记位于 ddl 之前（tag 顺序 json → db → primary_key → ddl → description）
	assertContains(t, got, "`db:\"user_id\" primary_key:\"true\" ddl:\"bigint NOT NULL\"`", "主键列的 tag 顺序")
	// ddl 片段不含 PRIMARY KEY 约束文本（表级信息由 primary_key tag 与索引块承载）
	assertNotContains(t, got, "ddl:\"bigint NOT NULL PRIMARY KEY", "ddl 片段不应内联主键约束")
}

// TestBuildStruct_EmptyColumnsWithIndexes 覆盖「空列 + 非空索引」组合：
// 结构体为空但索引块照常渲染（数据驱动，两者各自独立），且产物可被 go/parser 解析。
func TestBuildStruct_EmptyColumnsWithIndexes(t *testing.T) {
	indexes := []IndexInfo{{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Primary: true}}
	got := buildStruct("EmptyEntity", nil, false, "空表", "db", indexes)
	want := "// 空表\n//\n// 索引:\n//   - PRIMARY KEY (id)\ntype EmptyEntity struct {\n}"
	if got != want {
		t.Errorf("空列 + 非空索引的产物错误\ngot:  %q\nwant: %q", got, want)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+got, parser.AllErrors); err != nil {
		t.Errorf("产物应可解析: %v\n%s", err, got)
	}
}

// TestBuildIndexCommentBlock_SymbolOnlyName 覆盖纯符号索引名（如 MySQL 函数索引的内部名、用户自造的符号名）：
// 不校验列名/索引名合法性，原样呈现（索引块是提示性元数据，严格校验会误报表达式索引），
// 且符号文本不会破坏注释行结构（产物仍可解析）。
func TestBuildIndexCommentBlock_SymbolOnlyName(t *testing.T) {
	got := buildIndexCommentBlock([]IndexInfo{
		{Name: "-|-", Columns: []string{"a"}},
		{Name: "#expr_idx", Columns: []string{"#expr"}, Unique: true},
	})
	want := []string{
		"//   - UNIQUE KEY #expr_idx (#expr)",
		"//   - KEY -|- (a)",
	}
	if len(got) != len(want) {
		t.Fatalf("索引块行数错误\ngot:  %q\nwant: %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("索引块第 %d 行错误\ngot:  %q\nwant: %q", i+1, got[i], want[i])
		}
	}
	structCode := buildStruct("TEntity", []Column{{Name: "a", Type: "text", StructFieldInfo: StructFieldInfo{Name: "A", Type: "string"}}}, false, "T 表", "db", []IndexInfo{{Name: "-|-", Columns: []string{"a"}}})
	if _, err := parser.ParseFile(token.NewFileSet(), "", "package model\n"+structCode, parser.AllErrors); err != nil {
		t.Errorf("含符号索引名的产物应可解析: %v\n%s", err, structCode)
	}
}

// TestSplitGeneratedCode 验证生成代码按「结构体名 / 方法名」拆分：
// 两段各自携带其 Doc 注释；缺失声明返回空串；语法非法时返回解析错误。
func TestSplitGeneratedCode(t *testing.T) {
	t.Run("struct_and_documented_method", func(t *testing.T) {
		code := "// UserEntity 表实体\ntype UserEntity struct {\n\tID int64\n}\n\n// ToDO 转为 DO\nfunc (e *UserEntity) ToDO() UserDO {\n\treturn UserDO{}\n}"
		structText, methodText, err := splitGeneratedCode(code, "UserEntity", "ToDO")
		if err != nil {
			t.Fatalf("splitGeneratedCode() error = %v", err)
		}
		assertContains(t, structText, "// UserEntity 表实体", "结构体段应含前置 Doc 注释")
		assertContains(t, structText, "type UserEntity struct", "结构体段应含结构体声明")
		assertContains(t, methodText, "// ToDO 转为 DO", "方法段应含前置 Doc 注释")
		assertContains(t, methodText, "func (e *UserEntity) ToDO()", "方法段应含方法声明")
	})

	t.Run("missing_declarations", func(t *testing.T) {
		structText, methodText, err := splitGeneratedCode("", "UserEntity", "ToDO")
		if err != nil {
			t.Fatalf("splitGeneratedCode() error = %v", err)
		}
		if structText != "" || methodText != "" {
			t.Fatalf("空代码应返回两段空串，实际 struct=%q method=%q", structText, methodText)
		}
	})

	t.Run("syntax_error", func(t *testing.T) {
		if _, _, err := splitGeneratedCode("type UserEntity struct {", "UserEntity", "ToDO"); err == nil {
			t.Fatal("语法非法的生成代码应返回解析错误")
		}
	})
}

// TestWriteOrReplaceStruct_InvalidGeneratedCode 验证存量文件再生成时，
// 生成代码语法非法（结构体段、方法段分别构造）返回带「生成代码存在语法错误」的错误，
// 且错误发生在落盘之前——原文件内容逐字节不变、不残留临时文件。
func TestWriteOrReplaceStruct_InvalidGeneratedCode(t *testing.T) {
	cases := []struct {
		name       string
		entityCode string
		doCode     string
	}{
		{"invalid_entity_code", "type UserEntity struct {", "type UserDO struct{}"},
		{"invalid_do_code", "type UserEntity struct{}", "type UserDO struct {"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			filePath := filepath.Join(dir, "user.go")
			const orig = "package model\n\ntype Keep struct {\n\tName string\n}\n"
			if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
				t.Fatalf("写入初始文件失败: %v", err)
			}

			err := writeOrReplaceStruct(filePath, "UserEntity", tc.entityCode, "UserDO", tc.doCode, nil)
			if err == nil || !strings.Contains(err.Error(), "生成代码存在语法错误") {
				t.Fatalf("非法生成代码应报语法错误，实际 err=%v", err)
			}
			got, readErr := os.ReadFile(filePath)
			if readErr != nil {
				t.Fatalf("读取文件失败: %v", readErr)
			}
			if string(got) != orig {
				t.Fatalf("失败后原文件应逐字节不变\nwant:\n%s\ngot:\n%s", orig, got)
			}
			entries, readDirErr := os.ReadDir(dir)
			if readDirErr != nil {
				t.Fatalf("读取目录失败: %v", readDirErr)
			}
			if len(entries) != 1 {
				t.Fatalf("失败后不应残留临时文件，实际目录项 %d 个", len(entries))
			}
		})
	}
}

// TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls 验证骨架保持式合并在
// 生成声明缺失/重复时的槽位处理：生成代码不含 DO 时，原文件中独立声明的 DO 结构体、
// 仅含生成类型的括号块、与用户类型混排的括号块内的生成 Spec，均连同其 ToEntity 方法一并移除；
// 同类生成声明重复出现时只保留首个槽位。
func TestWriteOrReplaceStruct_DropAndDedupeGeneratedDecls(t *testing.T) {
	// 只传 Entity 结构体（不含任何方法与 DO）：原文件中的全部 DO 槽位与其方法都应被移除
	const entityOnly = "type UserEntity struct {\n\tID int64\n}"
	// 完整生成代码：供块内/跨槽位重复生成 Spec 的去重用例使用
	const entityCode = "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	const doCode = "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"

	t.Run("drop_single_decl", func(t *testing.T) {
		orig := "package model\n\ntype UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() UserDO {\n\treturn UserDO{}\n}\n\ntype UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() UserEntity {\n\treturn UserEntity{}\n}\n"
		got := writeAndVerify(t, orig, entityOnly, "", nil)
		assertNotContains(t, got, "type UserDO struct", "doCode 为空应移除 DO 结构体声明")
		assertNotContains(t, got, "func (d *UserDO) ToEntity()", "doCode 为空应移除 ToEntity 方法")
		assertNotContains(t, got, "func (e *UserEntity) ToDO()", "entityCode 不含方法应移除旧 ToDO 方法")
		assertContains(t, got, "type UserEntity struct", "Entity 结构体应保留")
		typeCheckGoSource(t, got)
	})

	t.Run("drop_only_generated_block", func(t *testing.T) {
		orig := "package model\n\ntype UserEntity struct {\n\tID int64\n}\n\ntype (\n\tUserDO struct {\n\t\tID any\n\t}\n)\n"
		got := writeAndVerify(t, orig, entityOnly, "", nil)
		assertNotContains(t, got, "type UserDO struct", "仅含生成类型的括号块应整块移除")
		assertNotContains(t, got, "type (\n", "移除后不应残留空括号块")
		typeCheckGoSource(t, got)
	})

	t.Run("drop_from_mixed_block", func(t *testing.T) {
		orig := "package model\n\ntype UserEntity struct {\n\tID int64\n}\n\ntype (\n\tUserDO struct {\n\t\tID any\n\t}\n\n\tKeep struct {\n\t\tName string\n\t}\n)\n"
		got := writeAndVerify(t, orig, entityOnly, "", nil)
		assertNotContains(t, got, "type UserDO struct", "混排块内生成 Spec 应被剔除")
		assertContains(t, got, "type (\n", "含用户类型的括号块应保留块形态")
		assertContains(t, got, "Keep struct", "同块内的用户类型应原样保留")
		typeCheckGoSource(t, got)
	})

	t.Run("dedupe_duplicate_generated_decls", func(t *testing.T) {
		// 同类生成声明重复（用户手写与生成共存的历史产物）：语法合法但语义重复，解析不报错
		orig := "package model\n\ntype UserEntity struct {\n\tID int64\n}\n\ntype UserDO struct {\n\tID any\n}\n\ntype UserDO struct {\n\tID any\n}\n"
		got := writeAndVerify(t, orig, entityOnly, "type UserDO struct {\n\tID any\n}", nil)
		if n := strings.Count(got, "type UserDO struct"); n != 1 {
			t.Fatalf("重复的生成类型声明应去重为 1 份，实际 %d 份\n%s", n, got)
		}
		typeCheckGoSource(t, got)
	})

	t.Run("dedupe_duplicate_generated_specs_in_mixed_block", func(t *testing.T) {
		// 同一 type 块内的生成 Spec 重复：首个原位替换、其余剔除，用户类型保留
		orig := "package model\n\ntype (\n\tUserEntity struct {\n\t\tID int\n\t}\n\tUserEntity struct {\n\t\tID string\n\t}\n\tKeep struct {\n\t\tName string\n\t}\n)\n"
		got := writeAndVerify(t, orig, entityCode, doCode, nil)
		if n := strings.Count(got, "UserEntity struct {"); n != 1 {
			t.Fatalf("块内重复的生成 Spec 应去重为 1 份，实际 %d 份\n%s", n, got)
		}
		assertContains(t, got, "Keep struct", "同块内的用户类型应保留")
		typeCheckGoSource(t, got)
	})

	t.Run("dedupe_block_spec_after_standalone_decl", func(t *testing.T) {
		// 独立生成声明在前、type 块内又出现同名生成 Spec：块内重复项剔除，不残留重复类型
		orig := "package model\n\ntype UserEntity struct {\n\tID int\n}\n\ntype (\n\tUserEntity struct {\n\t\tID string\n\t}\n\tKeep struct {\n\t\tName string\n\t}\n)\n"
		got := writeAndVerify(t, orig, entityCode, doCode, nil)
		if n := strings.Count(got, "UserEntity struct {"); n != 1 {
			t.Fatalf("跨槽位重复的生成 Spec 应去重为 1 份，实际 %d 份\n%s", n, got)
		}
		assertContains(t, got, "Keep struct", "同块内的用户类型应保留")
		typeCheckGoSource(t, got)
	})

	t.Run("dedupe_only_duplicate_specs_block_dropped", func(t *testing.T) {
		// 块内全部生成 Spec 均为前序重复且无用户类型：整块移除，不残留空的 type 括号块
		orig := "package model\n\ntype UserEntity struct {\n\tID int\n}\n\ntype (\n\tUserEntity struct {\n\t\tID string\n\t}\n\tUserEntity struct {\n\t\tID bool\n\t}\n)\n"
		got := writeAndVerify(t, orig, entityCode, doCode, nil)
		if n := strings.Count(got, "UserEntity struct {"); n != 1 {
			t.Fatalf("重复的生成 Spec 应去重为 1 份，实际 %d 份\n%s", n, got)
		}
		assertNotContains(t, got, "type (", "全部为前序重复的生成块应整块移除")
		typeCheckGoSource(t, got)
	})
}
