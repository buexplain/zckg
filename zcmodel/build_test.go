package zcmodel

import (
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
	got := buildStruct("UserInfoEntity", testColumns(), false, "UserInfoEntity 用户表", "db")
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
	got := buildStruct("UserInfoDO", testColumns(), true, "", "db")
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
	got := buildStruct("UserInfoEntity", testColumns(), false, "", "column")
	if !strings.Contains(got, "`json:\"id\" column:\"id\" description:\"主键\"`") {
		t.Errorf("buildStruct 自定义 tag 名称输出错误:\n%s", got)
	}
}

// TestBuildStruct_EmptyComment 验证无注释字段不生成 description tag
func TestBuildStruct_EmptyComment(t *testing.T) {
	cols := []Column{
		{Name: "id", StructFieldInfo: StructFieldInfo{Name: "ID", Type: "int64"}},
	}
	got := buildStruct("Entity", cols, false, "", "db")
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
	got := buildStruct("TEntity", cols, false, "", "db")
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

// TestWriteOrReplaceStruct_NewFile 验证文件不存在时创建新文件
func TestWriteOrReplaceStruct_NewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	want := "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
	if string(content) != want {
		t.Errorf("新建文件内容错误\nwant:\n%s\ngot:\n%s", want, content)
	}
}

// TestWriteOrReplaceStruct_EmptyFile 验证空文件按新建处理
func TestWriteOrReplaceStruct_EmptyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	if err := os.WriteFile(filePath, []byte("  \n"), 0644); err != nil {
		t.Fatalf("创建空文件失败: %v", err)
	}
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	want := "package model\n\n" + entityCode + "\n\n" + doCode + "\n"
	if string(content) != want {
		t.Errorf("空文件重建内容错误\nwant:\n%s\ngot:\n%s", want, content)
	}
}

// TestWriteOrReplaceStruct_KeepUserCode 验证重新生成时移除旧生成代码、保留用户代码与 import
func TestWriteOrReplaceStruct_KeepUserCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	// 第二次生成：结构体字段变化（移除 Name，新增 Age）
	entityCode := "type UserEntity struct {\n\tID   int64\n\tAge  int\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tID   any\n\tAge  any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 用户代码与 import 保留
	for _, s := range []string{
		"import \"fmt\"",
		"func (e *UserEntity) Hello() string {",
		"func (d *UserDO) World() string {",
		"var Extra = 1",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("重新生成后缺少: %s\n输出:\n%s", s, got)
		}
	}
	// 新生成代码生效、旧字段被移除（产物经 gofmt 格式化，字段对齐按 gofmt 标准）
	if !regexp.MustCompile(`Age\s+int\b`).MatchString(got) {
		t.Errorf("重新生成后缺少新字段 Age:\n%s", got)
	}
	if strings.Contains(got, "Name string") || strings.Contains(got, "Name any") {
		t.Errorf("重新生成后旧字段 Name 未被移除:\n%s", got)
	}
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
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)
	for _, s := range []string{"type (", "Status int", "Level  int", "// 用户自定义类型块"} {
		if !strings.Contains(got, s) {
			t.Errorf("用户类型块应完整保留，缺少: %s\n输出:\n%s", s, got)
		}
	}
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
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)
	// import "time" 自动引入，且位于 package 与生成代码之间
	if !strings.Contains(got, "import \"time\"") {
		t.Errorf("需要 time 包时新建文件应自动引入 import \"time\":\n%s", got)
	}
	if !strings.HasPrefix(got, "package model\n\nimport \"time\"\n\ntype UserEntity struct {") {
		t.Errorf("import 位置错误:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_ExistingFile_AutoAddTimeImport 验证已存在文件缺少 time import 时自动补上，且保留用户 import
func TestWriteOrReplaceStruct_ExistingFile_AutoAddTimeImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)
	// 缺失的 time 合并进原 import 块（单一 import 块，避免形成两个 import 块）
	if !strings.Contains(got, "import (\n\t\"fmt\"\n\t\"time\"\n)") {
		t.Errorf("缺失的 time import 应合并进原 import 块:\n%s", got)
	}
	if !strings.Contains(got, "func (e *UserEntity) Hello() string {") {
		t.Errorf("用户自定义方法应保留:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_ExistingFile_TimeImportExists 验证文件已导入 time 时不重复添加
func TestWriteOrReplaceStruct_ExistingFile_TimeImportExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	orig := `package model

import "time"

type UserEntity struct {
	ID int
}
`
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)
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
			dir := filepath.Join(t.TempDir(), "model")
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("创建目录失败: %v", err)
			}
			filePath := filepath.Join(dir, "user.go")
			if err := os.WriteFile(filePath, []byte(tt.orig), 0644); err != nil {
				t.Fatalf("写入初始文件失败: %v", err)
			}
			entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}\n\nfunc (e *UserEntity) ToDO() { _ = e }"
			doCode := "type UserDO struct {\n\tCreatedAt any\n}\n\nfunc (d *UserDO) ToEntity() { _ = d }"
			if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, []string{"time"}); err != nil {
				t.Fatalf("writeOrReplaceStruct() error = %v", err)
			}
			content, err := os.ReadFile(filePath)
			if err != nil {
				t.Fatalf("读取文件失败: %v", err)
			}
			got := string(content)
			// 别名/空白导入不满足生成代码对默认包名的引用，必须补充标准导入（合并进同一 import 块）
			if !strings.Contains(got, "\t\"time\"\n") {
				t.Errorf("存量文件为%s时不应视为已导入，应补充标准导入 \"time\":\n%s", tt.name, got)
			}
			// 用户原有的别名/空白导入完整保留（与标准导入合法共存于同一 import 块）
			if !strings.Contains(got, tt.keepImport) {
				t.Errorf("用户原有导入 %s 应保留:\n%s", tt.keepImport, got)
			}
			// 缺失导入合并进原 import 块，不形成两个 import 块
			if strings.Count(got, "import (") != 1 {
				t.Errorf("应只有一个 import 块:\n%s", got)
			}
			if !strings.Contains(got, "type UserEntity struct {") {
				t.Errorf("生成代码缺失:\n%s", got)
			}
		})
	}
}

// TestWriteOrReplaceStruct_MultipleNeededImports 验证需要多个包时生成 import (…) 块，且缺失的包逐个补齐
func TestWriteOrReplaceStruct_MultipleNeededImports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	entityCode := "type UserEntity struct {\n\tCreatedAt time.Time\n}"
	doCode := "type UserDO struct {\n\tCreatedAt any\n}"
	needed := []string{"time", "github.com/foo/bar"}

	// 新建文件：多个 import 组装为排序后的 import (…) 块
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, needed); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)
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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, needed); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}
	content, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got = string(content)
	// 缺失的包合并进原 import 块（"time" 已存在不重复，仅补 "github.com/foo/bar"；
	// 最终经 gofmt 排序，"github.com/foo/bar" 排在前）
	if !strings.Contains(got, "import (\n\t\"github.com/foo/bar\"\n\t\"time\"\n)") {
		t.Errorf("缺失的 import 应合并进原 import 块:\n%s", got)
	}
	if strings.Count(got, `"time"`) != 1 {
		t.Errorf("已存在的 import 不应重复添加:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_KeepBuildTagsAndPackageComment 验证再生成保留文件头 build tags、
// package 文档注释与原 package 行（逐字节），且已有文件尊重原包名而非输出目录推导名。
func TestWriteOrReplaceStruct_KeepBuildTagsAndPackageComment(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID   int64\n\tAge  int\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID  any\n\tAge any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 文件头（build tags + package 注释 + 原 package 行）逐字节保留
	wantHeader := "//go:build ignore\n// +build ignore\n\n// Package custompkg 包文档注释。\npackage custompkg\n"
	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("文件头 build tags/package 注释未逐字节保留\nwant prefix:\n%s\ngot:\n%s", wantHeader, got)
	}
	// 包名尊重原文件，不被输出目录推导名 model 覆盖
	if strings.Contains(got, "\npackage model\n") {
		t.Errorf("已有文件的包名不应被改为输出目录推导名:\n%s", got)
	}
	// 用户代码保留
	if !strings.Contains(got, "var Extra = 1") {
		t.Errorf("用户代码丢失:\n%s", got)
	}
	// 生成代码各只保留一份
	if strings.Count(got, "type UserEntity struct {") != 1 || strings.Count(got, "type UserDO struct {") != 1 {
		t.Errorf("重新生成后生成代码出现多次:\n%s", got)
	}
}

// TestWriteOrReplaceStruct_KeepUserTypeInMixedBlock 验证 type 块中混有用户类型时，
// 再生成仅剔除生成的类型（按 Spec 粒度），用户类型及其注释完整保留。
func TestWriteOrReplaceStruct_KeepUserTypeInMixedBlock(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 混合 type 块中的用户类型（含注释）与用户函数保留
	for _, s := range []string{
		"// MyHelper 用户手写类型",
		"MyHelper struct {",
		"func MyFunc() {",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("混合 type 块中的用户代码丢失: %s\n输出:\n%s", s, got)
		}
	}
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
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 值接收者 ToDO 被移除，仅保留生成的指针接收者版本
	if strings.Contains(got, "func (e UserEntity) ToDO()") {
		t.Errorf("值接收者 ToDO 未被移除:\n%s", got)
	}
	if strings.Count(got, "ToDO()") != 1 {
		t.Errorf("ToDO 方法应只保留一份:\n%s", got)
	}
	if !strings.Contains(got, "func (e *UserEntity) ToDO()") {
		t.Errorf("生成的指针接收者 ToDO 丢失:\n%s", got)
	}
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
	got := buildStruct("UserInfoEntity", cols, false, "", "")
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
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 值接收者自定义方法与无接收者函数均保留
	for _, s := range []string{
		"func (e UserEntity) Validate() error {",
		"func (d UserDO) Tag() string {",
		"func FreeFunc() {",
	} {
		if !strings.Contains(got, s) {
			t.Errorf("值接收者自定义方法应保留，缺少: %s\n输出:\n%s", s, got)
		}
	}
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
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")

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
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}

	entityCode := "type UserEntity struct {\n\tID int64\n}\n\nfunc (e *UserEntity) ToDO() {}"
	doCode := "type UserDO struct {\n\tID any\n}\n\nfunc (d *UserDO) ToEntity() {}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode, nil); err != nil {
		t.Fatalf("writeOrReplaceStruct() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	got := string(content)

	// 游离注释与用户类型均保留
	if !strings.Contains(got, "// 游离注释：不与任何类型绑定") {
		t.Errorf("混合 type 块中的游离注释应保留:\n%s", got)
	}
	if !strings.Contains(got, "MyHelper struct {") {
		t.Errorf("混合 type 块中的用户类型应保留:\n%s", got)
	}
	if strings.Count(got, "type UserEntity struct {") != 1 {
		t.Errorf("生成类型应只保留一份:\n%s", got)
	}
	// 生成文件必须可解析
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "", got, parser.AllErrors); err != nil {
		t.Fatalf("生成文件解析失败: %v\n%s", err, got)
	}
}
