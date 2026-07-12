package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ======================== toPascalCase ========================

func TestToPascalCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// ===== 原 tableNameToPascal 覆盖的场景 =====
		// snake_case
		{"user_info", "UserInfo"},
		{"id", "ID"},
		{"user_id", "UserID"},
		{"name", "Name"},
		{"a_b_c", "ABC"},
		{"user_name_age", "UserNameAge"},
		{"order_item", "OrderItem"},
		{"created_at", "CreatedAt"},
		{"user__name", "UserName"},
		{"_user", "User"},
		{"user_", "User"},
		// camelCase
		{"userInfo", "UserInfo"},
		{"userId", "UserID"},
		// PascalCase
		{"UserInfo", "UserInfo"},
		{"UserId", "UserID"},
		// kebab-case
		{"user-info", "UserInfo"},
		{"user-id", "UserID"},
		// UPPER_SNAKE
		{"USER_INFO", "UserInfo"},
		{"USER_ID", "UserID"},

		// ===== 原 columnToFieldName 覆盖的场景 =====
		{"email", "Email"},
		{"getUserById", "GetUserByID"},
		{"PasswordMd5", "PasswordMd5"},
		{"USER_NAME", "UserName"},
		// 混合风格
		{"User_Account", "UserAccount"},
		{"userID", "UserID"},
		// 空字符串
		{"", ""},
	}
	for _, c := range cases {
		got := toPascalCase(c.input)
		if got != c.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestSanitizeColumnName(t *testing.T) {
	cases := []struct {
		input   string
		replace string
		want    string
	}{
		// ========== replace="_" 模式 ==========
		// 首字母大写
		{"name", "_", "Name"},
		{"id", "_", "Id"},
		{"user", "_", "User"},
		// 连字符替换为下划线
		{"user-Account", "_", "User_Account"},
		{"user-info", "_", "User_info"},
		{"a-b-c", "_", "A_b_c"},
		// 保留已有下划线
		{"User_Account", "_", "User_Account"},
		{"user_id", "_", "User_id"},
		// PascalCase 保留原始结构
		{"UserAccount", "_", "UserAccount"},
		{"PasswordMd5", "_", "PasswordMd5"},
		// 无连字符的普通名称
		{"name", "_", "Name"},
		// 空字符串
		{"", "_", ""},

		// ========== replace="" 模式 ==========
		// 单连字符：左侧尾字母大写 + 右侧首字母大写
		{"user-Account", "", "UseRAccount"},
		{"user-account", "", "UseRAccount"},
		{"first-name", "", "FirsTName"},
		// 多连字符
		{"first-name-part", "", "FirsTNamEPart"},
		{"a-b-c", "", "ABC"},
		// 连字符在开头
		{"-name", "", "Name"},
		// 连字符在结尾
		{"name-", "", "NamE"},
		// 仅有连字符（空结果 → 返回空字符串）
		{"---", "", ""},
		{"-", "", ""},
		// 无连字符：仅首字母大写
		{"name", "", "Name"},
		{"UserAccount", "", "UserAccount"},
		{"PasswordMd5", "", "PasswordMd5"},
		// 下划线也作为分隔符处理（与连字符行为一致）
		{"User_Account", "", "UseRAccount"},
		{"user_id", "", "UseRId"},
		// 空字符串
		{"", "", ""},
	}
	for _, c := range cases {
		got := sanitizeColumnName(c.input, c.replace)
		if got != c.want {
			t.Errorf("sanitizeColumnName(%q, %q) = %q, want %q", c.input, c.replace, got, c.want)
		}
	}
}



// testFieldMap 测试辅助函数，根据列名列表生成 fieldNameMap
func testFieldMap(columns []Column) map[string]string {
	return buildFieldNameMap(columns)
}

// ======================== buildFieldNameMap ========================

func TestBuildFieldNameMap_NoCollision(t *testing.T) {
	columns := []Column{
		{Name: "id"},
		{Name: "name"},
	}
	m := buildFieldNameMap(columns)
	if m["id"] != "ID" {
		t.Errorf("expected ID, got %s", m["id"])
	}
	if m["name"] != "Name" {
		t.Errorf("expected Name, got %s", m["name"])
	}
}

func TestBuildFieldNameMap_Collision(t *testing.T) {
	columns := []Column{
		{Name: "User_Account"},
		{Name: "UserAccount"},
		{Name: "user-Account"},
	}
	m := buildFieldNameMap(columns)
	// 三个列名经 toPascalCase 都产生 UserAccount，发生碰撞
	// 碰撞后改用 sanitizeColumnName
	if m["User_Account"] != "User_Account" {
		t.Errorf("expected User_Account, got %s", m["User_Account"])
	}
	if m["UserAccount"] != "UserAccount" {
		t.Errorf("expected UserAccount, got %s", m["UserAccount"])
	}
	if m["user-Account"] != "UseRAccount" {
		t.Errorf("expected UseRAccount, got %s", m["user-Account"])
	}
}

// ======================== splitWords ========================

func TestSplitWords(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"user_info", []string{"user", "info"}},
		{"userID", []string{"user", "ID"}},
		{"getUserById", []string{"get", "User", "By", "Id"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"user-name", []string{"user", "name"}},
		{"USER_ID", []string{"USER", "ID"}},
		{"PasswordMd5", []string{"Password", "Md5"}},
		{"", nil},
		{"user__name", []string{"user", "name"}},
		{"_user", []string{"user"}},
		{"user_", []string{"user"}},
		{"a_b_c", []string{"a", "b", "c"}},
		{"User_Account", []string{"User", "Account"}},
	}
	for _, c := range cases {
		got := splitWords(c.input)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("splitWords(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// ======================== toLowerCamel ========================

func TestToLowerCamel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"user_info", "userInfo"},
		{"user-id", "userId"},
		{"UserName", "userName"},
		{"getUserById", "getUserById"},
		{"HTTPServer", "httpServer"},
		{"", ""},
	}
	for _, c := range cases {
		got := toLowerCamel(splitWords(c.input))
		if got != c.want {
			t.Errorf("toLowerCamel(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== toUpperCamel ========================

func TestToUpperCamel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"user_info", "UserInfo"},
		{"user-id", "UserId"},
		{"userName", "UserName"},
		{"getUserById", "GetUserById"},
		{"HTTPServer", "HttpServer"},
		{"", ""},
	}
	for _, c := range cases {
		got := toUpperCamel(splitWords(c.input))
		if got != c.want {
			t.Errorf("toUpperCamel(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== toLowerSnake ========================

func TestToLowerSnake(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"userInfo", "user_info"},
		{"User-Name", "user_name"},
		{"getUserById", "get_user_by_id"},
		{"HTTPServer", "http_server"},
		{"", ""},
	}
	for _, c := range cases {
		got := toLowerSnake(splitWords(c.input))
		if got != c.want {
			t.Errorf("toLowerSnake(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== toUpperSnake ========================

func TestToUpperSnake(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"userInfo", "USER_INFO"},
		{"user-name", "USER_NAME"},
		{"getUserById", "GET_USER_BY_ID"},
		{"HTTPServer", "HTTP_SERVER"},
		{"", ""},
	}
	for _, c := range cases {
		got := toUpperSnake(splitWords(c.input))
		if got != c.want {
			t.Errorf("toUpperSnake(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== toLowerKebab ========================

func TestToLowerKebab(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"userInfo", "user-info"},
		{"user_name", "user-name"},
		{"getUserById", "get-user-by-id"},
		{"HTTPServer", "http-server"},
		{"", ""},
	}
	for _, c := range cases {
		got := toLowerKebab(splitWords(c.input))
		if got != c.want {
			t.Errorf("toLowerKebab(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== toUpperKebab ========================

func TestToUpperKebab(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"userInfo", "USER-INFO"},
		{"user_name", "USER-NAME"},
		{"getUserById", "GET-USER-BY-ID"},
		{"HTTPServer", "HTTP-SERVER"},
		{"", ""},
	}
	for _, c := range cases {
		got := toUpperKebab(splitWords(c.input))
		if got != c.want {
			t.Errorf("toUpperKebab(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== formatJSONTag ========================

func TestFormatJSONTag(t *testing.T) {
	cases := []struct {
		colName string
		style   string
		want    string
	}{
		{"user_id", "lowerCamel", "userId"},
		{"user_id", "upperCamel", "UserId"},
		{"user_id", "lowerSnake", "user_id"},
		{"user_id", "upperSnake", "USER_ID"},
		{"user_id", "lowerKebab", "user-id"},
		{"user_id", "upperKebab", "USER-ID"},
		{"PasswordMd5", "lowerSnake", "password_md5"},
		{"PasswordMd5", "lowerCamel", "passwordMd5"},
		{"", "lowerCamel", ""},
		{"user_id", "", ""},
		{"user_id", "unknown", ""},
	}
	for _, c := range cases {
		got := formatJSONTag(c.colName, c.style)
		if got != c.want {
			t.Errorf("formatJSONTag(%q, %q) = %q, want %q", c.colName, c.style, got, c.want)
		}
	}
}

// ======================== mapSQLTypeToGoType ========================

func TestMapSQLTypeToGoType(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// 整数类型
		{"int", "int"},
		{"integer", "int"},
		{"bigint", "int64"},
		{"smallint", "int"},
		{"tinyint", "int"},
		// 浮点类型
		{"float", "float64"},
		{"double", "float64"},
		{"decimal", "float64"},
		// 布尔类型
		{"bool", "bool"},
		{"boolean", "bool"},
		// 字符串类型
		{"string", "string"},
		{"varchar", "string"},
		{"char", "string"},
		{"text", "string"},
		{"longtext", "string"},
		// 时间类型
		{"time", "time.Time"},
		{"datetime", "time.Time"},
		{"timestamp", "time.Time"},
		{"date", "time.Time"},
		// 未知类型默认返回 string
		{"unknown", "string"},
		{"json", "string"},
		// 大小写不敏感
		{"INT", "int"},
		{"VARCHAR", "string"},
		{"DateTime", "time.Time"},
	}
	for _, c := range cases {
		got := mapSQLTypeToGoType(c.input)
		if got != c.want {
			t.Errorf("mapSQLTypeToGoType(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== buildStruct ========================

func TestBuildStruct_Entity(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildStruct("UserInfoEntity", columns, false, "", "column", "upperCamel", testFieldMap(columns))
	want := `type UserInfoEntity struct {
	ID int ` + "`json:\"Id\" column:\"id\"`" + `
	Name string ` + "`json:\"Name\" column:\"name\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(Entity) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_DO(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildStruct("UserInfoDO", columns, true, "", "column", "upperCamel", testFieldMap(columns))
	want := `type UserInfoDO struct {
	ID *int ` + "`json:\"Id\" column:\"id\"`" + `
	Name *string ` + "`json:\"Name\" column:\"name\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(DO) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_TimeField(t *testing.T) {
	columns := []Column{
		{Name: "created_at", Type: "datetime"},
	}
	got := buildStruct("TestEntity", columns, false, "", "column", "upperCamel", testFieldMap(columns))
	want := `type TestEntity struct {
	CreatedAt time.Time ` + "`json:\"CreatedAt\" column:\"created_at\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(time field) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_WithComment(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
	}
	comment := "UserInfoEntity test_db.user_info 表 entity 结构体，常用于数据库读取操作。"
	got := buildStruct("UserInfoEntity", columns, false, comment, "column", "upperCamel", testFieldMap(columns))
	want := `// UserInfoEntity test_db.user_info 表 entity 结构体，常用于数据库读取操作。
type UserInfoEntity struct {
	ID int ` + "`json:\"Id\" column:\"id\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(comment) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_WithComment_DO(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
	}
	comment := "UserInfoDO test_db.user_info 表 do 结构体，常用于数据库写入操作。"
	got := buildStruct("UserInfoDO", columns, true, comment, "column", "upperCamel", testFieldMap(columns))
	want := `// UserInfoDO test_db.user_info 表 do 结构体，常用于数据库写入操作。
type UserInfoDO struct {
	ID *int ` + "`json:\"Id\" column:\"id\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(comment DO) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_CustomTagName(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildStruct("UserInfoEntity", columns, false, "", "db", "upperCamel", testFieldMap(columns))
	want := `type UserInfoEntity struct {
	ID int ` + "`json:\"Id\" db:\"id\"`" + `
	Name string ` + "`json:\"Name\" db:\"name\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(custom tag) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_WithColumnComment(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int", Comment: "用户ID"},
		{Name: "name", Type: "string"},
	}
	got := buildStruct("UserInfoEntity", columns, false, "", "column", "upperCamel", testFieldMap(columns))
	want := `type UserInfoEntity struct {
	ID int ` + "`json:\"Id\" column:\"id\" description:\"用户ID\"`" + `
	Name string ` + "`json:\"Name\" column:\"name\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(column comment) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_WithJSONTag(t *testing.T) {
	columns := []Column{
		{Name: "user_id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildStruct("UserInfoEntity", columns, false, "", "column", "upperCamel", testFieldMap(columns))
	want := `type UserInfoEntity struct {
	UserID int ` + "`json:\"UserId\" column:\"user_id\"`" + `
	Name string ` + "`json:\"Name\" column:\"name\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(json tag) =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildStruct_WithAllTags(t *testing.T) {
	columns := []Column{
		{Name: "user_id", Type: "int", Comment: "用户ID"},
		{Name: "name", Type: "string", Comment: "姓名"},
	}
	got := buildStruct("UserInfoEntity", columns, false, "", "db", "upperCamel", testFieldMap(columns))
	want := `type UserInfoEntity struct {
	UserID int ` + "`json:\"UserId\" db:\"user_id\" description:\"用户ID\"`" + `
	Name string ` + "`json:\"Name\" db:\"name\" description:\"姓名\"`" + `
}`
	if got != want {
		t.Errorf("buildStruct(all tags) =\n%s\nwant:\n%s", got, want)
	}
}

// ======================== buildToDOMethod ========================

func TestBuildToDOMethod(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildToDOMethod("UserInfoEntity", "UserInfoDO", columns, testFieldMap(columns))
	want := `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	var d *UserInfoDO
	if len(userInfoDO) > 0 && userInfoDO[0] != nil {
		d = userInfoDO[0]
	} else {
		d = &UserInfoDO{}
	}
	d.ID = &e.ID
	d.Name = &e.Name
	return d
}`
	if got != want {
		t.Errorf("buildToDOMethod() =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildToDOMethod_SingleColumn(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
	}
	got := buildToDOMethod("OrderEntity", "OrderDO", columns, testFieldMap(columns))
	want := `func (e *OrderEntity) ToDO(orderDO ...*OrderDO) *OrderDO {
	var d *OrderDO
	if len(orderDO) > 0 && orderDO[0] != nil {
		d = orderDO[0]
	} else {
		d = &OrderDO{}
	}
	d.ID = &e.ID
	return d
}`
	if got != want {
		t.Errorf("buildToDOMethod(single) =\n%s\nwant:\n%s", got, want)
	}
}

// ======================== buildToEntityMethod ========================

func TestBuildToEntityMethod(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}
	got := buildToEntityMethod("UserInfoEntity", "UserInfoDO", columns, testFieldMap(columns))
	want := `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	var e *UserInfoEntity
	if len(userInfoEntity) > 0 && userInfoEntity[0] != nil {
		e = userInfoEntity[0]
	} else {
		e = &UserInfoEntity{}
	}
	if d.ID != nil {
		e.ID = *d.ID
	}
	if d.Name != nil {
		e.Name = *d.Name
	}
	return e
}`
	if got != want {
		t.Errorf("buildToEntityMethod() =\n%s\nwant:\n%s", got, want)
	}
}

func TestBuildToEntityMethod_SingleColumn(t *testing.T) {
	columns := []Column{
		{Name: "id", Type: "int"},
	}
	got := buildToEntityMethod("OrderEntity", "OrderDO", columns, testFieldMap(columns))
	want := `func (d *OrderDO) ToEntity(orderEntity ...*OrderEntity) *OrderEntity {
	var e *OrderEntity
	if len(orderEntity) > 0 && orderEntity[0] != nil {
		e = orderEntity[0]
	} else {
		e = &OrderEntity{}
	}
	if d.ID != nil {
		e.ID = *d.ID
	}
	return e
}`
	if got != want {
		t.Errorf("buildToEntityMethod(single) =\n%s\nwant:\n%s", got, want)
	}
}

// ======================== packageName ========================

func TestPackageName(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{filepath.Join("path", "to", "test"), "test"},
		{filepath.Join("path", "to", "model"), "model"},
		{".", "main"},
		{"", "main"},
	}
	for _, c := range cases {
		got := packageName(c.input)
		if got != c.want {
			t.Errorf("packageName(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ======================== writeOrReplaceStruct ========================

// writeOrReplaceStruct 文件不存在时创建新文件
func TestWriteOrReplaceStruct_NewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "package test") {
		t.Errorf("output missing package declaration:\n%s", got)
	}
	if !strings.Contains(got, "type UserInfoEntity struct") {
		t.Errorf("output missing Entity struct:\n%s", got)
	}
	if !strings.Contains(got, "type UserInfoDO struct") {
		t.Errorf("output missing DO struct:\n%s", got)
	}
	if !strings.Contains(got, "func (e *UserInfoEntity) ToDO") {
		t.Errorf("output missing ToDO method:\n%s", got)
	}
	if !strings.Contains(got, "func (d *UserInfoDO) ToEntity") {
		t.Errorf("output missing ToEntity method:\n%s", got)
	}
}

// writeOrReplaceStruct 替换已存在的生成代码，保留用户自定义代码
func TestWriteOrReplaceStruct_ReplaceGenerated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	// 初始文件：包含生成代码 + 用户自定义代码
	initial := `package test

type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}

func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}

type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}

func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}

func Helper() {
}
`
	if err := os.WriteFile(filePath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 新的生成代码（字段变更：增加 Name）
	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
	Name string ` + "`column:\"name\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
	Name *string ` + "`column:\"name\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// 用户代码应保留
	if !strings.Contains(got, "func Helper()") {
		t.Errorf("user code Helper() should be preserved:\n%s", got)
	}
	// 新的生成代码应包含 Name 字段
	if !strings.Contains(got, "Name string") {
		t.Errorf("new Entity field Name should be present:\n%s", got)
	}
	if !strings.Contains(got, "Name *string") {
		t.Errorf("new DO field Name should be present:\n%s", got)
	}
	// 不应出现重复的生成代码
	if strings.Count(got, "type UserInfoEntity struct") != 1 {
		t.Errorf("Entity struct should appear exactly once:\n%s", got)
	}
	if strings.Count(got, "type UserInfoDO struct") != 1 {
		t.Errorf("DO struct should appear exactly once:\n%s", got)
	}
}

// writeOrReplaceStruct Entity/DO 自定义方法分别放在各自生成代码后面
func TestWriteOrReplaceStruct_CustomMethodPlacement(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	// 初始文件：自定义方法散落在各处
	initial := `package test

type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}

func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}

type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}

func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}

func (e *UserInfoEntity) GetID() int {
	return e.ID
}

func (d *UserInfoDO) GetID() int {
	if d.ID != nil {
		return *d.ID
	}
	return 0
}

func Helper() {
}
`
	if err := os.WriteFile(filePath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// Entity 自定义方法 GetID 应在 ToDO 之后、DO 结构体之前
	toDOIdx := strings.Index(got, "func (e *UserInfoEntity) ToDO")
	entityGetIDIdx := strings.Index(got, "func (e *UserInfoEntity) GetID")
	doStructIdx := strings.Index(got, "type UserInfoDO struct")

	if toDOIdx == -1 || entityGetIDIdx == -1 || doStructIdx == -1 {
		t.Fatalf("missing expected code in output:\n%s", got)
	}
	if !(toDOIdx < entityGetIDIdx && entityGetIDIdx < doStructIdx) {
		t.Errorf("Entity GetID should be between ToDO and DO struct:\n%s", got)
	}

	// DO 自定义方法 GetID 应在 ToEntity 之后、Helper 之前
	toEntityIdx := strings.Index(got, "func (d *UserInfoDO) ToEntity")
	doGetIDIdx := strings.Index(got, "func (d *UserInfoDO) GetID")
	helperIdx := strings.Index(got, "func Helper()")

	if toEntityIdx == -1 || doGetIDIdx == -1 || helperIdx == -1 {
		t.Fatalf("missing expected code in output:\n%s", got)
	}
	if !(toEntityIdx < doGetIDIdx && doGetIDIdx < helperIdx) {
		t.Errorf("DO GetID should be between ToEntity and Helper:\n%s", got)
	}
}

// writeOrReplaceStruct 保留 import 声明
func TestWriteOrReplaceStruct_PreserveImports(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	initial := `package test

import (
	"time"
)

type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
	CreatedAt time.Time ` + "`column:\"created_at\"`" + `
}

func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}

type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
	CreatedAt *time.Time ` + "`column:\"created_at\"`" + `
}

func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}
`
	if err := os.WriteFile(filePath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
	CreatedAt time.Time ` + "`column:\"created_at\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
	CreatedAt *time.Time ` + "`column:\"created_at\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, `"time"`) {
		t.Errorf("import should be preserved:\n%s", got)
	}
}

// writeOrReplaceStruct 保留前置 Doc 注释
func TestWriteOrReplaceStruct_PreserveDocComments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	initial := `package test

type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}

func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}

type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}

func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}

// HelperFunc is a user function
func HelperFunc() {
}
`
	if err := os.WriteFile(filePath, []byte(initial), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)
	if !strings.Contains(got, "// HelperFunc is a user function") {
		t.Errorf("Doc comment should be preserved:\n%s", got)
	}
}

// writeOrReplaceStruct 文件存在但内容为空时按新建处理
func TestWriteOrReplaceStruct_EmptyFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	filePath := filepath.Join(dir, "user_info.go")

	// 创建空文件
	if err := os.WriteFile(filePath, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	entityCode := `type UserInfoEntity struct {
	ID int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	return nil
}`
	doCode := `type UserInfoDO struct {
	ID *int ` + "`column:\"id\"`" + `
}` + "\n\n" + `func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	return nil
}`

	err := writeOrReplaceStruct(filePath, "UserInfoEntity", entityCode, "UserInfoDO", doCode)
	if err != nil {
		t.Fatalf("writeOrReplaceStruct failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// 应包含 package 声明
	if !strings.Contains(got, "package test") {
		t.Errorf("output missing package declaration:\n%s", got)
	}
	// 应包含 Entity 结构体
	if !strings.Contains(got, "type UserInfoEntity struct") {
		t.Errorf("output missing Entity struct:\n%s", got)
	}
	// 应包含 DO 结构体
	if !strings.Contains(got, "type UserInfoDO struct") {
		t.Errorf("output missing DO struct:\n%s", got)
	}
}

// ======================== Generate（端到端） ========================

// Generate 完整流程：从零创建文件，再次运行验证幂等性
func TestGenerate_CreateNewFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}

	Generate(dir, "test_db", "user_info", "column", "upperCamel", columns)

	filePath := filepath.Join(dir, "user_info.go")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// 验证结构体注释
	if !strings.Contains(got, "// UserInfoEntity test_db.user_info 表 entity 结构体，常用于数据库读取操作。") {
		t.Errorf("missing Entity comment:\n%s", got)
	}
	if !strings.Contains(got, "// UserInfoDO test_db.user_info 表 do 结构体，常用于数据库写入操作。") {
		t.Errorf("missing DO comment:\n%s", got)
	}
	// 验证 package 声明
	if !strings.HasPrefix(got, "package test\n") {
		t.Errorf("output should start with package declaration:\n%s", got)
	}
	// 验证 Entity 结构体
	if !strings.Contains(got, "type UserInfoEntity struct") {
		t.Errorf("missing Entity struct:\n%s", got)
	}
	if !strings.Contains(got, "ID int") {
		t.Errorf("missing ID field:\n%s", got)
	}
	if !strings.Contains(got, "Name string") {
		t.Errorf("missing Name field:\n%s", got)
	}
	// 验证 DO 结构体
	if !strings.Contains(got, "type UserInfoDO struct") {
		t.Errorf("missing DO struct:\n%s", got)
	}
	if !strings.Contains(got, "ID *int") {
		t.Errorf("missing ID pointer field:\n%s", got)
	}
	if !strings.Contains(got, "Name *string") {
		t.Errorf("missing Name pointer field:\n%s", got)
	}
	// 验证转换方法
	if !strings.Contains(got, "func (e *UserInfoEntity) ToDO") {
		t.Errorf("missing ToDO method:\n%s", got)
	}
	if !strings.Contains(got, "func (d *UserInfoDO) ToEntity") {
		t.Errorf("missing ToEntity method:\n%s", got)
	}
}

// Generate 二次运行：保留用户自定义代码，替换生成代码
func TestGenerate_ReplacePreservesUserCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}

	// 第一次生成
	Generate(dir, "test_db", "user_info", "column", "upperCamel", columns)

	filePath := filepath.Join(dir, "user_info.go")

	// 添加用户自定义代码
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	original := string(content)
	// 在文件末尾追加用户自定义方法
	modified := strings.TrimSuffix(original, "\n") + `

func (e *UserInfoEntity) GetID() int {
	return e.ID
}

func (d *UserInfoDO) GetID() int {
	if d.ID != nil {
		return *d.ID
	}
	return 0
}

func Helper() {
}
`
	if err := os.WriteFile(filePath, []byte(modified), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 增加一列，重新生成
	columns2 := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
		{Name: "email", Type: "string"},
	}
	Generate(dir, "test_db", "user_info", "column", "upperCamel", columns2)

	content, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// 新字段应出现
	if !strings.Contains(got, "Email string") {
		t.Errorf("new Entity field Email should be present:\n%s", got)
	}
	if !strings.Contains(got, "Email *string") {
		t.Errorf("new DO field Email should be present:\n%s", got)
	}
	// 用户自定义代码应保留
	if !strings.Contains(got, "func (e *UserInfoEntity) GetID()") {
		t.Errorf("Entity GetID should be preserved:\n%s", got)
	}
	if !strings.Contains(got, "func (d *UserInfoDO) GetID()") {
		t.Errorf("DO GetID should be preserved:\n%s", got)
	}
	if !strings.Contains(got, "func Helper()") {
		t.Errorf("Helper should be preserved:\n%s", got)
	}
	// 不应出现重复的生成代码
	if strings.Count(got, "type UserInfoEntity struct") != 1 {
		t.Errorf("Entity struct should appear exactly once:\n%s", got)
	}
	if strings.Count(got, "type UserInfoDO struct") != 1 {
		t.Errorf("DO struct should appear exactly once:\n%s", got)
	}
}

// Generate 二次运行：Entity/DO 自定义方法分组放置
func TestGenerate_CustomMethodGrouping(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test")
	columns := []Column{
		{Name: "id", Type: "int"},
		{Name: "name", Type: "string"},
	}

	// 第一次生成
	Generate(dir, "test_db", "user_info", "column", "upperCamel", columns)

	filePath := filepath.Join(dir, "user_info.go")

	// 添加用户自定义代码（Entity 方法和 DO 方法和普通函数）
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	original := string(content)
	modified := strings.TrimSuffix(original, "\n") + `

func (e *UserInfoEntity) GetID() int {
	return e.ID
}

func (d *UserInfoDO) GetID() int {
	if d.ID != nil {
		return *d.ID
	}
	return 0
}

func Helper() {
}
`
	if err := os.WriteFile(filePath, []byte(modified), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// 重新生成
	Generate(dir, "test_db", "user_info", "column", "upperCamel", columns)

	content, err = os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	got := string(content)

	// 验证顺序：ToDO < EntityGetId < DOStruct < ToEntity < DOGetId < Helper
	toDOIdx := strings.Index(got, "func (e *UserInfoEntity) ToDO")
	entityGetIdIdx := strings.Index(got, "func (e *UserInfoEntity) GetID")
	doStructIdx := strings.Index(got, "type UserInfoDO struct")
	toEntityIdx := strings.Index(got, "func (d *UserInfoDO) ToEntity")
	doGetIdIdx := strings.Index(got, "func (d *UserInfoDO) GetID")
	helperIdx := strings.Index(got, "func Helper()")

	if toDOIdx == -1 || entityGetIdIdx == -1 || doStructIdx == -1 ||
		toEntityIdx == -1 || doGetIdIdx == -1 || helperIdx == -1 {
		t.Fatalf("missing expected code:\n%s", got)
	}
	if !(toDOIdx < entityGetIdIdx && entityGetIdIdx < doStructIdx) {
		t.Errorf("Entity GetID should be between ToDO and DO struct:\n%s", got)
	}
	if !(toEntityIdx < doGetIdIdx && doGetIdIdx < helperIdx) {
		t.Errorf("DO GetID should be between ToEntity and Helper:\n%s", got)
	}
}
