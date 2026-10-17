package zcmodel

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestGenerate 验证 Generate 主流程产物：包名、自动补全的 time import、
// Entity/DO 结构体注释与声明、字段行（字段名 + 类型 + 完整 tag）、
// 以及 ToDO/ToEntity 方法签名与其中的字段赋值/类型断言语句。
func TestGenerate(t *testing.T) {
	// 输出目录名需为合法 Go 包名（writeOrReplaceStruct 以目录名推导包名）
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Database:         "test_db",
		Dialect:          DialectMysql,
		TableName:        "user_info",
		TableComment:     "用户信息表",
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
		// TableComment 非空时拼入结构体注释（空时回退“表”）
		"// UserInfoEntity test_db.user_info 用户信息表，entity结构体，常用于数据库读取操作。",
		"type UserInfoEntity struct {",
		"`json:\"id\" db:\"id\" ddl:\"bigint(20)\" description:\"主键\"`",
		"`json:\"userName\" db:\"user_name\" ddl:\"VARCHAR(255)\" description:\"用户名\"`",
		"`json:\"createdAt\" db:\"created_at\" ddl:\"datetime\"`",
		"type UserInfoDO struct {",
		"func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {",
		"func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {",
		"d.ID = e.ID",
		"d.UserName = e.UserName",
		"if v, ok := d.ID.(int64); ok {",
		"if v, ok := d.UserName.(string); ok {",
	}
	assertContainsAll(t, got, want, "Generate 产物")
	// 字段行按 gofmt 风格对齐，用正则匹配字段名、类型、tag 的组合（tag 顺序 json → db → ddl → description，
	// Nullable 为 nil 时 ddl 片段不含 NULL 标记）。
	// tag 字面量经 regexp.QuoteMeta 转义：ddl 片段含括号/引号等正则元字符（如 bigint(20)），不转义会被当作捕获组。
	wantRegex := []*regexp.Regexp{
		regexp.MustCompile(`ID\s+int64\s+` + regexp.QuoteMeta("`json:\"id\" db:\"id\" ddl:\"bigint(20)\" description:\"主键\"`")),
		regexp.MustCompile(`UserName\s+string\s+` + regexp.QuoteMeta("`json:\"userName\" db:\"user_name\" ddl:\"VARCHAR(255)\" description:\"用户名\"`")),
		regexp.MustCompile(`CreatedAt\s+time\.Time\s+` + regexp.QuoteMeta("`json:\"createdAt\" db:\"created_at\" ddl:\"datetime\"`")),
		regexp.MustCompile(`ID\s+any\s+` + regexp.QuoteMeta("`json:\"id\" db:\"id\" ddl:\"bigint(20)\" description:\"主键\"`")),
		regexp.MustCompile(`UserName\s+any\s+` + regexp.QuoteMeta("`json:\"userName\" db:\"user_name\" ddl:\"VARCHAR(255)\" description:\"用户名\"`")),
	}
	for _, re := range wantRegex {
		if !re.MatchString(got) {
			t.Errorf("生成内容未匹配: %v", re)
		}
	}
}

// TestGenerate_JsonTagValueWithSpecialChars 验证 formatJSONTag 入口（列名含双引号、显式指定合法字段名）
// 产出的 json tag 经净化后生成文件可解析、反射可完整还原。
func TestGenerate_JsonTagValueWithSpecialChars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:        dir,
		Dialect:          DialectMysql,
		TableName:        "user_info",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "na\"me", Type: "varchar(255)", StructFieldInfo: StructFieldInfo{Name: "Name"}},
		},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "user_info.go", string(content), parser.AllErrors)
	if err != nil {
		t.Fatalf("生成文件存在语法错误: %v\n%s", err, content)
	}
	tag := fieldTagOf(file, "Name")
	if tag == "" {
		t.Fatalf("未提取到字段 tag:\n%s", content)
	}
	if gotJSON := reflect.StructTag(tag).Get("json"); gotJSON != "na\"me" {
		t.Errorf("json tag 还原失败\ngot:  %q\nwant: %q", gotJSON, "na\"me")
	}
}

// TestGenerate_ExplicitJsonTagWithEmptyCase 锁定「显式优先」：全局 JsonTagValueCase 为空
// （不自动生成 json tag）时，单列显式指定的 StructFieldInfo.JsonTagValue 仍会生成 json tag；
// 未显式指定的列则完全不生成 json tag。
func TestGenerate_ExplicitJsonTagWithEmptyCase(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := Generate(Input{
		OutputDir: dir,
		Dialect:   DialectMysql,
		TableName: "user_info",
		Columns: []*Column{
			{Name: "id", Type: "bigint", StructFieldInfo: StructFieldInfo{JsonTagValue: "explicit"}},
			{Name: "name", Type: "varchar(64)"},
		},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	assertContains(t, got, `json:"explicit"`, "全局风格为空时显式 JsonTagValue 仍应生成 json tag")
	assertNotContains(t, got, `json:"name"`, "未显式指定 JsonTagValue 的列不应自动生成 json tag")
}

// TestGenerate_InvalidJsonTagValueCase 验证 JsonTagValueCase 不在六种支持风格之内时的错误路径：
// Generate 必须返回错误，且错误消息说明命名风格不受支持。
func TestGenerate_InvalidJsonTagValueCase(t *testing.T) {
	input := Input{
		OutputDir:        t.TempDir(),
		Dialect:          DialectMysql,
		TableName:        "user_info",
		JsonTagValueCase: NameCase("invalidCase"),
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("Generate() 期望返回错误，实际为 nil")
	}
	if !strings.Contains(err.Error(), "不支持的 json tag 命名风格") {
		t.Errorf("错误信息应说明 json tag 命名风格不合法，实际: %v", err)
	}
}

// TestGenerate_UnknownDialect 验证方言不在 mysql/postgres/sqlite 之内时的错误路径：
// Generate 必须返回错误，且错误消息指明原因与传入的非法方言值。
func TestGenerate_UnknownDialect(t *testing.T) {
	input := Input{
		OutputDir: t.TempDir(),
		Dialect:   Dialect("oracle"),
		TableName: "user_info",
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("Generate() 期望返回错误，实际为 nil")
	}
	// 断言具体错误原因：避免因其他前置校验报错而假通过
	if !strings.Contains(err.Error(), "不支持的数据库方言") {
		t.Errorf("错误信息应说明方言不受支持，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "oracle") {
		t.Errorf("错误信息应包含传入的方言值 oracle，实际: %v", err)
	}
}

// TestGenerate_KeepUserCode 验证文件已存在时重新生成会保留用户自定义代码（含方法体），
// 且生成部分被正确更新：新增列同时进入 Entity 与 DO，各生成声明只保留一份。
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

	// 在生成的文件中追加用户自定义代码。
	// 用闭包收敛句柄生命周期：defer 保证 WriteString 失败时句柄也被释放，
	// 且第二次 Generate 之前文件已关闭（Windows 上 rename 无法覆盖存在打开句柄的文件）。
	filePath := filepath.Join(dir, "user_info.go")
	func() {
		f, err := os.OpenFile(filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			t.Fatalf("打开文件失败: %v", err)
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				t.Errorf("关闭文件失败: %v", cerr)
			}
		}()
		if _, err := f.WriteString("\n// 用户自定义方法\nfunc (e *UserInfoEntity) CustomMethod() string {\n\treturn \"custom\"\n}\n"); err != nil {
			t.Fatalf("追加用户代码失败: %v", err)
		}
	}()

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
	// 方法体也必须完整保留：验证 AST 偏移截取不会截断函数体
	assertContainsAll(t, got, []string{
		"func (e *UserInfoEntity) CustomMethod() string {",
		"return \"custom\"",
	}, "重新生成后用户自定义代码应完整保留")
	// 新增列应同时进入 Entity（具体类型）与 DO（any）
	if !regexp.MustCompile(`Extra\s+string\s+` + "`json:\"extra\" db:\"extra\" ddl:\"text\"`").MatchString(got) {
		t.Errorf("重新生成后 Entity 缺少新增字段 Extra")
	}
	if !regexp.MustCompile(`Extra\s+any\s+` + "`json:\"extra\" db:\"extra\" ddl:\"text\"`").MatchString(got) {
		t.Errorf("重新生成后 DO 缺少新增字段 Extra")
	}
	// 生成声明各只保留一份：重新生成为替换而非追加
	for _, decl := range []string{
		"type UserInfoEntity struct {",
		"type UserInfoDO struct {",
		"func (e *UserInfoEntity) ToDO(",
		"func (d *UserInfoDO) ToEntity(",
	} {
		if n := strings.Count(got, decl); n != 1 {
			t.Errorf("生成声明 %q 应只出现一次，实际 %d 次:\n%s", decl, n, got)
		}
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
	assertContains(t, string(content), `import "time"`, "自动补全 time import")
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
	assertContainsAll(t, got, []string{
		`"github.com/shopspring/decimal"`,
		"\t\"time\"\n",
	}, "自定义 import 与自动补全的 time 应同时出现")
	// tag 字面量经 regexp.QuoteMeta 转义：ddl 片段含括号（numeric(10,2)），不转义会被当作捕获组
	if !regexp.MustCompile(`Amount\s+decimal\.Decimal\s+` + regexp.QuoteMeta("`json:\"amount\" db:\"amount\" ddl:\"numeric(10,2)\"`")).MatchString(got) {
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

// TestGenerate_TableNameEscape 验证 TableName 含路径穿越/非法字符/非法首字符（含中文表名）、
// 或推导出的结构体名不是导出标识符（schema 限定名、含 $、下划线后接数字/中文）时
// 拒绝生成并给出明确错误，且输出目录外无文件逃逸。
func TestGenerate_TableNameEscape(t *testing.T) {
	base := t.TempDir()
	escapeDir := filepath.Join(base, "escape_target")
	if err := os.MkdirAll(escapeDir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	tests := []struct {
		name      string
		tableName string
		wantErr   string
	}{
		{"相对路径穿越", "../escaped", "表名包含非法字符"},
		{"绝对路径", filepath.Join(base, "escaped_abs"), "表名包含非法字符"},
		{"Windows非法字符冒号", "user:info", "表名包含非法字符"},
		{"Windows非法字符星号", "user*info", "表名包含非法字符"},
		{"点", ".", "表名非法"},
		{"双点", "..", "表名非法"},
		{"空表名", "", "表名为空"},
		// 非 ASCII 首字符（中文表名）与数字开头会推导出无法编译的标识符，须前置报明确错误（ZCM-02）
		{"中文表名", "订单", "表名首字符必须为 ASCII 字母或下划线"},
		{"数字开头表名", "2_order", "表名首字符必须为 ASCII 字母或下划线"},
		// 首字符合法但推导出的结构体名不是导出标识符：须前置报错，而非兜底的语法错误或静默产出未导出类型
		{"schema 限定表名", "public.users", "不是合法的导出 Go 标识符"},
		{"含点表名", "user.order", "不是合法的导出 Go 标识符"},
		{"含美元符表名", "user$x", "不是合法的导出 Go 标识符"},
		{"下划线后接数字", "_2fa", "不是合法的导出 Go 标识符"},
		{"下划线后接中文", "_用户", "不是合法的导出 Go 标识符"},
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
			// 错误信息须可直接定位根因，而非兜底的“生成代码存在语法错误”
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("TableName=%q 错误信息应包含 %q，实际: %v", tt.tableName, tt.wantErr, err)
			}
			// 输出目录之外不得出现新文件
			after := snapshotFiles(t, base)
			if !reflect.DeepEqual(after, before) {
				t.Errorf("TableName=%q 生成产生了文件逃逸\nbefore: %v\nafter:  %v", tt.tableName, before, after)
			}
		})
	}
}

// TestGenerate_FileNameConflictWithGoRules 验证文件名（默认取表名，或显式的 Input.FileName）触发 Go 文件名构建规则时报错且不写出任何文件：
// _ 开头被 go 工具忽略、_test 后缀成为测试文件、_GOOS / _GOARCH / _GOOS_GOARCH 后缀成为平台约束。
// 来源为表名时错误须提示通过 Input.FileName 改名；显式 FileName 同样受该规则约束。
func TestGenerate_FileNameConflictWithGoRules(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		fileName  string
		wantErr   []string
	}{
		{"下划线开头表名", "_user", "", []string{"_user.go", "被 go 工具忽略", "Input.FileName"}},
		{"_test 后缀表名", "user_test", "", []string{"user_test.go", "测试文件", "Input.FileName"}},
		{"GOOS 后缀表名", "order_linux", "", []string{"order_linux.go", "平台构建约束", "Input.FileName"}},
		{"本机 GOOS 后缀表名", "log_windows", "", []string{"log_windows.go", "平台构建约束", "Input.FileName"}},
		{"GOARCH 后缀表名", "stat_amd64", "", []string{"stat_amd64.go", "平台构建约束", "Input.FileName"}},
		{"GOOS_GOARCH 后缀表名", "x_linux_arm64", "", []string{"x_linux_arm64.go", "平台构建约束", "Input.FileName"}},
		{"显式文件名下划线开头", "user", "_user", []string{"文件名 _user.go", "被 go 工具忽略"}},
		{"显式文件名 GOOS 后缀", "user", "user_darwin", []string{"文件名 user_darwin.go", "平台构建约束"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			err := Generate(Input{
				OutputDir: dir,
				Dialect:   DialectMysql,
				TableName: tt.tableName,
				FileName:  tt.fileName,
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			})
			if err == nil {
				t.Fatalf("期望返回错误，实际为 nil")
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("错误信息应包含 %q，实际: %v", want, err)
				}
			}
			// 校验先于建目录，失败时输出目录不应被创建
			if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
				t.Errorf("生成失败时不应创建输出目录: %v", statErr)
			}
		})
	}
}

// TestGenerate_FileNameOverride 验证 Input.FileName 覆盖默认文件名：产物写入 {FileName}.go，
// 结构体名仍由表名推导；不触发文件名规则的普通表名（含与 GOOS 同名但无下划线前缀的 linux）照常使用表名。
func TestGenerate_FileNameOverride(t *testing.T) {
	tests := []struct {
		name      string
		tableName string
		fileName  string
		wantFile  string
		wantType  string
	}{
		{"下划线开头表名改名", "_user", "user", "user.go", "type UserEntity struct {"},
		{"GOOS 后缀表名改名", "order_linux", "order_linux_tbl", "order_linux_tbl.go", "type OrderLinuxEntity struct {"},
		{"无下划线前缀的 GOOS 同名表名", "linux", "", "linux.go", "type LinuxEntity struct {"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			if err := Generate(Input{
				OutputDir: dir,
				Dialect:   DialectMysql,
				TableName: tt.tableName,
				FileName:  tt.fileName,
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			}); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("读取输出目录失败: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != tt.wantFile {
				t.Fatalf("输出目录应只有 %s，实际: %v", tt.wantFile, entries)
			}
			content, err := os.ReadFile(filepath.Join(dir, tt.wantFile))
			if err != nil {
				t.Fatalf("读取生成文件失败: %v", err)
			}
			assertContains(t, string(content), tt.wantType, "结构体名应由表名推导")
		})
	}
}

// TestGenerate_InvalidFileName 验证显式 Input.FileName 的路径安全校验：拒绝 "." / ".."、路径分隔符、
// Windows 非法文件名字符与多余的 .go 扩展名，错误信息含具体原因，且输出目录外无文件逃逸。
func TestGenerate_InvalidFileName(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "model")
	tests := []struct {
		name     string
		fileName string
		wantErr  string
	}{
		{"点", ".", "文件名非法"},
		{"双点", "..", "文件名非法"},
		{"路径穿越", "../escaped", "文件名包含非法字符"},
		{"反斜杠", `a\b`, "文件名包含非法字符"},
		{"冒号", "a:b", "文件名包含非法字符"},
		{"带 .go 扩展名", "user.go", "文件名不应包含 .go 扩展名"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := snapshotFiles(t, base)
			err := Generate(Input{
				OutputDir: dir,
				Dialect:   DialectMysql,
				TableName: "user",
				FileName:  tt.fileName,
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			})
			if err == nil {
				t.Fatalf("FileName=%q 期望返回错误，实际为 nil", tt.fileName)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("FileName=%q 错误信息应包含 %q，实际: %v", tt.fileName, tt.wantErr, err)
			}
			if after := snapshotFiles(t, base); !reflect.DeepEqual(after, before) {
				t.Errorf("FileName=%q 生成产生了文件\nbefore: %v\nafter:  %v", tt.fileName, before, after)
			}
		})
	}
}

// TestGenerate_InvalidUTF8Name 验证表名/文件名不是合法 UTF-8 时前置报错且不产生文件：
// Windows 会把非法字节替换为 U+FFFD，若放行会导致落盘文件名与调用方期望路径不一致。
func TestGenerate_InvalidUTF8Name(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name      string
		tableName string
		fileName  string
		wantErr   string
	}{
		{"表名非法 UTF-8", "user\x8e", "", "表名不是合法的 UTF-8"},
		{"显式文件名非法 UTF-8", "user", "user\x8e", "文件名不是合法的 UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := snapshotFiles(t, base)
			err := Generate(Input{
				OutputDir: filepath.Join(base, "model"),
				Dialect:   DialectMysql,
				TableName: tt.tableName,
				FileName:  tt.fileName,
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			})
			if err == nil {
				t.Fatalf("tableName=%q fileName=%q 期望返回错误，实际为 nil", tt.tableName, tt.fileName)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("错误信息应包含 %q，实际: %v", tt.wantErr, err)
			}
			if after := snapshotFiles(t, base); !reflect.DeepEqual(after, before) {
				t.Errorf("非法 UTF-8 名称生成了文件\nbefore: %v\nafter:  %v", before, after)
			}
		})
	}
}

// TestGoFileNameConflict 逐条锁定 goFileNameConflict 的判定：触发规则时返回对应原因，普通文件名返回空串。
// 平台后缀由 go/build 判定，大写的 _LINUX 不是平台约束、首个下划线之前的 GOOS 名（linux_user）也不是。
func TestGoFileNameConflict(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"user", ""},
		{"user_info", ""},
		{"linux", ""},
		{"linux_user", ""},
		{"order_LINUX", ""},
		{"_user", "以 _ 或 . 开头的文件会被 go 工具忽略"},
		{".user", "以 _ 或 . 开头的文件会被 go 工具忽略"},
		{"user_test", "_test 后缀会使其被识别为测试文件"},
		{"user_linux_test", "_test 后缀会使其被识别为测试文件"},
		{"order_linux", "文件名后缀会被识别为 GOOS/GOARCH 平台构建约束"},
		{"order_windows", "文件名后缀会被识别为 GOOS/GOARCH 平台构建约束"},
		{"order_386", "文件名后缀会被识别为 GOOS/GOARCH 平台构建约束"},
		{"order_js_wasm", "文件名后缀会被识别为 GOOS/GOARCH 平台构建约束"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goFileNameConflict(tt.name); got != tt.want {
				t.Errorf("goFileNameConflict(%q) = %q, want %q", tt.name, got, tt.want)
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
// Generate 前置报错（错误信息含列名与标识符上下文）且不写出文件（ZCM-02）。
func TestGenerate_InvalidColumnNames(t *testing.T) {
	tests := []struct {
		name    string
		colName string
	}{
		{"数字开头", "2fa_code"},
		{"数字开头带后缀", "1st_place"},
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
			// 错误信息须包含列名与标识符说明，直接定位根因，而非兜底的“生成代码存在语法错误”
			if !strings.Contains(err.Error(), tt.colName) || !strings.Contains(err.Error(), "不是合法的 Go 标识符") {
				t.Errorf("错误信息应包含列名与标识符说明，实际: %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
				t.Errorf("生成失败时不应写出文件: %v", statErr)
			}
		})
	}
}

// TestGenerate_EmptyColumns 固化空 Columns 输入的行为：生成仅含空结构体与互转方法的合法文件。
func TestGenerate_EmptyColumns(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       nil,
	}
	if err := Generate(input); err != nil {
		t.Fatalf("空 Columns 应生成空结构体，Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	assertContainsAll(t, got, []string{
		"type UserInfoEntity struct {",
		"type UserInfoDO struct {",
		"func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {",
		"func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {",
	}, "空 Columns 应生成空结构体与互转方法")
	// 产物必须是可解析的合法 Go 代码
	if _, err := format.Source(content); err != nil {
		t.Errorf("空 Columns 生成产物无法通过 gofmt: %v", err)
	}
}

// TestGenerate_UnknownColumnTypeFallback 验证映射表未覆盖的列类型兜底为 string，避免生成非法代码。
func TestGenerate_UnknownColumnTypeFallback(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       []*Column{{Name: "extra", Type: "some_custom_type"}},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !regexp.MustCompile(`Extra\s+string`).Match(content) {
		t.Errorf("未知列类型应兜底为 string:\n%s", content)
	}
}

// TestGenerate_EmptyFieldName 验证列名转换后字段名为空（如纯分隔符列名）时报错，
// 错误信息含列名上下文。
func TestGenerate_EmptyFieldName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       []*Column{{Name: "_", Type: "text"}},
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("列名转换后字段名为空应报错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "字段名为空") {
		t.Errorf("错误信息应说明字段名为空，实际: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
		t.Errorf("生成失败时不应写出文件: %v", statErr)
	}
}

// TestGenerate_MkdirFail 验证输出目录无法创建时返回明确错误。
func TestGenerate_MkdirFail(t *testing.T) {
	base := t.TempDir()
	// 用普通文件占据目录位置，MkdirAll 必失败
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("创建占位文件失败: %v", err)
	}
	input := Input{
		OutputDir: filepath.Join(blocker, "model"),
		Database:  "test_db",
		Dialect:   DialectMysql,
		TableName: "user_info",
		Columns:   []*Column{{Name: "id", Type: "bigint"}},
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("输出目录无法创建应报错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "创建输出目录失败") {
		t.Errorf("错误信息应为创建目录失败，实际: %v", err)
	}
}

// TestGenerate_ExistingFileSyntaxError 验证存量文件语法错误时报错且不覆盖原文件（用户代码保护红线）。
func TestGenerate_ExistingFileSyntaxError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")
	broken := "package model\n\nfunc broken( {\n"
	if err := os.WriteFile(filePath, []byte(broken), 0644); err != nil {
		t.Fatalf("写入初始文件失败: %v", err)
	}
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       []*Column{{Name: "id", Type: "bigint"}},
	}
	err := Generate(input)
	if err == nil {
		t.Fatalf("存量文件语法错误时应报错，实际为 nil")
	}
	if !strings.Contains(err.Error(), "生成结构体失败") {
		t.Errorf("错误信息应包含生成结构体失败，实际: %v", err)
	}
	// 原文件内容不得被修改
	content, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("读取文件失败: %v", readErr)
	}
	if string(content) != broken {
		t.Errorf("存量文件语法错误时不得覆盖原文件\nwant:\n%s\ngot:\n%s", broken, content)
	}
}

// TestGenerate_ChineseColumnName 验证中文列名转换出的字段名首字符不是大写字母（未导出，会被 json 与 zcdb 映射
// 静默跳过）时前置报错并提示 StructFieldInfo.Name、不写出文件；通过 StructFieldInfo.Name 指定导出名后正常生成，
// 列映射 tag 保留中文列名，且产物与 gofmt 标准输出逐字节一致。
func TestGenerate_ChineseColumnName(t *testing.T) {
	newInput := func(dir string, fieldName string) Input {
		return Input{
			OutputDir:        dir,
			Database:         "test_db",
			Dialect:          DialectMysql,
			TableName:        "user_info",
			ColumnTagName:    "db",
			JsonTagValueCase: NameCaseLowerCamel,
			Columns: []*Column{
				{Name: "id", Type: "bigint"},
				{Name: "用户表", Type: "varchar(255)", Comment: "中文列名注释", StructFieldInfo: StructFieldInfo{Name: fieldName}},
			},
		}
	}
	t.Run("未指定字段名报错", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "model")
		err := Generate(newInput(dir, ""))
		if err == nil {
			t.Fatalf("中文列名未指定导出字段名应报错，实际为 nil")
		}
		for _, want := range []string{`"用户表"`, "不是导出标识符", "StructFieldInfo.Name"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("错误信息应包含 %q，实际: %v", want, err)
			}
		}
		if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
			t.Errorf("生成失败时不应写出文件: %v", statErr)
		}
	})
	t.Run("指定导出字段名后生成", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "model")
		if err := Generate(newInput(dir, "UserTable")); err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
		if err != nil {
			t.Fatalf("读取生成文件失败: %v", err)
		}
		if !regexp.MustCompile("(?m)^\\s+UserTable\\s+string\\s+`json:\"用户表\" db:\"用户表\" ddl:\"varchar\\(255\\)\" description:\"中文列名注释\"`$").Match(content) {
			t.Errorf("中文列名字段生成错误:\n%s", content)
		}
		formatted, err := format.Source(content)
		if err != nil {
			t.Fatalf("生成产物无法通过 gofmt: %v", err)
		}
		if string(formatted) != string(content) {
			t.Errorf("生成产物与 gofmt 输出不一致\ngot:\n%s\nwant:\n%s", content, formatted)
		}
	})
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
	// 精确断言错误文本：列名与字段名一律以 %q（双引号）输出，与文档「字段名以 %q 输出」的声明一致
	const want = `列 "user_name" 与 "userName" 转换后的字段名重复: "UserName"`
	if err.Error() != want {
		t.Errorf("字段名重复错误信息不符\nwant: %s\ngot:  %s", want, err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
		t.Errorf("生成失败时不应写出文件: %v", statErr)
	}
}

// TestGenerate_UnusableFieldNames 验证能通过 token.IsIdentifier 却无法编译或无法映射的字段名前置报错且不写出文件：
// 与生成方法 ToDO/ToEntity 同名（列名 to_entity、to_d_o 转换而来或显式指定）会产生「字段与方法同名」编译错误；
// 显式指定的空白标识符 _ 不是导出标识符，在 ToDO/ToEntity 中无法被引用。错误信息须含列名与改名提示。
func TestGenerate_UnusableFieldNames(t *testing.T) {
	tests := []struct {
		name      string
		colName   string
		fieldName string
		wantErr   string
	}{
		{"列名转换为 ToEntity", "to_entity", "", "与生成方法同名"},
		{"列名转换为 ToDO", "to_d_o", "", "与生成方法同名"},
		{"显式指定 ToDO", "x", "ToDO", "与生成方法同名"},
		{"显式指定空白标识符", "x", "_", "不是导出标识符"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			err := Generate(Input{
				OutputDir: dir,
				Dialect:   DialectMysql,
				TableName: "user_info",
				Columns:   []*Column{{Name: tt.colName, Type: "text", StructFieldInfo: StructFieldInfo{Name: tt.fieldName}}},
			})
			if err == nil {
				t.Fatalf("列 %q 期望返回错误，实际为 nil", tt.colName)
			}
			for _, want := range []string{strconv.Quote(tt.colName), tt.wantErr, "StructFieldInfo.Name"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("错误信息应包含 %q，实际: %v", want, err)
				}
			}
			if _, statErr := os.Stat(filepath.Join(dir, "user_info.go")); !os.IsNotExist(statErr) {
				t.Errorf("生成失败时不应写出文件: %v", statErr)
			}
		})
	}
}

// TestGenerate_NilColumn 验证 Columns 含 nil 元素时返回带下标的错误（而非解引用 panic），且不创建输出目录。
func TestGenerate_NilColumn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	err := Generate(Input{
		OutputDir: dir,
		Dialect:   DialectMysql,
		TableName: "user_info",
		Columns:   []*Column{{Name: "id", Type: "bigint"}, nil},
	})
	if err == nil || err.Error() != "列[1] 为 nil" {
		t.Fatalf("期望错误 %q，实际: %v", "列[1] 为 nil", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("生成失败时不应创建输出目录: %v", statErr)
	}
}

// TestGenerate_EmptyOutputDir 验证 OutputDir 为空串时返回「输出目录为空」，
// 而非 os.MkdirAll("") 的系统错误，且不会在当前工作目录写出文件。
func TestGenerate_EmptyOutputDir(t *testing.T) {
	wd := t.TempDir()
	t.Chdir(wd)
	err := Generate(Input{
		Dialect:   DialectMysql,
		TableName: "user_info",
		Columns:   []*Column{{Name: "id", Type: "bigint"}},
	})
	if err == nil || err.Error() != "输出目录为空" {
		t.Fatalf("期望错误 %q，实际: %v", "输出目录为空", err)
	}
	entries, readErr := os.ReadDir(wd)
	if readErr != nil {
		t.Fatalf("读取工作目录失败: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("工作目录不应出现文件: %v", entries)
	}
}

// TestGenerate_StructCommentQualifiedName 验证结构体注释中的表限定名：Database 非空时为 {Database}.{TableName}，
// 为空时只呈现表名，不出现残缺的 .table 前缀。
func TestGenerate_StructCommentQualifiedName(t *testing.T) {
	tests := []struct {
		name     string
		database string
		want     []string
	}{
		{"有数据库名", "test_db", []string{
			"// UserInfoEntity test_db.user_info 表，entity结构体，常用于数据库读取操作。\n",
			"// UserInfoDO test_db.user_info 表，do结构体，常用于数据库写入操作。\n",
		}},
		{"无数据库名", "", []string{
			"// UserInfoEntity user_info 表，entity结构体，常用于数据库读取操作。\n",
			"// UserInfoDO user_info 表，do结构体，常用于数据库写入操作。\n",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			if err := Generate(Input{
				OutputDir: dir,
				Database:  tt.database,
				Dialect:   DialectMysql,
				TableName: "user_info",
				Columns:   []*Column{{Name: "id", Type: "bigint"}},
			}); err != nil {
				t.Fatalf("Generate() error = %v", err)
			}
			content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
			if err != nil {
				t.Fatalf("读取生成文件失败: %v", err)
			}
			got := string(content)
			assertContainsAll(t, got, tt.want, "结构体注释的表限定名")
			assertNotContains(t, got, " .user_info", "不应出现残缺的 .table 限定名")
		})
	}
}

// TestGenerate_InvalidColumnTagName 验证 ColumnTagName 不能作为 reflect.StructTag 的 key（含空格、控制字符、
// 冒号、双引号、反引号、DEL）时前置报错且不创建输出目录；合法 key（如 db）由其余 Generate 用例覆盖。
func TestGenerate_InvalidColumnTagName(t *testing.T) {
	for _, tagName := range []string{"a b", "a\tb", "a:b", `a"b`, "a`b", "a\x7fb"} {
		t.Run(strconv.Quote(tagName), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "model")
			err := Generate(Input{
				OutputDir:     dir,
				Dialect:       DialectMysql,
				TableName:     "user_info",
				ColumnTagName: tagName,
				Columns:       []*Column{{Name: "id", Type: "bigint"}},
			})
			want := fmt.Sprintf("列映射 tag 名非法: %q", tagName)
			if err == nil || err.Error() != want {
				t.Fatalf("期望错误 %q，实际: %v", want, err)
			}
			if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
				t.Errorf("生成失败时不应创建输出目录: %v", statErr)
			}
		})
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
	assertContainsAll(t, string(content), []string{"ID", "CreatedAt", "time.Time", `import "time"`},
		"补全结果应体现在生成文件中")
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
	// 文件头（build tags + package 注释 + 原 package 行）逐字节保留，且不追加任何生成声明
	wantHeader := "//go:build ignore\n// +build ignore\n\n// Package model 包文档注释。\npackage model\n"
	if !strings.HasPrefix(got, wantHeader) {
		t.Errorf("文件头 build tags/package 注释未保留\nwant prefix:\n%s\ngot:\n%s", wantHeader, got)
	}
	assertContainsAll(t, got, []string{
		"var Extra = 1",
		"type UserInfoEntity struct {",
	}, "含 build tags 的文件再生成后用户代码与生成代码应共存")
}

// TestGenerate_RegenerateWithAliasImport 验证存量文件以别名导入生成代码所需包（如 import mytime "time"）时，
// 再生成不被判重误判为已存在：补充标准导入，别名导入与用户代码完整保留，产物可编译（ZCM-03）。
func TestGenerate_RegenerateWithAliasImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	input := Input{
		OutputDir:     dir,
		Database:      "test_db",
		Dialect:       DialectMysql,
		TableName:     "user_info",
		ColumnTagName: "db",
		Columns:       []*Column{{Name: "id", Type: "bigint"}},
	}
	if err := Generate(input); err != nil {
		t.Fatalf("第一次 Generate() error = %v", err)
	}
	// 模拟用户改造存量文件：把 time 改为别名导入，并新增依赖该别名的自定义方法
	filePath := filepath.Join(dir, "user_info.go")
	userCode := `package model

import (
	mytime "time"
)

type UserInfoEntity struct {
	ID int64
}

func (e *UserInfoEntity) Now() mytime.Time {
	return mytime.Now()
}
`
	if err := os.WriteFile(filePath, []byte(userCode), 0644); err != nil {
		t.Fatalf("写入存量文件失败: %v", err)
	}
	// 再生成：新增 datetime 列，生成代码引用默认包名 time.Time
	input.Columns = append(input.Columns, &Column{Name: "created_at", Type: "datetime"})
	if err := Generate(input); err != nil {
		t.Fatalf("别名导入存量文件再生成应成功，Generate() error = %v", err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)
	// 别名导入不满足生成代码需要，须补充标准导入 "time"，同时保留用户别名导入与自定义方法
	assertContainsAll(t, got, []string{
		"mytime \"time\"",
		"\t\"time\"\n",
		"func (e *UserInfoEntity) Now() mytime.Time {",
	}, "别名导入存量文件再生成")
	if strings.Count(got, "import (") != 1 {
		t.Errorf("缺失导入应合并进原 import 块（不形成两个 import 块）:\n%s", got)
	}
	if !regexp.MustCompile(`CreatedAt\s+time\.Time`).MatchString(got) {
		t.Errorf("新增列 CreatedAt 生成错误:\n%s", got)
	}
	// 编译级校验：别名导入与补充的标准导入共存、生成代码引用默认包名 time 可用（非 imported and not used）
	typeCheckGoSource(t, got)
}

// sampleColumns 返回 §3.4 样板的列定义（MySQL 8 元数据形态：Nullable/Default/PrimaryKey/Extra 均显式给出）。
func sampleColumns() []*Column {
	return []*Column{
		{Name: "id", Type: "bigint unsigned", Nullable: boolPtr(false), PrimaryKey: true, Extra: "auto_increment", Comment: "主键"},
		{Name: "order_no", Type: "varchar(32)", Nullable: boolPtr(false), Comment: "订单号"},
		{Name: "user_id", Type: "bigint", Nullable: boolPtr(false), Comment: "用户ID"},
		{Name: "email", Type: "varchar(64)", Nullable: boolPtr(false), Comment: "用户邮箱"},
		{Name: "amount", Type: "decimal(10,2)", Nullable: boolPtr(false), Default: strPtr("0.00"), Comment: "订单金额（保留两位小数）"},
		{Name: "status", Type: "enum('pending','paid','refunded')", Nullable: boolPtr(false), Default: strPtr("pending"), Comment: "订单状态"},
		{Name: "remark", Type: "varchar(255)", Nullable: boolPtr(true), Comment: "备注（可为空）"},
		{Name: "created_at", Type: "datetime", Nullable: boolPtr(false), Default: strPtr("CURRENT_TIMESTAMP"), Extra: "DEFAULT_GENERATED", Comment: "创建时间"},
	}
}

// sampleIndexes 返回 §3.4 样板的索引（刻意打乱传入顺序，验证渲染端自行排序）。
func sampleIndexes() []IndexInfo {
	return []IndexInfo{
		{Name: "idx_user_status", Columns: []string{"user_id", "status"}},
		{Name: "uk_email", Columns: []string{"email"}, Unique: true},
		{Name: "PRIMARY", Columns: []string{"id"}, Unique: true, Primary: true},
		{Name: "uk_order_no", Columns: []string{"order_no"}, Unique: true},
	}
}

// TestGenerate_SampleWithMetadataGolden 端到端 golden 锁定 §3.4 样板产物：
// 索引块（排序后固定顺序 + gofmt 三空格清单形态）、字段 tag 顺序与 ddl 片段取值逐行核对，
// 索引块在 Entity 与 DO 各出现一次，产物与 gofmt 输出逐字节一致并可被 go/parser 解析。
func TestGenerate_SampleWithMetadataGolden(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	err := Generate(Input{
		OutputDir: dir, Database: "test_db", Dialect: DialectMysql,
		TableName: "user_order", TableComment: "订单表", ColumnTagName: "db",
		JsonTagValueCase: NameCaseLowerCamel, Columns: sampleColumns(), Indexes: sampleIndexes(),
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_order.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)

	assertContainsAll(t, got, []string{
		"package model\n",
		`import "time"`,
		"// UserOrderEntity test_db.user_order 订单表，entity结构体，常用于数据库读取操作。",
		"//",
		"// 索引:",
		"//   - PRIMARY KEY (id)",
		"//   - UNIQUE KEY uk_email (email)",
		"//   - UNIQUE KEY uk_order_no (order_no)",
		"//   - KEY idx_user_status (user_id, status)",
		"type UserOrderEntity struct {",
		"type UserOrderDO struct {",
	}, "§3.4 样板产物")

	// tag 顺序 json → db → primary_key → ddl → description；字段类型与 ddl 片段取值逐列核对
	// （期望事实源与真库端到端用例共用 sampleFieldExpectations；
	//  tag 字面量经 QuoteMeta：ddl 含括号与单引号，不转义会被当作正则元字符）
	for _, tc := range sampleFieldExpectations() {
		entityRe := regexp.MustCompile(regexp.QuoteMeta(tc.Field) + `\s+` + regexp.QuoteMeta(tc.Type) + `\s+` + regexp.QuoteMeta(tc.Tag))
		if !entityRe.MatchString(got) {
			t.Errorf("Entity 字段 %s（%s）未匹配期望 tag: %s", tc.Field, tc.Type, tc.Tag)
		}
		// DO 侧字段类型为 any，tag 与 Entity 完全一致
		doRe := regexp.MustCompile(regexp.QuoteMeta(tc.Field) + `\s+any\s+` + regexp.QuoteMeta(tc.Tag))
		if !doRe.MatchString(got) {
			t.Errorf("DO 字段 %s 未匹配期望 tag: %s", tc.Field, tc.Tag)
		}
	}
	// 索引块在 Entity 与 DO 的 doc 注释中各出现一次
	if n := strings.Count(got, "//   - PRIMARY KEY (id)"); n != 2 {
		t.Errorf("索引块应在 Entity 与 DO 各出现一次，实际 %d 次:\n%s", n, got)
	}
	// 断言精度：主键行不得显示传入的物理索引名（渲染端隐藏）
	assertNotContains(t, got, "KEY PRIMARY", "主键行不应渲染为普通 KEY 行")

	// 产物必须与 gofmt 标准输出逐字节一致（三空格清单形态已按 gofmt 规范书写）
	formatted, err := format.Source(content)
	if err != nil {
		t.Fatalf("生成产物无法通过 gofmt: %v\n%s", err, got)
	}
	if string(formatted) != got {
		t.Errorf("生成产物与 gofmt 输出不一致\ngot:\n%s\nwant:\n%s", got, formatted)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "user_order.go", got, parser.AllErrors); err != nil {
		t.Errorf("生成产物存在语法错误: %v", err)
	}
}

// TestGenerate_LegacyInputDelta 验证老式 Input（不填任何新字段）的产物差异恰为 `ddl` tag：
// Nullable 未设置时 ddl 片段只含类型（不误标 NOT NULL），不生成 primary_key tag，索引块因数据为空自然省略。
func TestGenerate_LegacyInputDelta(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "model")
	err := Generate(Input{
		OutputDir: dir, Database: "test_db", Dialect: DialectMysql,
		TableName: "user_info", TableComment: "用户信息表", ColumnTagName: "db",
		JsonTagValueCase: NameCaseLowerCamel,
		Columns: []*Column{
			{Name: "id", Type: "bigint(20)", Comment: "主键"},
			{Name: "user_name", Type: "VARCHAR(255)", Comment: "用户名"},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	got := string(content)

	assertContains(t, got, "// UserInfoEntity test_db.user_info 用户信息表，entity结构体，常用于数据库读取操作。\ntype UserInfoEntity struct {", "无索引时不生成索引块")
	for _, tc := range []struct{ field, tag string }{
		{"ID", "`json:\"id\" db:\"id\" ddl:\"bigint(20)\" description:\"主键\"`"},
		{"UserName", "`json:\"userName\" db:\"user_name\" ddl:\"VARCHAR(255)\" description:\"用户名\"`"},
	} {
		re := regexp.MustCompile(regexp.QuoteMeta(tc.field) + `\s+\S+\s+` + regexp.QuoteMeta(tc.tag))
		if !re.MatchString(got) {
			t.Errorf("老式 Input 字段 %s 的 ddl tag 应为「仅类型」，实际未匹配: %s", tc.field, tc.tag)
		}
	}
	// 未提供元数据时不产生任何推断：无 NULL 标记、无 primary_key tag、无索引块
	assertNotContains(t, got, "NOT NULL", "Nullable 未知时不应渲染 NOT NULL")
	assertNotContains(t, got, "primary_key", "未标记主键时不应生成 primary_key tag")
	assertNotContains(t, got, "索引:", "未提供索引时不应生成索引块")
}

// TestGenerate_NoGeneratedMarker 回归锁死「产物不是官方意义上的生成物」：产物不得匹配 Go 官方
// 生成标记正则（^// Code generated .* DO NOT EDIT\.$）——匹配会使 staticcheck/golangci-lint 等
// 工具整体跳过该文件，而本文件含用户代码、不应被整体豁免静态检查。
// 自拟的分区声明说明头已按决策移除（2026-09-21），故同时锁死产物以 package 声明起始。
func TestGenerate_NoGeneratedMarker(t *testing.T) {
	official := regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	dir := filepath.Join(t.TempDir(), "model")
	if err := Generate(Input{
		OutputDir: dir, Database: "test_db", Dialect: DialectMysql,
		TableName: "user_info", ColumnTagName: "db",
		Columns: []*Column{{Name: "id", Type: "bigint(20)"}},
	}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "user_info.go"))
	if err != nil {
		t.Fatalf("读取生成文件失败: %v", err)
	}
	if !strings.HasPrefix(string(content), "package model\n") {
		t.Errorf("产物应以 package 声明起始（不生成任何文件头声明）:\n%s", content)
	}
	for i, line := range strings.Split(string(content), "\n") {
		if official.MatchString(line) {
			t.Errorf("产物第 %d 行匹配官方生成标记正则: %q", i+1, line)
		}
	}
}

// TestIsPathWithinDir_RelError 覆盖 filepath.Rel 报错时 isPathWithinDir 直接返回 false 的分支。
// 相对目录与绝对路径混用时 filepath.Rel 在各平台均报错：Rel 比较两侧「是否以分隔符开头」，
// 不一致即报错（Windows 上还叠加卷名不同）。
// 由于返回 false 也可能来自后续的 ".." 前缀检查分支，先断言 Rel 确实报错，确保覆盖目标分支。
func TestIsPathWithinDir_RelError(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("获取工作目录失败: %v", err)
	}
	const relDir = "relative/dir"
	absPath := filepath.Join(wd, "abs.go")

	// 前提校验：与 isPathWithinDir 内部一致地传入 Clean 后的路径
	if _, relErr := filepath.Rel(filepath.Clean(relDir), filepath.Clean(absPath)); relErr == nil {
		t.Fatalf("前提不成立：filepath.Rel(%q, %q) 未报错，本用例无法覆盖 Rel 错误分支", relDir, absPath)
	}
	if isPathWithinDir(relDir, absPath) {
		t.Fatal("相对目录与绝对路径混用时 Rel 报错，应返回 false")
	}
}

// FuzzGenerate 覆盖 Generate 的端到端路径与失败原子性：
// 输入表/文件名/tag/注释/列规格（列名、类型、注释以 ';' 分隔，'|' 分列，最多 16 列，单项 ≤1KB）；
// 成功时断言产物确实写入唯一目标文件、Entity/DO 声明与字段名/类型/tag 与输入一致、
// 可解析/格式化、二次生成字节一致；失败时既有哨兵文件逐字节不变、路径解析失败时
// 不创建输出目录且无临时文件。编译级类型检查按内容哈希抽样执行（约 1/16），
// 完整类型检查由定向用例覆盖。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcmodel -run '^$' -fuzz '^FuzzGenerate$' -fuzztime=30s
func FuzzGenerate(f *testing.F) {
	type seed struct {
		dialectIdx   uint8
		table        string
		fileName     string
		columnTag    string
		tableComment string
		columns      string
		flags        uint8
	}
	seeds := []seed{
		{0, "users", "", "db", "用户表", "id;bigint(20);主键|user_name;varchar(64);用户名", 0},
		{1, "users", "", "db", "", "id;bigint;PK|created_at;timestamp with time zone;", 1 | 8},
		{2, "users", "users_model", "db", "用户", "id;INTEGER;|name;TEXT;", 2 | 4},
		{0, "user_info", "", "column", "注释含`反引号`\n换行", "id;int;|name;text;", 0},
		{0, "", "users", "db", "", "id;int;", 0},
		{0, "../escape", "users", "db", "", "id;int;", 0},
		{0, "public.users", "users", "db", "", "id;int;", 0},
		{0, "2fa", "", "db", "", "id;int;", 0},
		{0, "users", "x_test", "db", "", "id;int;", 0},
		{0, "users", "", "bad tag", "", "id;int;", 0},
		{0, "users", "", "db", "", "id;int;|id;text;", 0},
		{0, "users", "", "db", "", "nil", 0},
		{0, "users", "", "db", "", "", 0},
		{0, "users", "", "db", "", "id;int;", 64},
		{0, "users", "", "db", "", "id;unknown_type;", 0},
		{0, "users", "", "db", "", "id;int;", 32},
		{255, "users", "", "db", "", "id;int;", 0},
	}
	for _, s := range seeds {
		f.Add(s.dialectIdx, s.table, s.fileName, s.columnTag, s.tableComment, s.columns, s.flags)
	}

	f.Fuzz(func(t *testing.T, dialectIdx uint8, table, fileName, columnTag, tableComment, columnsSpec string, flags uint8) {
		if len(table) > 1<<10 || len(fileName) > 1<<10 || len(columnTag) > 1<<10 ||
			len(tableComment) > 1<<10 || len(columnsSpec) > 4<<10 {
			return
		}
		dialects := []Dialect{DialectMysql, DialectPostgres, DialectSqlite}
		dialect := dialects[int(dialectIdx)%len(dialects)]
		if dialectIdx == 255 {
			// 未知方言只保留一个明确入口，避免随机输入中 1/4 永远落在错误分支。
			dialect = Dialect("oracle")
		}
		columns := decodeFuzzColumns(columnsSpec)
		for i, column := range columns {
			if column == nil {
				continue
			}
			if flags&2 != 0 {
				column.Nullable = boolPtr(i%2 == 0)
			}
			if flags&4 != 0 {
				column.Default = strPtr("")
			}
			if i == 0 && flags&8 != 0 {
				column.PrimaryKey = true
			}
			if i == 0 && flags&16 != 0 {
				column.Extra = "auto_increment"
			}
		}
		input := Input{
			OutputDir:     filepath.Join(t.TempDir(), "model"),
			Database:      "fuzz_db",
			Dialect:       dialect,
			TableName:     table,
			FileName:      fileName,
			TableComment:  tableComment,
			ColumnTagName: columnTag,
			Columns:       columns,
		}
		if flags&1 != 0 {
			input.JsonTagValueCase = NameCaseLowerCamel
		}
		if flags&64 != 0 {
			input.JsonTagValueCase = NameCase("invalid")
		}
		if flags&32 != 0 {
			input.Indexes = []IndexInfo{{Name: "idx_fuzz", Columns: []string{"id"}}}
		}

		resolved, resolveErr := resolveFileName(fileName, table)
		sentinel := []byte("package model\n\nvar FuzzSentinel = 1\n")
		target := ""
		couldCreateDir := resolveErr == nil && resolved != ""
		if couldCreateDir {
			candidate := filepath.Join(input.OutputDir, resolved+".go")
			if isPathWithinDir(input.OutputDir, candidate) {
				if err := os.MkdirAll(input.OutputDir, 0o755); err != nil {
					t.Fatalf("mkdir output: %v", err)
				}
				// 平台不允许的文件名（如控制字符）无法预写哨兵；跳过预写，
				// 由 Generate 自身的校验/写入路径返回错误。
				if err := os.WriteFile(candidate, sentinel, 0o644); err == nil {
					target = candidate
				}
			}
		}

		err := Generate(input)
		if err != nil {
			if target != "" {
				after, readErr := os.ReadFile(target)
				if readErr != nil {
					t.Fatalf("read target after failed Generate: %v", readErr)
				}
				if !bytes.Equal(after, sentinel) {
					t.Fatalf("failed Generate modified target:\nbefore: %q\nafter:  %q\nerr: %v", sentinel, after, err)
				}
			}
			// 路径解析失败时 Generate 在 MkdirAll 之前返回，输出目录不得被创建。
			if !couldCreateDir {
				if _, statErr := os.Stat(input.OutputDir); !os.IsNotExist(statErr) {
					t.Fatalf("Generate failed (%v) but created output directory %s", err, input.OutputDir)
				}
			}
			assertFuzzNoTempFiles(t, input.OutputDir)
			return
		}
		if resolveErr != nil {
			t.Fatalf("resolveFileName failed (%v) but Generate succeeded", resolveErr)
		}
		if target == "" {
			t.Fatalf("Generate succeeded without a resolved target (table=%q file=%q)", table, fileName)
		}
		content, readErr := os.ReadFile(target)
		if readErr != nil {
			t.Fatalf("read generated file: %v", readErr)
		}
		if bytes.Equal(content, sentinel) {
			t.Fatalf("Generate succeeded but did not rewrite target %s", target)
		}
		if !bytes.Contains(content, []byte("FuzzSentinel")) {
			t.Fatalf("user sentinel var was lost during regeneration")
		}
		// 目标文件必须恰好是 resolved+".go"：防止生成器写错路径或额外落盘其他文件时误判成功。
		entries, readDirErr := os.ReadDir(input.OutputDir)
		if readDirErr != nil {
			t.Fatalf("read output dir: %v", readDirErr)
		}
		var goFiles []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.HasSuffix(entry.Name(), ".go") {
				goFiles = append(goFiles, entry.Name())
			}
		}
		wantFile := resolved + ".go"
		if len(goFiles) != 1 || goFiles[0] != wantFile {
			t.Fatalf("generated file set = %v, want [%s]", goFiles, wantFile)
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), target, content, parser.AllErrors)
		if parseErr != nil {
			t.Fatalf("generated file syntax error: %v\n%s", parseErr, content)
		}
		formatted, formatErr := format.Source(content)
		if formatErr != nil {
			t.Fatalf("generated file format error: %v", formatErr)
		}
		if !bytes.Equal(formatted, content) {
			t.Fatalf("generated file is not gofmt-stable")
		}
		// 编译级检查较慢，按内容哈希抽样；完整类型检查由定向用例覆盖。
		if fuzzShouldTypeCheck(content) {
			typeCheckGoSource(t, string(content))
		}
		assertFuzzNoTempFiles(t, input.OutputDir)

		expected := make([]*Column, len(columns))
		for i, column := range columns {
			if column == nil {
				t.Fatalf("Generate succeeded with nil column at index %d", i)
			}
			expected[i] = column
		}
		baseName := ToPascalCase(table)
		fuzzAssertGeneratedStruct(t, file, baseName+"Entity", expected, false, input.Dialect, columnTag, input.JsonTagValueCase)
		fuzzAssertGeneratedStruct(t, file, baseName+"DO", expected, true, input.Dialect, columnTag, input.JsonTagValueCase)
		if flags&32 != 0 && !bytes.Contains(content, []byte("idx_fuzz")) {
			t.Fatalf("index comment idx_fuzz missing from generated file")
		}

		if err := Generate(input); err != nil {
			t.Fatalf("second Generate failed: %v", err)
		}
		second, readErr := os.ReadFile(target)
		if readErr != nil {
			t.Fatalf("read regenerated file: %v", readErr)
		}
		if !bytes.Equal(content, second) {
			t.Fatalf("Generate is not idempotent for target %s", target)
		}
	})
}

// fuzzShouldTypeCheck 用内容哈希决定是否做编译级检查：约 1/16 的成功输入执行，
// 使 fuzz 保持可观的执行速率，同时仍能发现系统性生成代码错误。
func fuzzShouldTypeCheck(src []byte) bool {
	var sum uint32
	for _, b := range src {
		sum = sum*31 + uint32(b)
	}
	return sum%16 == 0
}

// fuzzAssertGeneratedStruct 校验生成的 Entity/DO 结构体：字段数量、顺序、名称、类型与 tag
// 必须与输入列一致；tag 经 reflect.StructTag 语义读取，反引号按 sanitizeTagValue 约定替换为单引号。
func fuzzAssertGeneratedStruct(t *testing.T, file *ast.File, name string, columns []*Column, do bool, dialect Dialect, columnTag string, jsonCase NameCase) {
	t.Helper()
	st := fuzzFindGeneratedStruct(t, file, name)
	if st.Fields == nil {
		t.Fatalf("%s has nil field list", name)
	}
	fields := st.Fields.List
	if len(fields) != len(columns) {
		t.Fatalf("%s field count = %d, want %d", name, len(fields), len(columns))
	}
	seen := make(map[string]bool, len(fields))
	for i, field := range fields {
		column := columns[i]
		if len(field.Names) != 1 {
			t.Fatalf("%s field %d has %d names, want 1", name, i, len(field.Names))
		}
		fieldName := field.Names[0].Name
		if want := ToPascalCase(column.Name); fieldName != want {
			t.Fatalf("%s field %d name = %q, want %q", name, i, fieldName, want)
		}
		if !token.IsExported(fieldName) {
			t.Fatalf("%s field %q is not exported", name, fieldName)
		}
		if fieldName == "ToDO" || fieldName == "ToEntity" {
			t.Fatalf("%s field %q conflicts with a generated method", name, fieldName)
		}
		if seen[fieldName] {
			t.Fatalf("%s contains duplicate field %q", name, fieldName)
		}
		seen[fieldName] = true

		wantType := "any"
		if !do {
			wantType = formatStructFieldType(dialect, column.Type)
			if wantType == "" {
				wantType = "string"
			}
		}
		if gotType := fuzzExprString(t, field.Type); gotType != wantType {
			t.Fatalf("%s field %q type = %q, want %q", name, fieldName, gotType, wantType)
		}

		tag := reflect.StructTag("")
		if field.Tag != nil {
			tag = reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
		}
		if columnTag != "" {
			want := strings.ReplaceAll(column.Name, "`", "'")
			got, ok := tag.Lookup(columnTag)
			if want == "" {
				if ok {
					t.Fatalf("%s field %q unexpectedly has tag %q=%q", name, fieldName, columnTag, got)
				}
			} else if !ok || got != want {
				t.Fatalf("%s field %q tag %q = (%q,%v), want %q", name, fieldName, columnTag, got, ok, want)
			}
		}
		if jsonCase != "" {
			want := strings.ReplaceAll(formatJSONTag(column.Name, jsonCase), "`", "'")
			got, ok := tag.Lookup("json")
			if want == "" {
				if ok {
					t.Fatalf("%s field %q unexpectedly has empty json tag", name, fieldName)
				}
			} else if !ok || got != want {
				t.Fatalf("%s field %q json tag = (%q,%v), want %q", name, fieldName, got, ok, want)
			}
		} else if _, ok := tag.Lookup("json"); ok {
			t.Fatalf("%s field %q unexpectedly has json tag", name, fieldName)
		}
		if column.PrimaryKey {
			if got, ok := tag.Lookup("primary_key"); !ok || got != "true" {
				t.Fatalf("%s field %q primary_key tag = (%q,%v), want true", name, fieldName, got, ok)
			}
		} else if _, ok := tag.Lookup("primary_key"); ok {
			t.Fatalf("%s field %q unexpectedly has primary_key tag", name, fieldName)
		}
		if column.Type != "" {
			want := strings.ReplaceAll(assembleDDLFragment(*column), "`", "'")
			if got, ok := tag.Lookup("ddl"); !ok || got != want {
				t.Fatalf("%s field %q ddl tag = (%q,%v), want %q", name, fieldName, got, ok, want)
			}
		} else if _, ok := tag.Lookup("ddl"); ok {
			t.Fatalf("%s field %q unexpectedly has ddl tag", name, fieldName)
		}
		if column.Comment != "" {
			want := strings.ReplaceAll(column.Comment, "`", "'")
			if got, ok := tag.Lookup("description"); !ok || got != want {
				t.Fatalf("%s field %q description tag = (%q,%v), want %q", name, fieldName, got, ok, want)
			}
		} else if _, ok := tag.Lookup("description"); ok {
			t.Fatalf("%s field %q unexpectedly has description tag", name, fieldName)
		}
	}
}

// fuzzFindGeneratedStruct 在解析后的文件中查找指定名称的结构体声明，缺失或重复均视为失败。
func fuzzFindGeneratedStruct(t *testing.T, file *ast.File, name string) *ast.StructType {
	t.Helper()
	var found *ast.StructType
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("type %s is not a struct", name)
			}
			if found != nil {
				t.Fatalf("duplicate type %s", name)
			}
			found = st
		}
	}
	if found == nil {
		t.Fatalf("missing generated type %s", name)
	}
	return found
}

// fuzzExprString 将 AST 类型表达式渲染为规范字符串，用于与期望 Go 类型比较。
func fuzzExprString(t *testing.T, expr ast.Expr) string {
	t.Helper()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), expr); err != nil {
		t.Fatalf("print field type: %v", err)
	}
	return buf.String()
}
