package zcmodel

import (
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

// TestGenerate_AutoFillTimeImport 验证 Generate 遇到 time.Time 类型且调用者未指定 Import 时，自动填充为 time
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
	// datetime 列被映射为 time.Time，Import 应被自动填充
	if got := input.Columns[1].StructFieldInfo.Import; got != "time" {
		t.Errorf("time.Time 类型的 Import 应自动填充为 time，实际为 %q", got)
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
	// 自定义 Import 显式指定后不会被 Generate 覆盖
	if got := input.Columns[1].StructFieldInfo.Import; got != "github.com/shopspring/decimal" {
		t.Errorf("调用者指定的 Import 被覆盖，实际为 %q", got)
	}
}
