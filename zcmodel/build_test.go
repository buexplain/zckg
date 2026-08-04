package zcmodel

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestWriteOrReplaceStruct_NewFile 验证文件不存在时创建新文件
func TestWriteOrReplaceStruct_NewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user.go")
	entityCode := "type UserEntity struct {\n\tID int\n}"
	doCode := "type UserDO struct {\n\tID any\n}"
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode); err != nil {
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
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode); err != nil {
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
	if err := writeOrReplaceStruct(filePath, "UserEntity", entityCode, "UserDO", doCode); err != nil {
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
	// 新生成代码生效、旧字段被移除
	if !strings.Contains(got, "Age  int") {
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
