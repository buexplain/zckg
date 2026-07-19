package zchttp

import (
	"context"
	"encoding/json"
	"testing"
)

// oaCreateUserReq 覆盖 OpenAPIMeta 嵌入、required、default、指针等场景
type oaCreateUserReq struct {
	OpenAPIMeta `tags:"User Management/Account" summary:"创建用户" description:"创建一个新的用户账户"`
	Name        string       `json:"name" required:"true" description:"真实名字"`
	Nickname    string       `json:"nickname" default:"" description:"昵称"`
	Age         *int         `json:"age" description:"年龄"`
	MailboxList []*oaMailbox `json:"mailboxList" required:"true" description:"可用的邮箱列表"`
	Company     *OaCompany   `json:"company" description:"供职的公司信息"`
	School      OaSchool     `json:"school" required:"true" description:"就读的学校信息"`
	Family      []OaFamily   `json:"family" description:"家庭成员信息"`
}

type OaFamily struct {
	Name       string       `json:"name" required:"true" description:"家庭成员名称"`
	Relation   string       `json:"relation" required:"true" description:"家庭成员关系"`
	IsMinor    *bool        `json:"isMinor" description:"是否未成年" default:"true"`
	BankList   []OaBank     `json:"bankList" description:"家庭成员银行信息"`
	HealthInfo OaHealthInfo `json:"healthInfo" description:"家庭成员健康信息"`
}

type OaHealthInfo struct {
	Height int `json:"height" required:"true" description:"身高"`
	Weight int `json:"weight" required:"true" description:"体重"`
}

type OaBank struct {
	Name    string `json:"name" required:"true" description:"银行名称"`
	Account string `json:"account" required:"true" description:"银行账户"`
}

type OaSchool struct {
	Name    string `json:"name" required:"true" description:"学校名称"`
	Address string `json:"address" description:"学校地址"`
}

type OaCompany struct {
	Name    string  `json:"name" required:"true" description:"公司名称"`
	Address string  `json:"address" description:"公司地址"`
	Phone   string  `json:"phone"  description:"公司电话"`
	Email   *string `json:"email" required:"false" description:"公司邮箱"`
}

type oaMailbox struct {
	Email    string  `json:"email" required:"true"`
	Category *string `json:"category" default:"工作邮箱" example:"工作邮箱|个人邮箱|学校邮箱"`
}

type oaCreateUserRes struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// oaListReq 覆盖 query 参数、ignore、default、required、指针、切片
type oaListReq struct {
	Keyword string   `json:"keyword" default:""`
	Email   string   `json:"email" required:"true"`
	Name    string   `json:"name"`
	Avatar  *string  `json:"avatar"`
	Tags    []string `json:"tags"`
	Hidden  string   `json:"hidden" ignore:"true"`
}

type oaListRes struct {
	Total int `json:"total"`
}

func oaCreateUser(_ context.Context, _ oaCreateUserReq) (oaCreateUserRes, error) {
	return oaCreateUserRes{}, nil
}

func oaList(_ context.Context, _ oaListReq) (oaListRes, error) {
	return oaListRes{}, nil
}

// ---- 嵌套结构体与嵌套结构体切片 OpenAPI 文档测试 ----

type oaAddress struct {
	City   string `json:"city" required:"true"`
	Street string `json:"street" description:"街道名称"`
}

type oaNestedReq struct {
	OpenAPIMeta `tags:"Nested" summary:"嵌套结构体请求"`
	Name        string    `json:"name" required:"true"`
	Addr        oaAddress `json:"addr"`
	Tags        []string  `json:"tags" default:"go,test"`
}

type oaNestedRes struct {
	Name string `json:"name"`
}

func oaNestedHandler(_ context.Context, _ oaNestedReq) (oaNestedRes, error) {
	return oaNestedRes{}, nil
}

type oaNestedSliceReq struct {
	OpenAPIMeta `tags:"NestedSlice" summary:"嵌套结构体切片请求"`
	Name        string      `json:"name" required:"true"`
	Addresses   []oaAddress `json:"addresses"`
}

type oaNestedSliceRes struct {
	Name string `json:"name"`
}

func oaNestedSliceHandler(_ context.Context, _ oaNestedSliceReq) (oaNestedSliceRes, error) {
	return oaNestedSliceRes{}, nil
}

// TestGenerateOpenAPI_NestedStruct 验证嵌套结构体在 OpenAPI 文档中正确生成独立 schema
func TestGenerateOpenAPI_NestedStruct(t *testing.T) {
	r := NewRouter()
	r.POST("/nested", oaNestedHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Nested Test", Version: "1.0.0"})

	// ---- 验证 schemas 中包含嵌套结构体的独立 schema ----
	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// 嵌套结构体 oaAddress 应当有独立的 schema（而非内联展开）
	addrSchema, ok := schemas["oaAddress"]
	if !ok {
		t.Fatalf("oaAddress schema missing from components/schemas")
	}
	addrObj := addrSchema.(map[string]any)
	if addrObj["type"] != "object" {
		t.Errorf("oaAddress type = %v, want object", addrObj["type"])
	}
	addrProps := addrObj["properties"].(map[string]any)
	if _, ok := addrProps["city"]; !ok {
		t.Errorf("oaAddress.properties.city missing")
	}
	if _, ok := addrProps["street"]; !ok {
		t.Errorf("oaAddress.properties.street missing")
	}
	// required 字段
	addrRequired := addrObj["required"].([]any)
	if len(addrRequired) != 1 || addrRequired[0] != "city" {
		t.Errorf("oaAddress.required = %v, want [city]", addrRequired)
	}

	// oaNestedReq 中的 addr 字段应引用 $ref
	reqSchema, ok := schemas["oaNestedReq"]
	if !ok {
		t.Fatalf("oaNestedReq schema missing")
	}
	reqObj := reqSchema.(map[string]any)
	reqProps := reqObj["properties"].(map[string]any)
	addrField := reqProps["addr"].(map[string]any)
	if ref, ok := addrField["$ref"]; !ok || ref != "#/components/schemas/oaAddress" {
		t.Errorf("oaNestedReq.properties.addr.$ref = %v, want #/components/schemas/oaAddress", addrField)
	}

	// ---- 验证 POST requestBody 的 schema 引用 ----
	paths := doc["paths"].(map[string]any)
	nestedItem := paths["/nested"].(map[string]any)
	post := nestedItem["post"].(map[string]any)
	reqBody := post["requestBody"].(map[string]any)
	content := reqBody["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	bodySchema := jsonContent["schema"].(map[string]any)
	if ref, ok := bodySchema["$ref"]; !ok || ref != "#/components/schemas/oaNestedReq" {
		t.Errorf("requestBody.$ref = %v, want #/components/schemas/oaNestedReq", bodySchema)
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// TestGenerateOpenAPI_NestedStructSlice 验证嵌套结构体切片在 OpenAPI 文档中正确生成为 array($ref)
func TestGenerateOpenAPI_NestedStructSlice(t *testing.T) {
	r := NewRouter()
	r.POST("/nested-slice", oaNestedSliceHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Nested Slice Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// oaAddress schema 必须存在（被切片元素引用）
	if _, ok := schemas["oaAddress"]; !ok {
		t.Fatalf("oaAddress schema should exist (referenced by array items)")
	}

	// oaNestedSliceReq 的 addresses 字段应为 array，items 引用 oaAddress
	reqSchema := schemas["oaNestedSliceReq"].(map[string]any)
	reqProps := reqSchema["properties"].(map[string]any)
	addrsField := reqProps["addresses"].(map[string]any)
	if addrsField["type"] != "array" {
		t.Errorf("addresses.type = %v, want array", addrsField["type"])
	}
	items := addrsField["items"].(map[string]any)
	if ref, ok := items["$ref"]; !ok || ref != "#/components/schemas/oaAddress" {
		t.Errorf("addresses.items.$ref = %v, want #/components/schemas/oaAddress", items)
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// ---- 嵌套结构体指针与嵌套结构体指针切片 OpenAPI 文档测试 ----

type oaNestedPtrReq struct {
	OpenAPIMeta `tags:"NestedPtr" summary:"嵌套结构体指针请求"`
	Name        string     `json:"name" required:"true"`
	Addr        *oaAddress `json:"addr" required:"true"`
}

type oaNestedPtrRes struct {
	Name string `json:"name"`
}

func oaNestedPtrHandler(_ context.Context, _ oaNestedPtrReq) (oaNestedPtrRes, error) {
	return oaNestedPtrRes{}, nil
}

// TestGenerateOpenAPI_NestedStructPtr 验证嵌套结构体指针的 OpenAPI 文档生成
// 指针类型应包裹 allOf + nullable，且 required 标签应在父级正确呈现
func TestGenerateOpenAPI_NestedStructPtr(t *testing.T) {
	r := NewRouter()
	r.POST("/nested-ptr", oaNestedPtrHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Nested Ptr Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// oaAddress 嵌套类型的独立 schema 必须存在
	addrSchema, ok := schemas["oaAddress"]
	if !ok {
		t.Fatalf("oaAddress schema missing")
	}
	addrObj := addrSchema.(map[string]any)
	addrRequired := addrObj["required"].([]any)
	if len(addrRequired) != 1 || addrRequired[0] != "city" {
		t.Errorf("oaAddress.required = %v, want [city]", addrRequired)
	}

	// oaNestedPtrReq 的 required 列表应包含 addr（指针字段标记了 required:"true"）
	reqSchema := schemas["oaNestedPtrReq"].(map[string]any)
	reqRequired := reqSchema["required"].([]any)
	foundAddr := false
	for _, r := range reqRequired {
		if r == "addr" {
			foundAddr = true
			break
		}
	}
	if !foundAddr {
		t.Errorf("oaNestedPtrReq.required = %v, should contain 'addr'", reqRequired)
	}

	// addr 字段是 *oaAddress 指针，应生成 nullable + allOf [$ref] 结构
	reqProps := reqSchema["properties"].(map[string]any)
	addrField := reqProps["addr"].(map[string]any)
	if addrField["nullable"] != true {
		t.Errorf("addr.nullable = %v, want true", addrField["nullable"])
	}
	allOf, ok := addrField["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("addr.allOf = %v, want [{$ref}]", addrField)
	}
	allOfItem := allOf[0].(map[string]any)
	if ref, ok := allOfItem["$ref"]; !ok || ref != "#/components/schemas/oaAddress" {
		t.Errorf("addr.allOf[0].$ref = %v, want #/components/schemas/oaAddress", allOfItem)
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

type oaNestedPtrSliceReq struct {
	OpenAPIMeta `tags:"NestedPtrSlice" summary:"嵌套结构体指针切片请求"`
	Name        string       `json:"name" required:"true"`
	Addresses   []*oaAddress `json:"addresses" required:"true"`
}

type oaNestedPtrSliceRes struct {
	Name string `json:"name"`
}

func oaNestedPtrSliceHandler(_ context.Context, _ oaNestedPtrSliceReq) (oaNestedPtrSliceRes, error) {
	return oaNestedPtrSliceRes{}, nil
}

// TestGenerateOpenAPI_NestedStructPtrSlice 验证嵌套结构体指针切片的 OpenAPI 文档生成
// 指针切片元素应包裹 allOf + nullable，且 required 标签应在父级正确呈现
func TestGenerateOpenAPI_NestedStructPtrSlice(t *testing.T) {
	r := NewRouter()
	r.POST("/nested-ptr-slice", oaNestedPtrSliceHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Nested Ptr Slice Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// oaAddress 嵌套类型的独立 schema 必须存在
	if _, ok := schemas["oaAddress"]; !ok {
		t.Fatalf("oaAddress schema should exist (referenced by array items)")
	}

	// oaNestedPtrSliceReq 的 required 列表应包含 addresses
	reqSchema := schemas["oaNestedPtrSliceReq"].(map[string]any)
	reqRequired := reqSchema["required"].([]any)
	foundAddrs := false
	for _, r := range reqRequired {
		if r == "addresses" {
			foundAddrs = true
			break
		}
	}
	if !foundAddrs {
		t.Errorf("oaNestedPtrSliceReq.required = %v, should contain 'addresses'", reqRequired)
	}

	// addresses 字段是 []*oaAddress，应为 array 类型
	reqProps := reqSchema["properties"].(map[string]any)
	addrsField := reqProps["addresses"].(map[string]any)
	if addrsField["type"] != "array" {
		t.Errorf("addresses.type = %v, want array", addrsField["type"])
	}

	// items 是 *oaAddress 指针，应生成 nullable + allOf [$ref]
	items := addrsField["items"].(map[string]any)
	if items["nullable"] != true {
		t.Errorf("addresses.items.nullable = %v, want true", items["nullable"])
	}
	allOf, ok := items["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("addresses.items.allOf = %v, want [{$ref}]", items)
	}
	allOfItem := allOf[0].(map[string]any)
	if ref, ok := allOfItem["$ref"]; !ok || ref != "#/components/schemas/oaAddress" {
		t.Errorf("addresses.items.allOf[0].$ref = %v, want #/components/schemas/oaAddress", allOfItem)
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// ---- 结构体自引用（递归嵌套）OpenAPI 文档测试 ----

// Category 商品分类：Children 自引用形成树形嵌套
type Category struct {
	ID       int         `json:"id" required:"true" description:"分类ID"`
	Name     string      `json:"name" required:"true" description:"分类名称"`
	ParentID *int        `json:"parentId" description:"父分类ID"`
	Children []*Category `json:"children" description:"子分类列表"`
}

type categoryListReq struct {
	OpenAPIMeta `tags:"Category" summary:"获取分类树" description:"返回完整的商品分类树"`
	RootID      *int `json:"rootId" default:"0" description:"根分类ID，0表示一级分类"`
}

type categoryListRes struct {
	Categories []Category `json:"categories" description:"分类列表"`
}

func categoryList(_ context.Context, _ categoryListReq) (categoryListRes, error) {
	return categoryListRes{}, nil
}

// TestGenerateOpenAPI_SelfRef 验证自引用结构体的 OpenAPI 文档生成
func TestGenerateOpenAPI_SelfRef(t *testing.T) {
	r := NewRouter()
	r.GET("/categories", categoryList)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "SelfRef Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// Category 独立 schema 必须存在（无无限递归）
	catSchema, ok := schemas["Category"]
	if !ok {
		t.Fatalf("Category schema missing — possible infinite recursion?")
	}
	catObj := catSchema.(map[string]any)
	if catObj["type"] != "object" {
		t.Errorf("Category.type = %v, want object", catObj["type"])
	}

	catProps := catObj["properties"].(map[string]any)
	if _, ok := catProps["id"]; !ok {
		t.Errorf("Category.properties.id missing")
	}
	if _, ok := catProps["name"]; !ok {
		t.Errorf("Category.properties.name missing")
	}
	if _, ok := catProps["parentId"]; !ok {
		t.Errorf("Category.properties.parentId missing")
	}

	// parentId: *int → integer + nullable
	pidField := catProps["parentId"].(map[string]any)
	if pidField["type"] != "integer" {
		t.Errorf("Category.parentId.type = %v, want integer", pidField["type"])
	}
	if pidField["nullable"] != true {
		t.Errorf("Category.parentId.nullable = %v, want true", pidField["nullable"])
	}

	// 关键：children 自引用字段
	childrenField := catProps["children"].(map[string]any)
	if childrenField["type"] != "array" {
		t.Errorf("Category.children.type = %v, want array", childrenField["type"])
	}
	if childrenField["description"] != "子分类列表" {
		t.Errorf("Category.children.description = %v, want 子分类列表", childrenField["description"])
	}

	// children.items: []*Category → nullable + allOf[$ref: Category]（自引用）
	childrenItems := childrenField["items"].(map[string]any)
	if childrenItems["nullable"] != true {
		t.Errorf("Category.children.items.nullable = %v, want true", childrenItems["nullable"])
	}
	childrenAllOf, ok := childrenItems["allOf"].([]any)
	if !ok || len(childrenAllOf) != 1 {
		t.Fatalf("Category.children.items.allOf = %v, want [{$ref}]", childrenItems)
	}
	allOfItem := childrenAllOf[0].(map[string]any)
	if ref, ok := allOfItem["$ref"]; !ok || ref != "#/components/schemas/Category" {
		t.Errorf("Category.children.items.allOf[0].$ref = %v, want #/components/schemas/Category (self-ref)", allOfItem)
	}

	// required: id, name
	catRequired := catObj["required"].([]any)
	if len(catRequired) != 2 {
		t.Errorf("Category.required = %v, want [id, name]", catRequired)
	}

	// 响应中 Categories 通过 categoryListRes schema 间接引用 Category
	resSchema, ok := schemas["categoryListRes"]
	if !ok {
		t.Fatalf("categoryListRes schema missing")
	}
	resObj := resSchema.(map[string]any)
	resProps := resObj["properties"].(map[string]any)
	catsField := resProps["categories"].(map[string]any)
	if catsField["type"] != "array" {
		t.Errorf("categoryListRes.categories.type = %v, want array", catsField["type"])
	}
	catsItems := catsField["items"].(map[string]any)
	if ref, ok := catsItems["$ref"]; !ok || ref != "#/components/schemas/Category" {
		t.Errorf("categoryListRes.categories.items.$ref = %v, want #/components/schemas/Category", catsItems)
	}

	// 可序列化（确保无循环引用）
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// TestGenerateOpenAPI_Routing 验证路由元信息：openapi 版本、paths、tags、summary、requestBody、responses、schema 存在性
func TestGenerateOpenAPI_Routing(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	if doc["openapi"] != "3.0.3" {
		t.Fatalf("openapi version = %v, want 3.0.3", doc["openapi"])
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths missing or wrong type")
	}
	usersItem, ok := paths["/users"].(map[string]any)
	if !ok {
		t.Fatalf("/users path missing")
	}

	// POST /users 的 tags、summary、requestBody、responses
	post, ok := usersItem["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing")
	}
	tags, ok := post["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "User Management" || tags[1] != "Account" {
		t.Fatalf("tags = %v, want [User Management Account]", post["tags"])
	}
	if post["summary"] != "创建用户" {
		t.Fatalf("summary = %v, want 创建用户", post["summary"])
	}
	if _, ok := post["requestBody"]; !ok {
		t.Fatalf("requestBody missing for POST")
	}
	postResp, ok := post["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for POST")
	}
	if _, ok := postResp["200"]; !ok {
		t.Fatalf("POST should respond 200, got %v", postResp)
	}

	// 验证 schemas 存在
	comps, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing")
	}
	schemas, ok := comps["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("schemas missing")
	}
	if _, ok := schemas["oaCreateUserReq"]; !ok {
		t.Errorf("oaCreateUserReq schema missing")
	}
	if _, ok := schemas["Response_oaCreateUserRes"]; !ok {
		t.Errorf("Response_oaCreateUserRes schema missing")
	}

	// 可序列化
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// TestGenerateOpenAPI_QueryRequired 验证 GET query 参数的 required 判定规则：
// ignore → 不出现、有 default → optional、required:"true" → required、指针/切片 → optional
func TestGenerateOpenAPI_QueryRequired(t *testing.T) {
	r := NewRouter()
	r.GET("/users", oaList)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	paths := doc["paths"].(map[string]any)
	usersItem := paths["/users"].(map[string]any)
	get, ok := usersItem["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing")
	}
	params, ok := get["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters missing for GET")
	}
	requiredByName := map[string]bool{}
	for _, p := range params {
		pm := p.(map[string]any)
		requiredByName[pm["name"].(string)] = pm["required"].(bool)
	}
	if _, seen := requiredByName["hidden"]; seen {
		t.Errorf("hidden should be ignored")
	}
	if requiredByName["keyword"] {
		t.Errorf("keyword has default, should be optional")
	}
	if !requiredByName["email"] {
		t.Errorf("email required:true, should be required")
	}
	if requiredByName["name"] {
		t.Errorf("name has no required tag, should be optional")
	}
	if requiredByName["avatar"] {
		t.Errorf("avatar is pointer, should be optional")
	}
	if requiredByName["tags"] {
		t.Errorf("tags is slice, should be optional")
	}
}

// TestGenerateOpenAPI_PtrSliceElementSchema 验证指针切片元素 oaMailbox 的独立 schema：
// default/example 传播、required 列表（有 default 的字段不进 required）
func TestGenerateOpenAPI_PtrSliceElementSchema(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	mailboxSchema, ok := schemas["oaMailbox"]
	if !ok {
		t.Fatalf("oaMailbox schema missing")
	}
	mailboxObj := mailboxSchema.(map[string]any)
	if mailboxObj["type"] != "object" {
		t.Errorf("oaMailbox.type = %v, want object", mailboxObj["type"])
	}
	mailboxProps := mailboxObj["properties"].(map[string]any)
	if _, ok := mailboxProps["email"]; !ok {
		t.Errorf("oaMailbox.properties.email missing")
	}
	catField := mailboxProps["category"].(map[string]any)
	if catField["type"] != "string" {
		t.Errorf("oaMailbox.category.type = %v, want string", catField["type"])
	}
	if catField["default"] != "工作邮箱" {
		t.Errorf("oaMailbox.category.default = %v, want 工作邮箱", catField["default"])
	}
	// example 中包含 |，coerceExample 对 string 类型原样保留
	if catField["example"] != "工作邮箱|个人邮箱|学校邮箱" {
		t.Errorf("oaMailbox.category.example = %v, want 工作邮箱|个人邮箱|学校邮箱", catField["example"])
	}
	// required 列表：仅 email（category 有 default 不进入 required）
	mailboxRequired := mailboxObj["required"].([]any)
	if len(mailboxRequired) != 1 || mailboxRequired[0] != "email" {
		t.Errorf("oaMailbox.required = %v, want [email]", mailboxRequired)
	}
}

// TestGenerateOpenAPI_PointerNestedSchema 验证指针嵌套 OaCompany 的独立 schema：
// string 字段带 description、*string → nullable、required:"false" → 不进 required
func TestGenerateOpenAPI_PointerNestedSchema(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	companySchema, ok := schemas["OaCompany"]
	if !ok {
		t.Fatalf("OaCompany schema missing")
	}
	companyObj := companySchema.(map[string]any)
	if companyObj["type"] != "object" {
		t.Errorf("OaCompany.type = %v, want object", companyObj["type"])
	}
	companyProps := companyObj["properties"].(map[string]any)

	// name: required, with description
	nameField := companyProps["name"].(map[string]any)
	if nameField["type"] != "string" {
		t.Errorf("OaCompany.name.type = %v, want string", nameField["type"])
	}
	if nameField["description"] != "公司名称" {
		t.Errorf("OaCompany.name.description = %v, want 公司名称", nameField["description"])
	}

	// email: *string with required:"false" → nullable string, NOT in required
	emailField := companyProps["email"].(map[string]any)
	if emailField["type"] != "string" {
		t.Errorf("OaCompany.email.type = %v, want string", emailField["type"])
	}
	if emailField["nullable"] != true {
		t.Errorf("OaCompany.email.nullable = %v, want true", emailField["nullable"])
	}
	if emailField["description"] != "公司邮箱" {
		t.Errorf("OaCompany.email.description = %v, want 公司邮箱", emailField["description"])
	}

	// required: 仅 name（email 为 required:"false"，不进入）
	companyRequired := companyObj["required"].([]any)
	if len(companyRequired) != 1 || companyRequired[0] != "name" {
		t.Errorf("OaCompany.required = %v, want [name]", companyRequired)
	}
}

// TestGenerateOpenAPI_ValueNestedSchema 验证值类型嵌套 OaSchool 的独立 schema：
// 字段应有正确的 description、required 仅包含标记 required:"true" 且无 default 的字段
func TestGenerateOpenAPI_ValueNestedSchema(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	schoolSchema, ok := schemas["OaSchool"]
	if !ok {
		t.Fatalf("OaSchool schema missing")
	}
	schoolObj := schoolSchema.(map[string]any)
	if schoolObj["type"] != "object" {
		t.Errorf("OaSchool.type = %v, want object", schoolObj["type"])
	}
	schoolProps := schoolObj["properties"].(map[string]any)
	if _, ok := schoolProps["name"]; !ok {
		t.Errorf("OaSchool.properties.name missing")
	}
	if _, ok := schoolProps["address"]; !ok {
		t.Errorf("OaSchool.properties.address missing")
	}
	schoolNameField := schoolProps["name"].(map[string]any)
	if schoolNameField["description"] != "学校名称" {
		t.Errorf("OaSchool.name.description = %v, want 学校名称", schoolNameField["description"])
	}
	// required: 仅 name（address 无 required/default）
	schoolRequired := schoolObj["required"].([]any)
	if len(schoolRequired) != 1 || schoolRequired[0] != "name" {
		t.Errorf("OaSchool.required = %v, want [name]", schoolRequired)
	}
}

// TestGenerateOpenAPI_MultiLevelNestedSchema 验证多层递归嵌套的独立 schema：
// OaFamily（值类型切片元素）→ OaBank（值类型切片）+ OaHealthInfo（值类型）
// 覆盖：description 传播、bool default、required 传播、$ref 引用
func TestGenerateOpenAPI_MultiLevelNestedSchema(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// ---- OaFamily ----
	familySchema, ok := schemas["OaFamily"]
	if !ok {
		t.Fatalf("OaFamily schema missing")
	}
	familyObj := familySchema.(map[string]any)
	if familyObj["type"] != "object" {
		t.Errorf("OaFamily.type = %v, want object", familyObj["type"])
	}
	familyProps := familyObj["properties"].(map[string]any)
	if _, ok := familyProps["name"]; !ok {
		t.Errorf("OaFamily.properties.name missing")
	}
	if _, ok := familyProps["relation"]; !ok {
		t.Errorf("OaFamily.properties.relation missing")
	}
	if _, ok := familyProps["isMinor"]; !ok {
		t.Errorf("OaFamily.properties.isMinor missing")
	}
	if _, ok := familyProps["bankList"]; !ok {
		t.Errorf("OaFamily.properties.bankList missing")
	}
	if _, ok := familyProps["healthInfo"]; !ok {
		t.Errorf("OaFamily.properties.healthInfo missing")
	}

	// description 传播
	famNameField := familyProps["name"].(map[string]any)
	if famNameField["description"] != "家庭成员名称" {
		t.Errorf("OaFamily.name.description = %v, want 家庭成员名称", famNameField["description"])
	}
	famRelField := familyProps["relation"].(map[string]any)
	if famRelField["description"] != "家庭成员关系" {
		t.Errorf("OaFamily.relation.description = %v, want 家庭成员关系", famRelField["description"])
	}

	// isMinor: bool, default:"true" → 不进入 required
	isMinorField := familyProps["isMinor"].(map[string]any)
	if isMinorField["type"] != "boolean" {
		t.Errorf("OaFamily.isMinor.type = %v, want boolean", isMinorField["type"])
	}
	if isMinorField["default"] != true {
		t.Errorf("OaFamily.isMinor.default = %v, want true", isMinorField["default"])
	}
	if isMinorField["description"] != "是否未成年" {
		t.Errorf("OaFamily.isMinor.description = %v, want 是否未成年", isMinorField["description"])
	}

	// bankList: []OaBank → array, items $ref
	bankListField := familyProps["bankList"].(map[string]any)
	if bankListField["type"] != "array" {
		t.Errorf("OaFamily.bankList.type = %v, want array", bankListField["type"])
	}
	if bankListField["description"] != "家庭成员银行信息" {
		t.Errorf("OaFamily.bankList.description = %v, want 家庭成员银行信息", bankListField["description"])
	}
	blItems := bankListField["items"].(map[string]any)
	if ref, ok := blItems["$ref"]; !ok || ref != "#/components/schemas/OaBank" {
		t.Errorf("OaFamily.bankList.items.$ref = %v, want #/components/schemas/OaBank", blItems)
	}

	// healthInfo: OaHealthInfo 值类型 → $ref
	hiField := familyProps["healthInfo"].(map[string]any)
	if hiField["description"] != "家庭成员健康信息" {
		t.Errorf("OaFamily.healthInfo.description = %v, want 家庭成员健康信息", hiField["description"])
	}
	if ref, ok := hiField["$ref"]; !ok || ref != "#/components/schemas/OaHealthInfo" {
		t.Errorf("OaFamily.healthInfo.$ref = %v, want #/components/schemas/OaHealthInfo", hiField)
	}

	// required: 仅 name、relation（isMinor 有 default，bankList/healthInfo 无 required）
	familyRequired := familyObj["required"].([]any)
	if len(familyRequired) != 2 {
		t.Errorf("OaFamily.required = %v, want [name, relation]", familyRequired)
	}
	hasFamName, hasFamRel := false, false
	for _, r := range familyRequired {
		if r == "name" {
			hasFamName = true
		}
		if r == "relation" {
			hasFamRel = true
		}
	}
	if !hasFamName || !hasFamRel {
		t.Errorf("OaFamily.required = %v, must contain [name, relation]", familyRequired)
	}

	// ---- OaBank ----
	bankSchema, ok := schemas["OaBank"]
	if !ok {
		t.Fatalf("OaBank schema missing")
	}
	bankObj := bankSchema.(map[string]any)
	if bankObj["type"] != "object" {
		t.Errorf("OaBank.type = %v, want object", bankObj["type"])
	}
	bankProps := bankObj["properties"].(map[string]any)
	if _, ok := bankProps["name"]; !ok {
		t.Errorf("OaBank.properties.name missing")
	}
	if _, ok := bankProps["account"]; !ok {
		t.Errorf("OaBank.properties.account missing")
	}
	if bankProps["name"].(map[string]any)["description"] != "银行名称" {
		t.Errorf("OaBank.name.description = %v, want 银行名称", bankProps["name"])
	}
	if bankProps["account"].(map[string]any)["description"] != "银行账户" {
		t.Errorf("OaBank.account.description = %v, want 银行账户", bankProps["account"])
	}
	bankRequired := bankObj["required"].([]any)
	if len(bankRequired) != 2 {
		t.Errorf("OaBank.required = %v, want [name, account]", bankRequired)
	}

	// ---- OaHealthInfo ----
	hiSchema, ok := schemas["OaHealthInfo"]
	if !ok {
		t.Fatalf("OaHealthInfo schema missing")
	}
	hiObj := hiSchema.(map[string]any)
	if hiObj["type"] != "object" {
		t.Errorf("OaHealthInfo.type = %v, want object", hiObj["type"])
	}
	hiProps := hiObj["properties"].(map[string]any)
	if _, ok := hiProps["height"]; !ok {
		t.Errorf("OaHealthInfo.properties.height missing")
	}
	if _, ok := hiProps["weight"]; !ok {
		t.Errorf("OaHealthInfo.properties.weight missing")
	}
	if hiProps["height"].(map[string]any)["description"] != "身高" {
		t.Errorf("OaHealthInfo.height.description = %v, want 身高", hiProps["height"])
	}
	if hiProps["weight"].(map[string]any)["description"] != "体重" {
		t.Errorf("OaHealthInfo.weight.description = %v, want 体重", hiProps["weight"])
	}
	hiRequired := hiObj["required"].([]any)
	if len(hiRequired) != 2 {
		t.Errorf("OaHealthInfo.required = %v, want [height, weight]", hiRequired)
	}
}

// TestGenerateOpenAPI_ReqNestedFields 验证请求体 oaCreateUserReq 中各维度的嵌套字段结构：
// []*T → array + items: nullable + allOf[$ref]; *T → nullable + allOf[$ref];
// T → 直接 $ref; []T → array + items: $ref; 父级 required 列表校验
func TestGenerateOpenAPI_ReqNestedFields(t *testing.T) {
	r := NewRouter()
	r.POST("/users", oaCreateUser)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Test API", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	reqSchema := schemas["oaCreateUserReq"].(map[string]any)
	reqProps := reqSchema["properties"].(map[string]any)

	// mailboxList: []*oaMailbox → array, items 为 nullable + allOf [$ref]
	mbField := reqProps["mailboxList"].(map[string]any)
	if mbField["type"] != "array" {
		t.Errorf("mailboxList.type = %v, want array", mbField["type"])
	}
	if mbField["description"] != "可用的邮箱列表" {
		t.Errorf("mailboxList.description = %v, want 可用的邮箱列表", mbField["description"])
	}
	mbItems := mbField["items"].(map[string]any)
	if mbItems["nullable"] != true {
		t.Errorf("mailboxList.items.nullable = %v, want true", mbItems["nullable"])
	}
	mbAllOf, ok := mbItems["allOf"].([]any)
	if !ok || len(mbAllOf) != 1 {
		t.Fatalf("mailboxList.items.allOf = %v, want [{$ref}]", mbItems)
	}
	if ref, ok := mbAllOf[0].(map[string]any)["$ref"]; !ok || ref != "#/components/schemas/oaMailbox" {
		t.Errorf("mailboxList.items.allOf[0].$ref = %v, want #/components/schemas/oaMailbox", mbAllOf[0])
	}

	// company: *OaCompany → nullable + allOf [$ref]
	compField := reqProps["company"].(map[string]any)
	if compField["nullable"] != true {
		t.Errorf("company.nullable = %v, want true", compField["nullable"])
	}
	if compField["description"] != "供职的公司信息" {
		t.Errorf("company.description = %v, want 供职的公司信息", compField["description"])
	}
	compAllOf, ok := compField["allOf"].([]any)
	if !ok || len(compAllOf) != 1 {
		t.Fatalf("company.allOf = %v, want [{$ref}]", compField)
	}
	if ref, ok := compAllOf[0].(map[string]any)["$ref"]; !ok || ref != "#/components/schemas/OaCompany" {
		t.Errorf("company.allOf[0].$ref = %v, want #/components/schemas/OaCompany", compAllOf[0])
	}

	// school: OaSchool 值类型嵌套 → 直接 $ref（非 nullable，非 allOf 包裹）
	schField := reqProps["school"].(map[string]any)
	if schField["description"] != "就读的学校信息" {
		t.Errorf("school.description = %v, want 就读的学校信息", schField["description"])
	}
	if ref, ok := schField["$ref"]; !ok || ref != "#/components/schemas/OaSchool" {
		t.Errorf("school.$ref = %v, want #/components/schemas/OaSchool", schField)
	}

	// family: []OaFamily → array, items 为直接 $ref（值类型切片，非 nullable）
	famField := reqProps["family"].(map[string]any)
	if famField["type"] != "array" {
		t.Errorf("family.type = %v, want array", famField["type"])
	}
	if famField["description"] != "家庭成员信息" {
		t.Errorf("family.description = %v, want 家庭成员信息", famField["description"])
	}
	famItems := famField["items"].(map[string]any)
	if ref, ok := famItems["$ref"]; !ok || ref != "#/components/schemas/OaFamily" {
		t.Errorf("family.items.$ref = %v, want #/components/schemas/OaFamily", famItems)
	}

	// required: name, mailboxList, school（均标记 required:"true"），不含 company、family（未标记 required）
	reqRequired := reqSchema["required"].([]any)
	hasName, hasMB, hasComp, hasSchool := false, false, false, false
	for _, r := range reqRequired {
		switch r {
		case "name":
			hasName = true
		case "mailboxList":
			hasMB = true
		case "company":
			hasComp = true
		case "school":
			hasSchool = true
		}
	}
	if !hasName {
		t.Errorf("oaCreateUserReq.required = %v, should contain 'name'", reqRequired)
	}
	if !hasMB {
		t.Errorf("oaCreateUserReq.required = %v, should contain 'mailboxList'", reqRequired)
	}
	if hasComp {
		t.Errorf("oaCreateUserReq.required = %v, should NOT contain 'company'", reqRequired)
	}
	if !hasSchool {
		t.Errorf("oaCreateUserReq.required = %v, should contain 'school'", reqRequired)
	}
	// family 未标记 required，不应出现在 required 列表中
	for _, r := range reqRequired {
		if r == "family" {
			t.Errorf("oaCreateUserReq.required = %v, should NOT contain 'family'", reqRequired)
		}
	}
}

// ---- 响应嵌套结构体 OpenAPI 文档测试 ----

// resNestedInfo 响应值类型嵌套
type resNestedInfo struct {
	Code    string `json:"code" required:"true" description:"信息编码"`
	Message string `json:"message" description:"信息内容"`
}

// resNestedTag 响应值类型切片元素
type resNestedTag struct {
	ID   int    `json:"id" required:"true" description:"标签ID"`
	Name string `json:"name" description:"标签名称"`
}

// resNestedDetail 响应指针类型切片元素
type resNestedDetail struct {
	Address string  `json:"address" required:"true" description:"详细地址"`
	ZipCode *string `json:"zipCode" default:"000000" description:"邮编"`
}

// resNestedUser 覆盖四维响应嵌套：值类型 Info、指针类型 Manager（自引用）、值切片 Tags、指针切片 Details
type resNestedUser struct {
	ID      int                `json:"id" required:"true" description:"用户ID"`
	Name    string             `json:"name" required:"true" description:"用户名"`
	Info    resNestedInfo      `json:"info" description:"用户扩展信息"`
	Manager *resNestedUser     `json:"manager" description:"直属上级"`
	Tags    []resNestedTag     `json:"tags" description:"用户标签"`
	Details []*resNestedDetail `json:"details" description:"地址明细"`
}

type resNestedReq struct {
	OpenAPIMeta `tags:"ResNested" summary:"获取嵌套响应"`
	ID          int `json:"id" required:"true"`
}

func resNestedHandler(_ context.Context, _ resNestedReq) (resNestedUser, error) {
	return resNestedUser{}, nil
}

// TestGenerateOpenAPI_ResNested 验证响应体嵌套结构体的 OpenAPI 文档生成：
// Response_ 包装层、值类型/指针/值切片/指针切片嵌套、自引用、description/required 传播
func TestGenerateOpenAPI_ResNested(t *testing.T) {
	r := NewRouter()
	r.GET("/res-nested", resNestedHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Res Nested Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// ---- Response_resNestedUser 包装层 ----
	wrapSchema, ok := schemas["Response_resNestedUser"]
	if !ok {
		t.Fatalf("Response_resNestedUser schema missing")
	}
	wrapObj := wrapSchema.(map[string]any)
	wrapProps := wrapObj["properties"].(map[string]any)
	// data 字段应引用 resNestedUser
	dataField := wrapProps["data"].(map[string]any)
	if ref, ok := dataField["$ref"]; !ok || ref != "#/components/schemas/resNestedUser" {
		t.Errorf("Response_resNestedUser.data.$ref = %v, want #/components/schemas/resNestedUser", dataField)
	}
	// code/message 基本字段存在
	if codeField := wrapProps["code"].(map[string]any); codeField["type"] != "integer" {
		t.Errorf("Response_resNestedUser.code.type = %v, want integer", codeField["type"])
	}
	if msgField := wrapProps["message"].(map[string]any); msgField["type"] != "string" {
		t.Errorf("Response_resNestedUser.message.type = %v, want string", msgField["type"])
	}

	// ---- resNestedUser schema（嵌套字段结构）----
	userSchema, ok := schemas["resNestedUser"]
	if !ok {
		t.Fatalf("resNestedUser schema missing")
	}
	userObj := userSchema.(map[string]any)
	userProps := userObj["properties"].(map[string]any)

	// info: resNestedInfo 值类型 → 直接 $ref
	infoField := userProps["info"].(map[string]any)
	if infoField["description"] != "用户扩展信息" {
		t.Errorf("resNestedUser.info.description = %v, want 用户扩展信息", infoField["description"])
	}
	if ref, ok := infoField["$ref"]; !ok || ref != "#/components/schemas/resNestedInfo" {
		t.Errorf("resNestedUser.info.$ref = %v, want #/components/schemas/resNestedInfo", infoField)
	}

	// manager: *resNestedUser 指针（自引用）→ nullable + allOf[$ref]
	mgrField := userProps["manager"].(map[string]any)
	if mgrField["nullable"] != true {
		t.Errorf("resNestedUser.manager.nullable = %v, want true", mgrField["nullable"])
	}
	if mgrField["description"] != "直属上级" {
		t.Errorf("resNestedUser.manager.description = %v, want 直属上级", mgrField["description"])
	}
	mgrAllOf, ok := mgrField["allOf"].([]any)
	if !ok || len(mgrAllOf) != 1 {
		t.Fatalf("resNestedUser.manager.allOf = %v, want [{$ref}]", mgrField)
	}
	if ref, ok := mgrAllOf[0].(map[string]any)["$ref"]; !ok || ref != "#/components/schemas/resNestedUser" {
		t.Errorf("resNestedUser.manager.allOf[0].$ref = %v, want #/components/schemas/resNestedUser (self-ref)", mgrAllOf[0])
	}

	// tags: []resNestedTag 值类型切片 → array, items 为 $ref
	tagsField := userProps["tags"].(map[string]any)
	if tagsField["type"] != "array" {
		t.Errorf("resNestedUser.tags.type = %v, want array", tagsField["type"])
	}
	if tagsField["description"] != "用户标签" {
		t.Errorf("resNestedUser.tags.description = %v, want 用户标签", tagsField["description"])
	}
	tagsItems := tagsField["items"].(map[string]any)
	if ref, ok := tagsItems["$ref"]; !ok || ref != "#/components/schemas/resNestedTag" {
		t.Errorf("resNestedUser.tags.items.$ref = %v, want #/components/schemas/resNestedTag", tagsItems)
	}

	// details: []*resNestedDetail 指针切片 → array, items 为 nullable + allOf[$ref]
	detField := userProps["details"].(map[string]any)
	if detField["type"] != "array" {
		t.Errorf("resNestedUser.details.type = %v, want array", detField["type"])
	}
	if detField["description"] != "地址明细" {
		t.Errorf("resNestedUser.details.description = %v, want 地址明细", detField["description"])
	}
	detItems := detField["items"].(map[string]any)
	if detItems["nullable"] != true {
		t.Errorf("resNestedUser.details.items.nullable = %v, want true", detItems["nullable"])
	}
	detAllOf, ok := detItems["allOf"].([]any)
	if !ok || len(detAllOf) != 1 {
		t.Fatalf("resNestedUser.details.items.allOf = %v, want [{$ref}]", detItems)
	}
	if ref, ok := detAllOf[0].(map[string]any)["$ref"]; !ok || ref != "#/components/schemas/resNestedDetail" {
		t.Errorf("resNestedUser.details.items.allOf[0].$ref = %v, want #/components/schemas/resNestedDetail", detAllOf[0])
	}

	// required: id, name
	userRequired := userObj["required"].([]any)
	if len(userRequired) != 2 {
		t.Errorf("resNestedUser.required = %v, want [id, name]", userRequired)
	}

	// ---- 子 schema：resNestedInfo（值类型嵌套）----
	infoSchema, ok := schemas["resNestedInfo"]
	if !ok {
		t.Fatalf("resNestedInfo schema missing")
	}
	infoObj := infoSchema.(map[string]any)
	infoProps := infoObj["properties"].(map[string]any)
	if infoProps["code"].(map[string]any)["description"] != "信息编码" {
		t.Errorf("resNestedInfo.code.description = %v, want 信息编码", infoProps["code"])
	}
	if infoProps["message"].(map[string]any)["description"] != "信息内容" {
		t.Errorf("resNestedInfo.message.description = %v, want 信息内容", infoProps["message"])
	}
	infoRequired := infoObj["required"].([]any)
	if len(infoRequired) != 1 || infoRequired[0] != "code" {
		t.Errorf("resNestedInfo.required = %v, want [code]", infoRequired)
	}

	// ---- 子 schema：resNestedTag（值切片元素）----
	tagSchema, ok := schemas["resNestedTag"]
	if !ok {
		t.Fatalf("resNestedTag schema missing")
	}
	tagObj := tagSchema.(map[string]any)
	tagProps := tagObj["properties"].(map[string]any)
	if tagProps["id"].(map[string]any)["description"] != "标签ID" {
		t.Errorf("resNestedTag.id.description = %v, want 标签ID", tagProps["id"])
	}
	if tagProps["name"].(map[string]any)["description"] != "标签名称" {
		t.Errorf("resNestedTag.name.description = %v, want 标签名称", tagProps["name"])
	}
	tagRequired := tagObj["required"].([]any)
	if len(tagRequired) != 1 || tagRequired[0] != "id" {
		t.Errorf("resNestedTag.required = %v, want [id]", tagRequired)
	}

	// ---- 子 schema：resNestedDetail（指针切片元素，有 default）----
	detailSchema, ok := schemas["resNestedDetail"]
	if !ok {
		t.Fatalf("resNestedDetail schema missing")
	}
	detailObj := detailSchema.(map[string]any)
	detailProps := detailObj["properties"].(map[string]any)
	if detailProps["address"].(map[string]any)["description"] != "详细地址" {
		t.Errorf("resNestedDetail.address.description = %v, want 详细地址", detailProps["address"])
	}
	// zipCode 有 default → 不进 required
	zipField := detailProps["zipCode"].(map[string]any)
	if zipField["default"] != "000000" {
		t.Errorf("resNestedDetail.zipCode.default = %v, want 000000", zipField["default"])
	}
	if zipField["description"] != "邮编" {
		t.Errorf("resNestedDetail.zipCode.description = %v, want 邮编", zipField["description"])
	}
	detailRequired := detailObj["required"].([]any)
	if len(detailRequired) != 1 || detailRequired[0] != "address" {
		t.Errorf("resNestedDetail.required = %v, want [address]", detailRequired)
	}

	// ---- 可序列化（确保自引用不产生循环）----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// ---- 嵌套值类型结构体中 default 的文档展示 ----

type oaSchoolWithDefault struct {
	Name    string  `json:"name" default:"巡天大学" required:"true" description:"学校名称"`
	Address *string `json:"address" default:"银河系" description:"学校地址"`
}

type oaSchoolDefaultReq struct {
	OpenAPIMeta `tags:"SchoolDefault" summary:"测试值类型default展示"`
	Nickname    string              `json:"nickname" default:"无情剑客" description:"昵称"`
	School      oaSchoolWithDefault `json:"school" required:"true" description:"就读的学校信息"`
}

type oaSchoolDefaultRes struct {
	OK bool `json:"ok"`
}

func oaSchoolDefaultHandler(_ context.Context, _ oaSchoolDefaultReq) (oaSchoolDefaultRes, error) {
	return oaSchoolDefaultRes{}, nil
}

// TestGenerateOpenAPI_ValueTypeDefaultInNestedStruct 验证：
// 值嵌套 struct（如 School 值类型嵌入 Req）中，值类型字段的 default 在注册阶段被填充 → 文档必须展示。
// 指针类型字段的 default 始终展示（请求阶段 fill nil 指针）。
func TestGenerateOpenAPI_ValueTypeDefaultInNestedStruct(t *testing.T) {
	r := NewRouter()
	r.POST("/school-default", oaSchoolDefaultHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "School Default Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// ---- 顶层 Req 中 Nickname（string 值类型）应有 default ----
	reqSchema, ok := schemas["oaSchoolDefaultReq"]
	if !ok {
		t.Fatalf("oaSchoolDefaultReq schema missing")
	}
	reqObj := reqSchema.(map[string]any)
	reqProps := reqObj["properties"].(map[string]any)

	nicknameField := reqProps["nickname"].(map[string]any)
	if nicknameField["default"] != "无情剑客" {
		t.Errorf("oaSchoolDefaultReq.nickname.default = %v, want 无情剑客", nicknameField["default"])
	}

	// ---- 嵌套值类型 oaSchoolWithDefault ----
	schoolSchema, ok := schemas["oaSchoolWithDefault"]
	if !ok {
		t.Fatalf("oaSchoolWithDefault schema missing")
	}
	schoolObj := schoolSchema.(map[string]any)
	schoolProps := schoolObj["properties"].(map[string]any)

	// Name: string 值类型，注册阶段填充，文档必须展示 default
	nameField := schoolProps["name"].(map[string]any)
	if nameField["type"] != "string" {
		t.Errorf("oaSchoolWithDefault.name.type = %v, want string", nameField["type"])
	}
	if nameField["default"] != "巡天大学" {
		t.Errorf("oaSchoolWithDefault.name.default = %v, want 巡天大学", nameField["default"])
	}
	if nameField["description"] != "学校名称" {
		t.Errorf("oaSchoolWithDefault.name.description = %v, want 学校名称", nameField["description"])
	}

	// Address: *string 指针类型，两个阶段都可能填充，文档必须展示 default
	addrField := schoolProps["address"].(map[string]any)
	if addrField["type"] != "string" {
		t.Errorf("oaSchoolWithDefault.address.type = %v, want string", addrField["type"])
	}
	if addrField["default"] != "银河系" {
		t.Errorf("oaSchoolWithDefault.address.default = %v, want 银河系", addrField["default"])
	}
	if addrField["description"] != "学校地址" {
		t.Errorf("oaSchoolWithDefault.address.description = %v, want 学校地址", addrField["description"])
	}

	// required: 两个字段都有 default → 都不进入 required
	schoolRequired := schoolObj["required"]
	if schoolRequired != nil {
		t.Errorf("oaSchoolWithDefault.required = %v, want nil (both fields have default)", schoolRequired)
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}

// ---- 指针嵌套结构体中值类型 default 不应展示 ----

type oaCompanyWithDefault struct {
	Phone string  `json:"phone" default:"135xxxx1234" description:"公司电话"`
	Email *string `json:"email" default:"buexplain@qq.com" description:"公司邮箱"`
}

type oaPtrNestedDefaultReq struct {
	OpenAPIMeta `tags:"PtrNestedDefault" summary:"测试指针嵌套default展示"`
	Nickname    string                `json:"nickname" default:"无情剑客" description:"昵称"`
	School      oaSchoolWithDefault   `json:"school" required:"true" description:"就读的学校信息"`
	Company     *oaCompanyWithDefault `json:"company" description:"供职的公司信息"`
}

type oaPtrNestedDefaultRes struct {
	OK bool `json:"ok"`
}

func oaPtrNestedDefaultHandler(_ context.Context, _ oaPtrNestedDefaultReq) (oaPtrNestedDefaultRes, error) {
	return oaPtrNestedDefaultRes{}, nil
}

// TestGenerateOpenAPI_PtrNestedValueTypeDefault 验证：
// 值类型字段（如 Phone string）嵌套在指针结构体（*Company）中时，
// 注册阶段 Company 为 nil 无法到达，请求阶段值类型被跳过 → default 实际不生效 → 文档不应展示。
// 指针类型字段（如 Email *string）请求阶段可 fill nil → 文档应展示。
func TestGenerateOpenAPI_PtrNestedValueTypeDefault(t *testing.T) {
	r := NewRouter()
	r.POST("/ptr-nested-default", oaPtrNestedDefaultHandler)

	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "Ptr Nested Default Test", Version: "1.0.0"})

	comps := doc["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)

	// ---- 值嵌套 struct（School）→ 值类型 default 应展示 ----
	schoolSchema, ok := schemas["oaSchoolWithDefault"]
	if !ok {
		t.Fatalf("oaSchoolWithDefault schema missing")
	}
	schoolObj := schoolSchema.(map[string]any)
	schoolProps := schoolObj["properties"].(map[string]any)

	// Name: string 值类型，值嵌套到达 → 应展示 default
	if _, ok := schoolProps["name"].(map[string]any)["default"]; !ok {
		t.Errorf("oaSchoolWithDefault.name.default missing (值嵌套，注册阶段已填充)")
	}

	// ---- 指针嵌套 struct（Company）→ 值类型 default 不应展示 ----
	companySchema, ok := schemas["oaCompanyWithDefault"]
	if !ok {
		t.Fatalf("oaCompanyWithDefault schema missing")
	}
	companyObj := companySchema.(map[string]any)
	companyProps := companyObj["properties"].(map[string]any)

	// Phone: string 值类型，仅指针嵌套到达 → 不应展示 default（两阶段都不会填）
	phoneField := companyProps["phone"].(map[string]any)
	if phoneField["type"] != "string" {
		t.Errorf("oaCompanyWithDefault.phone.type = %v, want string", phoneField["type"])
	}
	if phoneField["description"] != "公司电话" {
		t.Errorf("oaCompanyWithDefault.phone.description = %v, want 公司电话", phoneField["description"])
	}
	if _, hasDefault := phoneField["default"]; hasDefault {
		t.Errorf("oaCompanyWithDefault.phone.default = %v, want no default (值类型在指针嵌套下不可靠)", phoneField["default"])
	}

	// Email: *string 指针类型 → 请求阶段 fill nil → 应展示 default
	emailField := companyProps["email"].(map[string]any)
	if emailField["type"] != "string" {
		t.Errorf("oaCompanyWithDefault.email.type = %v, want string", emailField["type"])
	}
	if emailField["default"] != "buexplain@qq.com" {
		t.Errorf("oaCompanyWithDefault.email.default = %v, want buexplain@qq.com", emailField["default"])
	}
	if emailField["description"] != "公司邮箱" {
		t.Errorf("oaCompanyWithDefault.email.description = %v, want 公司邮箱", emailField["description"])
	}

	// ---- 可序列化 ----
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("doc not JSON-serializable: %v", err)
	}
}
