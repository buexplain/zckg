package zchttp

import (
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件针对覆盖率报告中的残余未覆盖分支做定向补强：
// 能通过注册/请求/生成等公共路径触达的一律走公共路径，
// 仅防御性分支（公共路径构造不出的病态输入）才以包内直调方式单测。

// cov2Serve 以指定 Router 构造引擎并执行一次请求，返回响应记录器。
func cov2Serve(t *testing.T, router *Router, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := NewEngine()
	engine.Router = router
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// ---------- binding.go ----------

// cov2TimeReq 用于覆盖 setTime 的空值短路分支。
type cov2TimeReq struct {
	T time.Time `form:"t" time_format:"2006-01-02"`
}

// TestBindValues_EmptyTimeValue 覆盖 setTime 对空字符串直接返回的分支：
// query 参数存在但值为空时不解析、字段保持零值。
func TestBindValues_EmptyTimeValue(t *testing.T) {
	router := NewRouter()
	router.GET("/t", func(_ context.Context, req cov2TimeReq) (cov2TimeReq, error) { return req, nil })

	rec := cov2Serve(t, router, http.MethodGet, "/t?t=", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var got cov2TimeReq
	decodeData(t, rec, &got)
	if !got.T.IsZero() {
		t.Fatalf("空 query 值不应解析出时间，实际: %v", got.T)
	}
}

// cov2HiddenFieldReq 含一个未导出字段（仅用于包内直调构造病态 meta）。
type cov2HiddenFieldReq struct {
	Public  string
	private string //nolint:structcheck // 仅用于构造不可设置字段
}

// TestBindValues_UnsettableFieldSkipped 覆盖 bindValues 中字段不可设置（CanSet=false）
// 时跳过的防御分支：正常注册路径无法构造出该状态，包内直调以病态 meta 触发。
func TestBindValues_UnsettableFieldSkipped(t *testing.T) {
	meta := structMeta{fields: []fieldMeta{
		{name: "public", indices: []int{0}},
		{name: "private", indices: []int{1}},
	}}
	reqPtr := reflect.New(reflect.TypeOf(cov2HiddenFieldReq{}))
	if err := bindValues(reqPtr, map[string][]string{
		"public":  {"ok"},
		"private": {"x"},
	}, nil, meta); err != nil {
		t.Fatalf("bindValues 尽力绑定应恒返回 nil: %v", err)
	}
	got := reqPtr.Interface().(*cov2HiddenFieldReq)
	if got.Public != "ok" {
		t.Fatalf("可设置字段应正常绑定，实际: %q", got.Public)
	}
	if got.private != "" {
		t.Fatal("不可设置字段不应被绑定")
	}
}

// TestBindPathParams_UnsettableFieldSkipped 覆盖 bindPathParams 中
// 字段不可设置时跳过的防御分支（病态 meta 直调触发）。
func TestBindPathParams_UnsettableFieldSkipped(t *testing.T) {
	params := []pathParamBinding{
		{indices: []int{0}},
		{indices: []int{1}},
	}
	reqPtr := reflect.New(reflect.TypeOf(cov2HiddenFieldReq{}))
	if err := bindPathParams(reqPtr, params, []string{"v0", "v1"}); err != nil {
		t.Fatalf("不可设置字段应被跳过而非报错: %v", err)
	}
	got := reqPtr.Interface().(*cov2HiddenFieldReq)
	if got.Public != "v0" {
		t.Fatalf("可设置路径参数应正常绑定，实际: %q", got.Public)
	}
}

// ---------- defaults.go ----------

// cov2Sub 带指针默认值字段，用于运行时嵌套容器默认值填充。
type cov2Sub struct {
	Tag *string `json:"tag" default:"filled"`
}

type cov2SliceNilReq struct {
	Items []*cov2Sub `json:"items"`
}

// TestApplyDefaults_SliceNilElem 覆盖运行时 applyDefaults 遍历结构体指针切片时
// 跳过 nil 元素的分支：null 元素跳过，非空元素的指针默认值正常填充。
func TestApplyDefaults_SliceNilElem(t *testing.T) {
	router := NewRouter()
	router.POST("/s", func(_ context.Context, req cov2SliceNilReq) (cov2SliceNilReq, error) { return req, nil })

	rec := cov2Serve(t, router, http.MethodPost, "/s", `{"items":[null,{"tag":null}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var got cov2SliceNilReq
	decodeData(t, rec, &got)
	if len(got.Items) != 2 || got.Items[0] != nil {
		t.Fatalf("第一个元素应保持 nil，实际: %+v", got.Items)
	}
	if got.Items[1].Tag == nil || *got.Items[1].Tag != "filled" {
		t.Fatalf("第二个元素的指针默认值应被填充，实际: %+v", got.Items[1])
	}
}

type cov2MapNilReq struct {
	M map[string]*cov2Sub `json:"m"`
}

// TestApplyDefaults_MapNilValue 覆盖运行时 applyDefaults 遍历指针值 map 时
// 跳过 nil 值的分支。
func TestApplyDefaults_MapNilValue(t *testing.T) {
	router := NewRouter()
	router.POST("/m", func(_ context.Context, req cov2MapNilReq) (cov2MapNilReq, error) { return req, nil })

	rec := cov2Serve(t, router, http.MethodPost, "/m", `{"m":{"a":null,"b":{"tag":null}}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var got cov2MapNilReq
	decodeData(t, rec, &got)
	if got.M["a"] != nil {
		t.Fatalf("nil 值应保持 nil，实际: %+v", got.M["a"])
	}
	if got.M["b"].Tag == nil || *got.M["b"].Tag != "filled" {
		t.Fatalf("非 nil 值的指针默认值应被填充，实际: %+v", got.M["b"])
	}
}

// cov2DeepContainerReq 覆盖注册期容器展开循环中的指针中间层分支。
type cov2DeepContainerReq struct {
	A [][]*cov2Sub          `json:"a"`
	B map[string][]*cov2Sub `json:"b"`
}

// TestCheckUnsupportedDefaults_NestedContainerPtrLayers 覆盖 checkUnsupportedDefaults
// 展开多层容器时的指针中间层分支（切片元素仍为指针）与
// map 值类型为指针元素切片的分支：注册不应 panic。
func TestCheckUnsupportedDefaults_NestedContainerPtrLayers(t *testing.T) {
	router := NewRouter()
	router.POST("/deep", func(_ context.Context, req cov2DeepContainerReq) (cov2DeepContainerReq, error) { return req, nil })

	// 注册成功即通过：类型树扫描完整走完（分支覆盖由覆盖率报告验证）
	rec := cov2Serve(t, router, http.MethodPost, "/deep", `{"a":[[{"tag":"x"}]],"b":{"k":[{"tag":"y"}]}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestApplyDefaultsWithVisiting_NonStructAndNilVisiting 覆盖
// reqPtr 指向非结构体时直接返回、以及 visiting 为 nil 时惰性创建的分支（包内直调）。
func TestApplyDefaultsWithVisiting_NonStructAndNilVisiting(t *testing.T) {
	n := 42
	applyDefaultsWithVisiting(reflect.ValueOf(&n), structMeta{}, false, nil) // 非 struct 直接返回

	type cov2Plain struct{ A string }
	applyDefaultsWithVisiting(reflect.ValueOf(&cov2Plain{}), cachedStructMeta(reflect.TypeOf(cov2Plain{})), false, nil)
	// 不 panic 即通过：visiting 惰性创建后正常遍历
}

// ---------- meta.go ----------

type cov2RaceMeta struct{ A string }

// TestCachedStructMeta_ConcurrentLoadOrStore 以并发竞争覆盖
// LoadOrStore 发现其他协程已抢先写入的分支（尽力触发：多轮并发重建）。
func TestCachedStructMeta_ConcurrentLoadOrStore(t *testing.T) {
	typ := reflect.TypeOf(cov2RaceMeta{})
	for round := 0; round < 30; round++ {
		structMetaCache.Delete(typ)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 64; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				cachedStructMeta(typ)
			}()
		}
		close(start)
		wg.Wait()
	}
	if _, ok := structMetaCache.Load(typ); !ok {
		t.Fatal("并发构建后缓存应存在")
	}
}

// TestBuildStructMetaWithVisiting_AlreadyVisiting 覆盖入口类型已在
// visiting 集合中时返回空 meta 断开环的分支（包内直调）。
func TestBuildStructMetaWithVisiting_AlreadyVisiting(t *testing.T) {
	typ := reflect.TypeOf(cov2RaceMeta{})
	m := buildStructMetaWithVisiting(typ, map[reflect.Type]bool{typ: true})
	if len(m.fields) != 0 {
		t.Fatalf("已在访问路径上的类型应返回空 meta，实际: %+v", m)
	}
}

// Cov2SelfEmbed 自引用嵌入类型：注册期嵌入展开必须断开环并告警，不得栈溢出。
// 类型名必须导出，否则会先被“未导出嵌入指针”分支拦截，触达不到递归环检测。
type Cov2SelfEmbed struct {
	*Cov2SelfEmbed
	Name string `json:"name"`
}

type cov2SelfEmbedRes struct {
	Name string `json:"name"`
}

// TestRegister_SelfRecursiveEmbedding 覆盖嵌入展开检测到自引用环时
// 跳过嵌入并告警的分支：注册与请求均应正常完成。
func TestRegister_SelfRecursiveEmbedding(t *testing.T) {
	router := NewRouter()
	router.POST("/self", func(_ context.Context, req Cov2SelfEmbed) (cov2SelfEmbedRes, error) {
		return cov2SelfEmbedRes{Name: req.Name}, nil
	})
	rec := cov2Serve(t, router, http.MethodPost, "/self", `{"name":"n1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "n1") {
		t.Fatalf("自引用嵌入类型应正常绑定，实际: %s", rec.Body.String())
	}
}

// cov2EmbedIgnored 嵌入结构体含 json:"-" 字段，展开时应被跳过。
type cov2EmbedIgnored struct {
	Secret string `json:"-"`
	Shown  string `json:"shown"`
}

type cov2EmbedIgnoredReq struct {
	cov2EmbedIgnored
}

// TestBuildStructMeta_EmbeddedFieldSkipped 覆盖嵌入展开循环中
// name 为空或 "-" 的子字段被跳过的分支。
func TestBuildStructMeta_EmbeddedFieldSkipped(t *testing.T) {
	m := buildStructMeta(reflect.TypeOf(cov2EmbedIgnoredReq{}))
	for _, fm := range m.fields {
		if fm.name == "-" || fm.name == "" {
			t.Fatalf("meta 不应包含需跳过的字段: %+v", fm)
		}
		if fm.name == "Secret" {
			t.Fatal("json:\"-\" 的嵌入字段不应进入可绑定集合")
		}
	}
	found := false
	for _, fm := range m.fields {
		if fm.name == "shown" {
			found = true
		}
	}
	if !found {
		t.Fatal("正常嵌入字段 shown 应进入可绑定集合")
	}
}

// ---------- validate.go ----------

type cov2PtrValidator struct{}

func (c *cov2PtrValidator) Validate() error { return errors.New("boom") }

// TestValidateCustom_AssertionFails 覆盖 implementsValidator 为 true 但
// 值类型接口断言失败的防御分支（方法定义在指针接收者上，传入的却是值）。
func TestValidateCustom_AssertionFails(t *testing.T) {
	typ := reflect.TypeOf(cov2PtrValidator{})
	meta := cachedStructMeta(typ)
	if !meta.implementsValidator {
		t.Fatal("*cov2PtrValidator 应被判定为实现 Validator")
	}
	if err := validateCustom(reflect.ValueOf(cov2PtrValidator{}), meta); err != nil {
		t.Fatalf("断言失败应返回 nil（防御分支），实际: %v", err)
	}
}

// TestValidateNonzeroWalk_NonStructAndNilVisited 覆盖入口值非结构体直接返回、
// 以及 visited 为 nil 时惰性创建的分支（包内直调）。
func TestValidateNonzeroWalk_NonStructAndNilVisited(t *testing.T) {
	if err := validateNonzeroWalk(reflect.ValueOf(42), structMeta{}, nil, "", false); err != nil {
		t.Fatalf("非结构体应直接返回 nil: %v", err)
	}
	type cov2NoNonzero struct{ A string }
	x := cov2NoNonzero{}
	if err := validateNonzeroWalk(reflect.ValueOf(&x).Elem(), cachedStructMeta(reflect.TypeOf(x)), nil, "", false); err != nil {
		t.Fatalf("无 nonzero 字段应返回 nil: %v", err)
	}
}

// ---------- router_trie.go ----------

// TestRouteConflictPanic_NilExisting 覆盖 existing 为 nil 时的冲突提示格式
// （中间节点冲突且该节点尚无终点 entry 的防御分支，正常注册路径不可达）。
func TestRouteConflictPanic_NilExisting(t *testing.T) {
	incoming := &routeEntry{handlerName: "pkg.H", handlerFile: "h.go", handlerLine: 9}
	defer func() {
		msg, ok := recover().(string)
		if !ok {
			t.Fatal("应 panic")
		}
		if !strings.Contains(msg, "route conflict") || !strings.Contains(msg, "pkg.H") || strings.Contains(msg, "already registered") {
			t.Fatalf("existing=nil 的冲突消息格式不符: %s", msg)
		}
	}()
	routeConflictPanic("GET", "/x", nil, incoming, "")
}

// ---------- openapi.go ----------

// TestGenerateOpenAPI_NilReqResEntry 覆盖 buildOperation 对 reqType/resType
// 为 nil 的 entry 返回空并被跳过的防御分支（注册路径不可达，直接构造路由记录）。
func TestGenerateOpenAPI_NilReqResEntry(t *testing.T) {
	r := NewRouter()
	r.routes = append(r.routes, routeRecord{
		method: http.MethodGet,
		path:   "/ghost",
		entry:  &routeEntry{handlerName: "ghost"},
	})
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	paths, _ := doc["paths"].(map[string]any)
	if _, ok := paths["/ghost"]; ok {
		t.Fatalf("reqType/resType 为 nil 的路由不应出现在文档中: %v", paths)
	}
}

type cov2WalkSub struct {
	Name string `json:"name" default:"n"`
}

type cov2MapWalkReq struct {
	A map[string][]*cov2WalkSub `json:"a"`
	B map[string][]cov2WalkSub  `json:"b"`
	C map[string]*cov2WalkSub   `json:"c"`
}

type cov2MapWalkRes struct{ Ok bool }

// TestGenerateOpenAPI_MapContainerWalks 覆盖 walkTypeUsage / walkDefaultsReachability
// 对 map 值类型为切片（指针/值元素）与指针的穿透分支。
func TestGenerateOpenAPI_MapContainerWalks(t *testing.T) {
	r := NewRouter()
	r.POST("/walk", func(_ context.Context, req cov2MapWalkReq) (cov2MapWalkRes, error) {
		return cov2MapWalkRes{Ok: true}, nil
	})
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	if doc["paths"] == nil {
		t.Fatal("文档应包含 paths")
	}
	if _, ok := doc["components"].(map[string]any)["schemas"].(map[string]any)["cov2WalkSub"]; !ok {
		t.Fatalf("map 容器穿透后应注册元素结构体 schema: %v", doc["components"])
	}
}

type cov2QuerySkipReq struct {
	Hidden string                `json:"hidden" ignore:"true"`
	File   *multipart.FileHeader `json:"file"`
	Plain  string                `json:"plain"`
	M      map[string]string     `json:"m"`
	Nested cov2WalkSub           `json:"nested"`
}

// TestGenerateOpenAPI_QueryParamSkips 覆盖 buildQueryParams 跳过
// ignore 字段与文件字段的分支。
func TestGenerateOpenAPI_QueryParamSkips(t *testing.T) {
	r := NewRouter()
	r.GET("/skip", func(_ context.Context, req cov2QuerySkipReq) (cov2QuerySkipReq, error) { return req, nil })
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	paths := doc["paths"].(map[string]any)
	op := paths["/skip"].(map[string]any)["get"].(map[string]any)
	params, _ := op["parameters"].([]any)
	names := map[string]bool{}
	for _, p := range params {
		pm := p.(map[string]any)
		if pm["in"] == "query" {
			names[pm["name"].(string)] = true
		}
	}
	if names["hidden"] || names["file"] || names["m"] || names["nested"] {
		t.Fatalf("ignore/文件/map/结构体字段不应作为 query 参数: %v", names)
	}
	if !names["plain"] {
		t.Fatalf("普通字段应作为 query 参数: %v", names)
	}
}

// TestBuildPathParams_NonStruct 覆盖 reqType 非结构体时返回 nil 的防御分支（包内直调）。
func TestBuildPathParams_NonStruct(t *testing.T) {
	g := &openAPIGenerator{
		schemas:           map[string]any{},
		typeNames:         map[reflect.Type]string{},
		nameToType:        map[string]reflect.Type{},
		reachedViaValue:   map[reflect.Type]bool{},
		reachedByDefaults: map[reflect.Type]bool{},
	}
	if got := g.buildPathParams(reflect.TypeOf(42), structMeta{}, map[string]bool{"a": true}, nil); got != nil {
		t.Fatalf("非结构体应返回 nil，实际: %v", got)
	}
}

type cov2PathExtraReq struct {
	ID    int    `json:"id"`
	Extra string `json:"extra"`
}

// TestGenerateOpenAPI_PathParamExtraFields 覆盖 buildPathParams 遍历字段时
// 跳过非路径参数字段的分支。
func TestGenerateOpenAPI_PathParamExtraFields(t *testing.T) {
	r := NewRouter()
	r.GET("/u/{id}", func(_ context.Context, req cov2PathExtraReq) (cov2PathExtraReq, error) { return req, nil })
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	paths := doc["paths"].(map[string]any)
	op := paths["/u/{id}"].(map[string]any)["get"].(map[string]any)
	params, _ := op["parameters"].([]any)
	pathParams := map[string]bool{}
	for _, p := range params {
		pm := p.(map[string]any)
		if pm["in"] == "path" {
			pathParams[pm["name"].(string)] = true
		}
	}
	if !pathParams["id"] || pathParams["extra"] {
		t.Fatalf("path 参数应仅含 id: %v", pathParams)
	}
}

type cov2SchemaSkipReq struct {
	Secret string `json:"-"`
	Ign    string `json:"ign" ignore:"true"`
	Kept   string `json:"kept"`
}

// TestGenerateOpenAPI_SchemaFieldSkips 覆盖 registerStructSchema 跳过
// name 为 "-" 与 ignore 字段的分支。
func TestGenerateOpenAPI_SchemaFieldSkips(t *testing.T) {
	r := NewRouter()
	r.POST("/schema", func(_ context.Context, req cov2SchemaSkipReq) (cov2SchemaSkipReq, error) { return req, nil })
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	schema := schemas["cov2SchemaSkipReq"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if _, ok := props["Secret"]; ok {
		t.Fatal("json:\"-\" 字段不应出现在 schema")
	}
	if _, ok := props["ign"]; ok {
		t.Fatal("ignore 字段不应出现在 schema")
	}
	if _, ok := props["kept"]; !ok {
		t.Fatal("正常字段应出现在 schema")
	}
}

type cov2MapKeyReq struct {
	M1 map[int]string `json:"m1" description:"int-keyed map"`
	M2 map[int]string `json:"m2"`
}

// TestGenerateOpenAPI_MapNonStringKey 覆盖 typeToSchema 对非 string key map
// 追加 key type 说明的两个分支（已有 description 追加 / 无 description 新建）。
func TestGenerateOpenAPI_MapNonStringKey(t *testing.T) {
	r := NewRouter()
	r.POST("/mapkey", func(_ context.Context, req cov2MapKeyReq) (cov2MapKeyReq, error) { return req, nil })
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["cov2MapKeyReq"].(map[string]any)["properties"].(map[string]any)

	m1 := props["m1"].(map[string]any)
	d1, _ := m1["description"].(string)
	if !strings.Contains(d1, "int-keyed map") || !strings.Contains(d1, "key type: int") {
		t.Fatalf("已有 description 应追加 key type 说明: %q", d1)
	}
	m2 := props["m2"].(map[string]any)
	d2, _ := m2["description"].(string)
	if d2 != "key type: int" {
		t.Fatalf("无 description 应新建 key type 说明: %q", d2)
	}
}

type cov2ChanFieldReq struct {
	Ch chan int `json:"ch"`
}

// TestGenerateOpenAPI_UnmappableKindEmptySchema 覆盖 typeToSchema 对
// 无法映射的 Kind（chan）退化为空 schema 的 default 分支。
func TestGenerateOpenAPI_UnmappableKindEmptySchema(t *testing.T) {
	r := NewRouter()
	r.POST("/chan", func(_ context.Context, req cov2ChanFieldReq) (cov2ChanFieldReq, error) { return req, nil })
	doc := GenerateOpenAPI(r, OpenAPIInfo{Title: "t", Version: "1"})
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["cov2ChanFieldReq"].(map[string]any)["properties"].(map[string]any)
	ch, ok := props["ch"].(map[string]any)
	if !ok || len(ch) != 0 {
		t.Fatalf("chan 字段应退化为空 schema，实际: %v", props["ch"])
	}
}
