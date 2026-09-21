package zcmodel

import (
	"go/ast"
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

// TestWriteOrReplaceStruct_NewFile 验证文件不存在时创建新文件
func TestWriteOrReplaceStruct_NewFile(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, "", entityCode, doCode, nil)
	want := fileHeaderPrefix + "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
	if got != want {
		t.Errorf("新建文件内容错误\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// TestWriteOrReplaceStruct_EmptyFile 验证空文件按新建处理
func TestWriteOrReplaceStruct_EmptyFile(t *testing.T) {
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	got := writeAndVerify(t, "  \n", entityCode, doCode, nil)
	want := fileHeaderPrefix + "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
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
	// 布局顺序：Entity 生成代码 < Entity 自定义方法 < DO 生成代码 < DO 自定义方法 < 其他用户代码
	order := []string{
		"type UserEntity struct {",
		"func (e *UserEntity) Hello() string {",
		"type UserDO struct {",
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
			t.Errorf("布局顺序错误: %s 应位于 %s 之后", s, order[0])
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
	if !strings.HasPrefix(got, fileHeaderPrefix+"package model\n\nimport \"time\"\n\ntype UserEntity struct {") {
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
	want := fileHeaderPrefix + "package model\n\nimport (\n\t\"github.com/foo/bar\"\n\t\"time\"\n)\n\n"
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

	// 说明头补写于最顶，其后为文件头（build tags + package 注释 + 原 package 行），逐字节保留
	wantHeader := fileHeaderPrefix + "//go:build ignore\n// +build ignore\n\n// Package custompkg 包文档注释。\npackage custompkg\n"
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
// 再生成仅剔除生成的类型（按 Spec 粒度），用户类型及其注释完整保留。
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
	// 生成类型各只一份（块内的旧声明被剔除）
	if strings.Count(got, "type UserEntity struct {") != 1 || strings.Count(got, "type UserDO struct {") != 1 {
		t.Errorf("生成类型应各只保留一份:\n%s", got)
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
// （如 func (e UserEntity) Validate() error）与指针接收者同样归位到对应结构体生成代码之后，
// 而非落入文件末尾的"其他用户代码"。
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
	// 布局顺序：Entity 生成代码 < Entity 值接收者方法 < DO 生成代码 < DO 值接收者方法 < 其他函数
	order := []string{
		"type UserEntity struct {",
		"func (e UserEntity) Validate() error {",
		"type UserDO struct {",
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
			t.Errorf("布局顺序错误: %s 位置不正确\n输出:\n%s", s, got)
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
	if strings.Count(got, "type UserEntity struct {") != 1 {
		t.Errorf("生成类型应只保留一份:\n%s", got)
	}
	// 生成文件必须可解析
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("生成文件解析失败: %v\n%s", err, got)
	}
}

// TestWriteOrReplaceStruct_PkgNameFallback 覆盖包名推导回退分支：
// 相对路径（无目录部分）的 Dir 为 "."，包名回退为 "main"。
// 用 t.Chdir 把工作目录切到临时目录，既保持"无目录部分"的相对路径语义，
// 又避免在仓库目录下生成文件（残留与并行 go test 竞争）。
func TestWriteOrReplaceStruct_PkgNameFallback(t *testing.T) {
	t.Chdir(t.TempDir())
	const filePath = "zcmodel_fallback_probe_output.go"

	entityCode := "type FallBackEntity struct {\n\tID int\n}"
	doCode := "type FallBackDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "FallBackEntity", entityCode, "FallBackDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.HasPrefix(string(content), fileHeaderPrefix+"package main\n") {
		t.Fatalf("无目录部分的相对路径应回退为 package main，实际:\n%s", content)
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
// import 声明自身的 Doc 经 mergeMissingImports 的 first.Doc 分支保留，并展开为块形式。
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
// GenDecl 自身 Doc 注释保留（removeGeneratedSpecs 的 d.Doc 分支），
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
// 与 mergeMissingImports 的命名导入（Name 非空）分支：块内带文档注释的别名导入
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
// 按白名单顺序以大写形态输出；DEFAULT_GENERATED 是噪音、其余标记（如 NDB 的 STORAGE DISK）不在白名单，一律过滤。
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

// TestBuildIndexCommentBlock_Format 验证渲染格式：主键行不显示方言内部物理索引名（PG 的 xxx_pkey、
// SQLite 的 sqlite_autoindex_* 即使出现在传入的 Name 中也不被渲染）、多列按定义顺序以逗号加空格连接、
// 非主键索引名原样呈现、表达式列占位符 #expr 透传；索引名与列文本中的换行净化为空格，防止注释行断裂。
// 排序键为净化前的原始索引名（真实索引名不含换行，此处 "idx\nnewline" 因 '\n' 的字节序小于 '_' 而排在前面）。
func TestBuildIndexCommentBlock_Format(t *testing.T) {
	got := buildIndexCommentBlock([]IndexInfo{
		{Name: "user_order_pkey", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "uk_multi", Columns: []string{"user_id", "status"}, Unique: true},
		{Name: "idx_expr", Columns: []string{"#expr"}},
		{Name: "idx\nnewline", Columns: []string{"a\nb"}},
	})
	want := []string{
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_multi (user_id, status)",
		"//   - KEY idx newline (a b)",
		"//   - KEY idx_expr (#expr)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("索引块渲染错误\ngot:  %q\nwant: %q", got, want)
	}
	assertNotContains(t, strings.Join(got, "\n"), "user_order_pkey", "主键行不应显示方言内部物理索引名")
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
// 无换行的文本原样返回。
func TestSanitizeCommentLine(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"LF", "a\nb", "a b"},
		{"CRLF 只替换为一个空格", "a\r\nb", "a b"},
		{"CR", "a\rb", "a b"},
		{"无换行原样", "正常 文本", "正常 文本"},
		{"空串", "", ""},
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

// ==================== 说明头判定与补写（hasFileHeaderComment / writeOrReplaceStruct） ====================

// TestHasFileHeaderComment 验证说明头判定按整行精确匹配：头部含首行即命中（兼容 CRLF 行尾），
// 用户改写说明块其余行但首行仍在时不重复补写；首行被改动则不命中。
func TestHasFileHeaderComment(t *testing.T) {
	firstLine := strings.SplitN(fileHeaderComment, "\n", 2)[0]
	cases := []struct {
		name   string
		header string
		want   bool
	}{
		{"空头部", "", false},
		{"仅有用户文件级注释", "// 用户注释\n\n", false},
		{"说明头整块在头部", fileHeaderComment + "\n\n", true},
		{"CRLF 行尾仍命中", firstLine + "\r\n\r\n", true},
		{"用户改写说明块其余行但首行在", firstLine + "\n// 用户改写的第二行\n", true},
		{"首行被用户改动则不命中", "// 本文件由 zcmodel 生成：用户改写了首行\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasFileHeaderComment(tc.header); got != tc.want {
				t.Errorf("hasFileHeaderComment(%q) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

// TestWriteOrReplaceStruct_HeaderIdempotentOnRegenerate 验证说明头补写的幂等性：
// 连续多次再生成后说明头首行只出现一次（不叠加），且用户自定义方法完整保留。
func TestWriteOrReplaceStruct_HeaderIdempotentOnRegenerate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	entityCode := "type UserEntity struct {\n\tID int64\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("首次生成失败: %v", err)
	}
	first, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	assertContains(t, string(first), fileHeaderComment, "新建文件应带说明头")

	// 追加用户代码后再次生成（走存量路径的说明头判定）
	withUser := append(first, []byte("\n// 用户自定义方法\nfunc (e *UserEntity) Hello() string { return \"hi\" }\n")...)
	if err := os.WriteFile(filePath, withUser, 0644); err != nil {
		t.Fatalf("写入用户代码失败: %v", err)
	}
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("再生成失败: %v", err)
	}
	second, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	firstLine := strings.SplitN(fileHeaderComment, "\n", 2)[0]
	if n := strings.Count(string(second), firstLine); n != 1 {
		t.Errorf("再生成后说明头首行应只出现一次，实际 %d 次:\n%s", n, second)
	}
	if !strings.HasPrefix(string(second), fileHeaderPrefix) {
		t.Errorf("存量文件产物应以说明头起始:\n%s", second)
	}
	assertContains(t, string(second), "func (e *UserEntity) Hello() string", "用户自定义方法应保留")
}

// TestWriteOrReplaceStruct_HeaderScopeLimitedToFileHead 验证说明头判定范围限定于 package 声明之前的头部：
// 说明块文本仅出现在文件中段（用户代码里）时，仍应在文件最顶补写说明头（不误判为已存在）。
func TestWriteOrReplaceStruct_HeaderScopeLimitedToFileHead(t *testing.T) {
	orig := "package model\n\nvar userText = 1\n\n// 本文件由 zcmodel 部分生成：Entity/DO 结构体及 ToDO/ToEntity 方法在再次调用\nvar headFirstLineInBody = 2\n"
	got := writeAndVerify(t, orig, "type UserEntity struct {\n\tID int\n}", "type UserDO struct {\n\tID any\n}", nil)
	if !strings.HasPrefix(got, fileHeaderPrefix+"package model\n") {
		t.Errorf("文件中段出现说明块文本时仍应在最顶补写说明头:\n%s", got)
	}
	assertContains(t, got, "var headFirstLineInBody = 2", "中段的用户代码应原样保留")
}

// TestWriteOrReplaceStruct_HeaderSeparatedFromPackageDoc 验证说明头与 package 文档注释之间以空行分隔
// （否则说明头会被 godoc 当作 package doc 的一部分），package 文档注释仍紧邻 package 行。
func TestWriteOrReplaceStruct_HeaderSeparatedFromPackageDoc(t *testing.T) {
	orig := "// Package model 包文档注释。\npackage model\n\ntype UserEntity struct {\n\tID int\n}\n"
	got := writeAndVerify(t, orig, "type UserEntity struct {\n\tID int64\n}", "type UserDO struct {\n\tID any\n}", nil)
	wantPrefix := fileHeaderPrefix + "// Package model 包文档注释。\npackage model\n"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Errorf("说明头应与 package 文档注释以空行分隔且 package doc 紧邻 package 行\nwant prefix:\n%s\ngot:\n%s", wantPrefix, got)
	}
}
