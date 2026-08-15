package zcmodel

import (
	"go/format"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	// 输出目录名需为合法 Go 包名（writeOrReplaceStruct 以目录名推导包名）
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint(20)", Comment: "主键"},
			{Name: "user_name", Type: "VARCHAR(255)", Comment: "用户名"},
			{Name: "created_at", Type: "datetime"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	want := []string{
		"package model",
		"import \"time\"", // datetime 列映射为 time.Time，必须自动引入 time 包
		"// UserInfoEntity test_db.user_info 表，entity结构体，常用于数据库读取操作。",
		"type UserInfoEntity struct {",
		"`json:\"id\" db:\"id\" description:\"主键\"`",
		"`json:\"userName\" db:\"user_name\" description:\"用户名\"`",
		"`json:\"createdAt\" db:\"created_at\"`",
		"type UserInfoDO struct {",
		"func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {",
		"func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {",
		"d.ID = e.ID",
		"d.UserName = e.UserName",
		"if v, ok := d.ID.(int64); ok {",
		"if v, ok := d.UserName.(string); ok {",
	}
	for _, s := range want {
		if !strings.Contains(got, s) {
			t.Errorf("生成内容缺少: %s", s)
		}
	}
	// 字段行按 gofmt 风格对齐，用正则匹配字段名、类型、tag 的组合
	wantRegex := []*regexp.Regexp{
		regexp.MustCompile(`ID\s+int64\s+` + "`json:\"id\" db:\"id\" description:\"主键\"`"),
		regexp.MustCompile(`UserName\s+string\s+` + "`json:\"userName\" db:\"user_name\" description:\"用户名\"`"),
		regexp.MustCompile(`CreatedAt\s+time\.Time\s+` + "`json:\"createdAt\" db:\"created_at\"`"),
		regexp.MustCompile(`ID\s+any\s+` + "`json:\"id\" db:\"id\" description:\"主键\"`"),
		regexp.MustCompile(`UserName\s+any\s+` + "`json:\"userName\" db:\"user_name\" description:\"用户名\"`"),
	}
	for _, re := range wantRegex {
		if !re.MatchString(got) {
			t.Errorf("生成内容未匹配: %v", re)
		}
	}
}

func TestGenerate_InvalidJsonTagValueCase(t *testing.T) {
	input := Input{
		OutputDir:        t.TempDir(),
		Dialect:          DialectMysql,
		TableName:        "user_info",
		JsonTagValueCase: NameCase("invalidCase"),
	}
	if err := Generate(input); err == nil {
		t.Errorf("Generate() 期望返回错误，实际为 nil")
	}
}

func TestGenerate_UnknownDialect(t *testing.T) {
	input := Input{
		OutputDir: t.TempDir(),
		Dialect:   Dialect("oracle"),
		TableName: "user_info",
	}
	if err := Generate(input); err == nil {
		t.Errorf("Generate() 期望返回错误，实际为 nil")
	}
}

// TestGenerate_KeepUserCode 验证文件已存在时重新生成会保留用户自定义代码
func TestGenerate_KeepUserCode(t *testing.T) {
	// 输出目录名需为合法 Go 包名（writeOrReplaceStruct 以目录名推导包名）
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint(20)"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("第一次 Generate() error = %v", err)
	}

	// 在生成的文件中追加用户自定义代码
	filePath := filepath.Join(dir, "user_info.go")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("打开文件失败: %v", err)
	}
	if _, err := f.WriteString("\n// 用户自定义方法\nfunc (e *UserInfoEntity) CustomMethod() string {\n\treturn \"custom\"\n}\n"); err != nil {
		t.Fatalf("追加用户代码失败: %v", err)
	}
	_ = f.Close()

	// 再次生成（新增一个字段）
	input.Columns = append(input.Columns, &Column{Name: "extra", Type: "text"})
	if err := Generate(input); err != nil {
		t.Fatalf("第二次 Generate() error = %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "func (e *UserInfoEntity) CustomMethod() string {") {
		t.Errorf("重新生成后用户自定义代码未被保留")
	}
	if !regexp.MustCompile(`Extra\s+string\s+` + "`json:\"extra\" db:\"extra\"`").MatchString(got) {
		t.Errorf("重新生成后缺少新增字段 Extra")
	}
}

// TestNeededImportsOf 验证按列 StructFieldInfo.Import 收集所需 import：去重、排序、空值不收集
func TestNeededImportsOf(t *testing.T) {
	tests := []struct {
		name    string
		columns []*Column
		want    []string
	}{
		{
			name:    "无列",
			columns: nil,
			want:    nil,
		},
		{
			name: "Import 为空不收集",
			columns: []*Column{
				{StructFieldInfo: StructFieldInfo{Type: "int64"}},
				{StructFieldInfo: StructFieldInfo{Type: "string"}},
				{StructFieldInfo: StructFieldInfo{Type: "[]byte"}},
			},
			want: nil,
		},
		{
			name: "收集指定的 Import",
			columns: []*Column{
				{StructFieldInfo: StructFieldInfo{Type: "time.Time", Import: "time"}},
				{StructFieldInfo: StructFieldInfo{Type: "int"}},
			},
			want: []string{"time"},
		},
		{
			name: "相同 Import 去重",
			columns: []*Column{
				{StructFieldInfo: StructFieldInfo{Type: "time.Time", Import: "time"}},
				{StructFieldInfo: StructFieldInfo{Type: "time.Time", Import: "time"}},
			},
			want: []string{"time"},
		},
		{
			name: "不同 Import 按字典序排序",
			columns: []*Column{
				{StructFieldInfo: StructFieldInfo{Type: "decimal.Decimal", Import: "github.com/shopspring/decimal"}},
				{StructFieldInfo: StructFieldInfo{Type: "time.Time", Import: "time"}},
			},
			want: []string{"github.com/shopspring/decimal", "time"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := neededImportsOf(tt.columns)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("neededImportsOf() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGenerate_AutoFillTimeImport 验证 Generate 遇到 time.Time 类型且调用者未指定 Import 时，
// 自动补全只作用于内部副本：生成文件引入 time 包，调用方传入的 Column 不被原地修改。
func TestGenerate_AutoFillTimeImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint(20)"},
			{Name: "created_at", Type: "datetime"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// 调用方的 Column 保持原样（datetime 的 Import 补全只发生在内部副本上）
	if got := input.Columns[1].StructFieldInfo; got != (StructFieldInfo{}) {
		t.Errorf("调用方的 StructFieldInfo 被原地修改: %+v", got)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.Contains(string(content), `import "time"`) {
		t.Errorf("生成文件缺少 import \"time\":\n%s", content)
	}
}

// TestGenerate_CustomTypeImport 验证调用者自定义字段类型并指定 Import 时，生成代码完整引入对应包
func TestGenerate_CustomTypeImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectPostgres,
		TableName:        "order_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint"},
			// 调用者自定义类型：numeric 列用精确的 decimal.Decimal，并显式声明 import 路径
			{Name: "amount", Type: "numeric(10,2)", StructFieldInfo: StructFieldInfo{
				Type:         "decimal.Decimal",
				Import:       "github.com/shopspring/decimal",
				JsonTagValue: "amount",
			}},
			{Name: "created_at", Type: "timestamptz"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "order_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	// 自定义 Import 与自动填充的 time 组成 import 块（多包时不是单行 import "time"）
	if !strings.Contains(got, `"github.com/shopspring/decimal"`) {
		t.Errorf("生成文件缺少自定义 import github.com/shopspring/decimal:\n%s", got)
	}
	if !strings.Contains(got, "\t\"time\"\n") {
		t.Errorf("生成文件缺少 import \"time\":\n%s", got)
	}
	if !regexp.MustCompile(`Amount\s+decimal\.Decimal\s+` + "`json:\"amount\" db:\"amount\"`").MatchString(got) {
		t.Errorf("自定义类型字段生成错误:\n%s", got)
	}
	// 自定义 Import 显式指定后不会被 Generate 覆盖，调用方数据整体不被原地修改
	if got := input.Columns[1].StructFieldInfo; got != (StructFieldInfo{
		Type:         "decimal.Decimal",
		Import:       "github.com/shopspring/decimal",
		JsonTagValue: "amount",
	}) {
		t.Errorf("调用方显式指定的 StructFieldInfo 被修改，实际为 %+v", got)
	}
}

// TestGenerate_TableNameEscape 验证 TableName 含路径穿越/非法字符时拒绝生成，且输出目录外无文件逃逸。
func TestGenerate_TableNameEscape(t *testing.T) {
	base := t.TempDir()
	escapeDir := filepath.Join(base, "escape_target")
	if err := os.MkdirAll(escapeDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	tests := []struct {
		name      string
		tableName string
	}{
		{"相对路径穿越", "../escaped"},
		{"绝对路径", filepath.Join(base, "escaped_abs")},
		{"Windows非法字符冒号", "user:info"},
		{"Windows非法字符星号", "user*info"},
		{"点", "."},
		{"双点", ".."},
		{"空表名", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := snapshotFiles(t, base)
			input := Input{
				OutputDir: escapeDir,
				Database:  "test_db",
				Dialect:   DialectMysql,
				TableName: tt.tableName,
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			}
			err := Generate(input)
			if err == nil {
				t.Fatalf("TableName=%q 期望返回错误，实际为 nil", tt.tableName)
			}
			// 输出目录之外不得出现新文件
			after := snapshotFiles(t, base)
			if !reflect.DeepEqual(after, before) {
				t.Errorf("TableName=%q 生成产生了文件逃逸\nbefore: %v\nafter:  %v", tt.tableName, before, after)
			}
		})
	}
}

// snapshotFiles 递归收集 root 下所有文件的路径集合，用于检测生成文件逃逸。
func snapshotFiles(t *testing.T, root string) map[string]bool {
	t.Helper()
	files := make(map[string]bool)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files[path] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历目录失败: %v", err)
	}
	return files
}

// TestGenerate_InvalidColumnNames 验证数字开头/纯数字列名转换出非法 Go 标识符时，
// Generate 报错且不写出非法文件（go/parser 自校验兜底）。
func TestGenerate_InvalidColumnNames(t *testing.T) {
	tests := []struct {
		name    string
		colName string
	}{
		{"数字开头", "1st_place"},
		{"纯数字", "123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			input := Input{
				OutputDir:     dir,
				Database:      "test_db",
				Dialect:       DialectMysql,
				TableName:     "user_info",
				ColumnTagName: "db",
				Columns:       []*Column{{Name: tt.colName, Type: "text"}},
			}
			err := Generate(input)
			if err == nil {
				t.Fatalf("列名 %q 生成非法标识符应报错，实际为 nil", tt.colName)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
				t.Errorf("生成失败时不应写出文件: %v", statErr)
			}
		})
	}
}

// TestGenerate_ChineseColumnName 验证中文列名生成合法 Go 标识符，且产物与 gofmt 标准输出逐字节一致
// （对齐按 rune 宽度，见 Minor-2）。
func TestGenerate_ChineseColumnName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint"},
			{Name: "用户表", Type: "varchar(255)", Comment: "中文列名注释"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("中文列名应生成合法代码，Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	// 中文是合法 Go 标识符，字段名保留原样
	if !regexp.MustCompile(`用户表\s+string\s+`).Match(content) {
		t.Errorf("中文列名字段生成错误:\n%s", content)
	}
	// 产物必须与 gofmt 标准输出逐字节一致
	formatted, err := format.Source(content)
	if err != nil {
		t.Fatalf("生成产物无法通过 gofmt: %v", err)
	}
	if string(formatted) != string(content) {
		t.Errorf("生成产物与 gofmt 输出不一致\ngot:\n%s\nwant:\n%s", content, formatted)
	}
}

// TestGenerate_DuplicateFieldNames 验证不同列转换后字段名重复时报错（如 user_name 与 userName 均转换为 UserName）。
func TestGenerate_DuplicateFieldNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns: []*Column{
			{Name: "user_name", Type: "varchar(255)"},
			{Name: "userName", Type: "varchar(255)"},
		},
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("字段名重复应报错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "重复") {
		t.Errorf("错误信息应说明字段名重复: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
		t.Errorf("生成失败时不应写出文件: %v", statErr)
	}
}

// TestGenerate_NoInputSideEffect 验证 Generate 不原地修改调用方的 Input：
// 字段名/类型/tag/Import 的补全只作用于内部副本，结果仅体现在生成文件中。
func TestGenerate_NoInputSideEffect(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		ColumnTagName:    "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint(20)"},
			{Name: "created_at", Type: "datetime"},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// 调用方数据保持原样：StructFieldInfo 完全未被补全
	for _, col := range input.Columns {
		if col.StructFieldInfo != (StructFieldInfo{}) {
			t.Errorf("列 %q 的 StructFieldInfo 被原地修改: %+v", col.Name, col.StructFieldInfo)
		}
	}
	// 补全结果只体现在生成文件中
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	for _, s := range []string{"ID", "CreatedAt", "time.Time", `import "time"`} {
		if !strings.Contains(string(content), s) {
			t.Errorf("生成文件缺少补全后的内容 %q:\n%s", s, content)
		}
	}
}

// TestGenerate_KeepBuildTags 验证对含 build tags 的已有文件再生成时，文件头指令与 package 注释完整保留。
func TestGenerate_KeepBuildTags(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")
	orig := "//go:build ignore\n// +build ignore\n\n// Package model 包文档注释。\npackage model\n\nimport \"fmt\"\n\nvar Extra = 1\n"
	if err := os.WriteFile(filePath, []byte(orig), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       []*Column{{Name: "id", Type: "bigint(20)"}},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	wantHeader := "//go:build ignore\n// +build ignore\n\n// Package model 包文档注释。\npackage model\n"
	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("文件头 build tags/package 注释未保留\nwant prefix:\n%s\ngot:\n%s", wantHeader, got)
	}
	if !strings.Contains(got, "var Extra = 1") {
		t.Errorf("用户代码丢失:\n%s", got)
	}
	if !strings.Contains(got, "type UserInfoEntity struct {") {
		t.Errorf("生成代码缺失:\n%s", got)
	}
}
