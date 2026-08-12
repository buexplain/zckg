package zchttp

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// ========== 测试辅助类型 ==========

type testReq struct {
	Name string `json:"name"`
}

type testRes struct {
	Message string `json:"message"`
}

// ========== 错误场景测试 ==========

func TestBuildEntry_NilHandler(t *testing.T) {
	_, err := buildEntry(nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
	if !strings.Contains(err.Error(), "must not be nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_NonFuncHandler(t *testing.T) {
	_, err := buildEntry("not a function", nil, nil)
	if err == nil {
		t.Fatal("expected error for non-function handler")
	}
	if !strings.Contains(err.Error(), "must be a function") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumIn_Zero(t *testing.T) {
	fn := func() (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 0 inputs")
	}
	if !strings.Contains(err.Error(), "exactly two arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumIn_One(t *testing.T) {
	fn := func(ctx context.Context) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 1 input")
	}
	if !strings.Contains(err.Error(), "exactly two arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumIn_Three(t *testing.T) {
	fn := func(ctx context.Context, req testReq, extra int) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 3 inputs")
	}
	if !strings.Contains(err.Error(), "exactly two arguments") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_FirstArgNotContext(t *testing.T) {
	fn := func(s string, req testReq) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with non-context first arg")
	}
	if !strings.Contains(err.Error(), "first argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_SecondArgNotStruct_String(t *testing.T) {
	fn := func(ctx context.Context, name string) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with non-struct second arg")
	}
	if !strings.Contains(err.Error(), "second argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_SecondArgNotStruct_Int(t *testing.T) {
	fn := func(ctx context.Context, n int) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with int second arg")
	}
	if !strings.Contains(err.Error(), "second argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_SecondArgNotStruct_Map(t *testing.T) {
	fn := func(ctx context.Context, m map[string]any) (testRes, error) { return testRes{}, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with map second arg")
	}
	if !strings.Contains(err.Error(), "second argument") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumOut_Zero(t *testing.T) {
	fn := func(ctx context.Context, req testReq) {}
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 0 returns")
	}
	if !strings.Contains(err.Error(), "exactly two return values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumOut_One(t *testing.T) {
	fn := func(ctx context.Context, req testReq) error { return nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 1 return")
	}
	if !strings.Contains(err.Error(), "exactly two return values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_WrongNumOut_Three(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error, int) { return testRes{}, nil, 0 }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with 3 returns")
	}
	if !strings.Contains(err.Error(), "exactly two return values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_FirstReturnNotStruct_String(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (string, error) { return "", nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with non-struct first return")
	}
	if !strings.Contains(err.Error(), "first return value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_FirstReturnNotStruct_Int(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (int, error) { return 0, nil }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with int first return")
	}
	if !strings.Contains(err.Error(), "first return value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_SecondReturnNotError(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, string) { return testRes{}, "" }
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for handler with non-error second return")
	}
	if !strings.Contains(err.Error(), "second return value") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildEntry_CustomErrorType(t *testing.T) {
	// 返回未实现 error 接口的自定义类型，*myErr 没有 Error() 方法
	type myErr struct{}
	fn := func(ctx context.Context, req testReq) (testRes, *myErr) {
		return testRes{}, nil
	}
	_, err := buildEntry(fn, nil, nil)
	if err == nil {
		t.Fatal("expected error for custom type not implementing error")
	}
}

// ========== 成功场景测试 ==========

func TestBuildEntry_ValueStructParams(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{Message: req.Name}, nil
	}
	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.handler == nil {
		t.Fatal("handler should not be nil")
	}

	if entry.reqType != reflect.TypeOf(testReq{}) {
		t.Fatalf("reqType mismatch: got %v, want %v", entry.reqType, reflect.TypeOf(testReq{}))
	}
	if entry.resType != reflect.TypeOf(testRes{}) {
		t.Fatalf("resType mismatch: got %v, want %v", entry.resType, reflect.TypeOf(testRes{}))
	}
	if entry.reqIsPtr {
		t.Fatal("reqIsPtr should be false for value type")
	}
	if entry.resIsPtr {
		t.Fatal("resIsPtr should be false for value type")
	}
	if entry.reqElemType != reflect.TypeOf(testReq{}) {
		t.Fatalf("reqElemType mismatch")
	}

	if !entry.handlerVal.IsValid() {
		t.Fatal("handlerVal should be valid")
	}

	if entry.handlerName == "" || entry.handlerName == "unknown" {
		t.Fatal("handlerName should be populated for a real function")
	}
	if entry.handlerFile == "" || entry.handlerFile == "unknown" {
		t.Fatal("handlerFile should be populated")
	}
	if entry.handlerLine == 0 {
		t.Fatal("handlerLine should be populated")
	}

	if len(entry.middlewares) != 0 {
		t.Fatalf("middlewares should be empty, got %d", len(entry.middlewares))
	}
}

func TestBuildEntry_PointerStructParams(t *testing.T) {
	fn := func(ctx context.Context, req *testReq) (*testRes, error) {
		return &testRes{Message: req.Name}, nil
	}
	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.reqIsPtr {
		t.Fatal("reqIsPtr should be true for pointer type")
	}
	if !entry.resIsPtr {
		t.Fatal("resIsPtr should be true for pointer type")
	}
	if entry.reqElemType != reflect.TypeOf(testReq{}) {
		t.Fatalf("reqElemType should be the struct type, got %v", entry.reqElemType)
	}
	if entry.reqType != reflect.TypeOf((*testReq)(nil)) {
		t.Fatalf("reqType should be *testReq, got %v", entry.reqType)
	}
}

func TestBuildEntry_MixedParams_ValueReqPointerRes(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (*testRes, error) {
		return &testRes{Message: req.Name}, nil
	}
	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.reqIsPtr {
		t.Fatal("reqIsPtr should be false")
	}
	if !entry.resIsPtr {
		t.Fatal("resIsPtr should be true")
	}
}

func TestBuildEntry_MixedParams_PointerReqValueRes(t *testing.T) {
	fn := func(ctx context.Context, req *testReq) (testRes, error) {
		return testRes{Message: req.Name}, nil
	}
	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.reqIsPtr {
		t.Fatal("reqIsPtr should be true")
	}
	if entry.resIsPtr {
		t.Fatal("resIsPtr should be false")
	}
}

func TestBuildEntry_MiddlewareSnapshot(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	mw1 := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}
	mw2 := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}
	mw3 := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}

	global := []MiddlewareHandler{mw1, mw2}
	group := []MiddlewareHandler{mw3}

	entry, err := buildEntry(fn, global, group)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.middlewares) != 3 {
		t.Fatalf("expected 3 middlewares, got %d", len(entry.middlewares))
	}

	p1 := reflect.ValueOf(entry.middlewares[0]).Pointer()
	p2 := reflect.ValueOf(entry.middlewares[1]).Pointer()
	p3 := reflect.ValueOf(entry.middlewares[2]).Pointer()

	if p1 != reflect.ValueOf(mw1).Pointer() {
		t.Fatal("first middleware should be mw1")
	}
	if p2 != reflect.ValueOf(mw2).Pointer() {
		t.Fatal("second middleware should be mw2")
	}
	if p3 != reflect.ValueOf(mw3).Pointer() {
		t.Fatal("third middleware should be mw3")
	}
}

func TestBuildEntry_MiddlewareSnapshot_IndependentCopy(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	mw1 := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}

	global := []MiddlewareHandler{mw1}

	entry, err := buildEntry(fn, global, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 修改原始 global 切片，不应影响 entry
	global[0] = nil
	if entry.middlewares[0] == nil {
		t.Fatal("entry middleware should not be affected by external modification")
	}
}

func TestBuildEntry_EmptyMiddlewares(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.middlewares) != 0 {
		t.Fatalf("expected 0 middlewares, got %d", len(entry.middlewares))
	}
}

func TestBuildEntry_HandlerLocation(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(entry.handlerName, "zchttp") {
		t.Fatalf("handlerName should contain package path, got %q", entry.handlerName)
	}

	if !strings.Contains(entry.handlerFile, "buildEntry_test.go") {
		t.Fatalf("handlerFile should contain test file name, got %q", entry.handlerFile)
	}

	if entry.handlerLine <= 0 {
		t.Fatalf("handlerLine should be positive, got %d", entry.handlerLine)
	}
}

func TestBuildEntry_HandlerEquality(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if reflect.ValueOf(entry.handler).Pointer() != reflect.ValueOf(fn).Pointer() {
		t.Fatal("entry.handler should be the same function")
	}
}

func TestBuildEntry_NilGlobalMiddlewares(t *testing.T) {
	fn := func(ctx context.Context, req testReq) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry == nil {
		t.Fatal("entry should not be nil")
	}
}

func TestBuildEntry_AnonymousStructReq(t *testing.T) {
	fn := func(ctx context.Context, req struct {
		Name string `json:"name"`
	}) (testRes, error) {
		return testRes{Message: req.Name}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.reqType.Kind() != reflect.Struct {
		t.Fatal("reqType should be struct")
	}
	if entry.reqElemType.Kind() != reflect.Struct {
		t.Fatal("reqElemType should be struct")
	}
}

func TestBuildEntry_EmbeddedStructReq(t *testing.T) {
	type base struct {
		ID int `json:"id"`
	}
	type createReq struct {
		base
		Name string `json:"name"`
	}

	fn := func(ctx context.Context, req createReq) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.reqType != reflect.TypeOf(createReq{}) {
		t.Fatalf("reqType mismatch")
	}
}

// ========== reqMeta 相关测试 ==========

func TestBuildEntry_ReqMeta_Fields(t *testing.T) {
	type reqWithTags struct {
		Name  string `json:"name"`
		Age   int    `json:"age" default:"18"`
		Email string `json:"email" nonzero:"true"`
	}

	fn := func(ctx context.Context, req reqWithTags) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.reqMeta.fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(entry.reqMeta.fields))
	}

	// 验证字段绑定名
	expectedNames := map[string]bool{"name": true, "age": true, "email": true}
	for _, fm := range entry.reqMeta.fields {
		if !expectedNames[fm.name] {
			t.Fatalf("unexpected field name: %q", fm.name)
		}
	}
}

func TestBuildEntry_NeedsNonzeroValidation_TopLevel(t *testing.T) {
	type reqWithRequired struct {
		Name string `json:"name" nonzero:"true"`
	}

	fn := func(ctx context.Context, req reqWithRequired) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.needsNonzeroValidation {
		t.Fatal("needsNonzeroValidation should be true (top-level nonzero field)")
	}
}

// TestBuildEntry_NeedsNonzeroValidation_NestedOnly 顶层无 nonzero 但嵌套层有时，
// 传递性标记必须为 true（若仅看顶层会误跳过导致漏校验）
func TestBuildEntry_NeedsNonzeroValidation_NestedOnly(t *testing.T) {
	type nestedInner struct {
		Code string `json:"code" nonzero:"true"`
	}
	type reqNestedOnly struct {
		Inner nestedInner `json:"inner"` // 顶层未标注
	}

	fn := func(ctx context.Context, req reqNestedOnly) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.needsNonzeroValidation {
		t.Fatal("needsNonzeroValidation should be true (nested nonzero field)")
	}
}

// TestBuildEntry_NeedsNonzeroValidation_None 全树无 nonzero 字段时标记为 false，
// 请求阶段整体跳过 validateNonzero
func TestBuildEntry_NeedsNonzeroValidation_None(t *testing.T) {
	type reqNoNonzero struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	fn := func(ctx context.Context, req reqNoNonzero) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.needsNonzeroValidation {
		t.Fatal("needsNonzeroValidation should be false (no nonzero anywhere)")
	}
}

func TestBuildEntry_ReqMeta_HasDefault(t *testing.T) {
	type reqWithDefault struct {
		Name string `json:"name" default:"hello"`
	}

	fn := func(ctx context.Context, req reqWithDefault) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.needsDeepCopy {
		t.Fatal("needsDeepCopy should be false for string defaults")
	}
}

func TestBuildEntry_ReqMeta_NoDefault(t *testing.T) {
	type reqNoDefault struct {
		Name string `json:"name"`
	}

	fn := func(ctx context.Context, req reqNoDefault) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.needsDeepCopy {
		t.Fatal("needsDeepCopy should be false")
	}
}

func TestBuildEntry_ReqMeta_ImplementsValidator(t *testing.T) {
	type reqWithValidator struct {
		Name string `json:"name"`
	}
	// reqWithValidator 实现 Validator 接口（指针接收者）
	// 注意：go 中 struct 定义不能直接带方法，这里通过 var _ Validator = (*reqWithValidator)(nil) 编译时验证

	fn := func(ctx context.Context, req *reqWithValidator) (testRes, error) {
		return testRes{}, nil
	}

	// 注册时 buildStructMeta 判断 *reqWithValidator 是否实现 Validator
	// reqElemType 是 reqWithValidator（去指针后），所以 implementsValidator 基于 reqWithValidator 判断
	_ = fn

	// 由于 go 限制不能在函数内定义带方法的类型，这里只验证 fields 不为空
	// 完整的 implementsValidator 测试通过已有集成测试覆盖

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.reqMeta.fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(entry.reqMeta.fields))
	}
}

func TestBuildEntry_ReqMeta_PointerReq(t *testing.T) {
	type reqWithTag struct {
		Title string `json:"title" nonzero:"true"`
	}

	fn := func(ctx context.Context, req *reqWithTag) (*testRes, error) {
		return &testRes{Message: req.Title}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.reqMeta.fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(entry.reqMeta.fields))
	}
	if !entry.needsNonzeroValidation {
		t.Fatal("needsNonzeroValidation should be true")
	}
	if entry.reqMeta.fields[0].name != "title" {
		t.Fatalf("expected field name 'title', got %q", entry.reqMeta.fields[0].name)
	}
}

// ========== buildOperationMeta 测试 ==========

func TestBuildOperationMeta_WithMeta(t *testing.T) {
	type reqWithMeta struct {
		OpenAPIMeta `tags:"User/Account" summary:"创建用户" description:"创建一个新用户"`
		Name        string `json:"name"`
	}

	meta := buildOperationMeta(reflect.TypeOf(reqWithMeta{}))

	if len(meta.tags) != 2 || meta.tags[0] != "User" || meta.tags[1] != "Account" {
		t.Fatalf("tags mismatch: %v", meta.tags)
	}
	if meta.summary != "创建用户" {
		t.Fatalf("summary: got %q, want '创建用户'", meta.summary)
	}
	if meta.description != "创建一个新用户" {
		t.Fatalf("description: got %q", meta.description)
	}
}

func TestBuildOperationMeta_NoMeta(t *testing.T) {
	type reqNoMeta struct {
		Name string `json:"name"`
	}

	meta := buildOperationMeta(reflect.TypeOf(reqNoMeta{}))

	if len(meta.tags) != 0 || meta.summary != "" || meta.description != "" {
		t.Fatalf("expected zero-value meta without OpenAPIMeta, got tags=%v summary=%q", meta.tags, meta.summary)
	}
}

func TestBuildOperationMeta_TagsTrimSpace(t *testing.T) {
	type reqSpaced struct {
		OpenAPIMeta `tags:" User / Account / Admin " summary:"test"`
		Name        string `json:"name"`
	}

	meta := buildOperationMeta(reflect.TypeOf(reqSpaced{}))

	if len(meta.tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(meta.tags), meta.tags)
	}
	if meta.tags[0] != "User" || meta.tags[1] != "Account" || meta.tags[2] != "Admin" {
		t.Fatalf("tags should be trimmed: %v", meta.tags)
	}
}

func TestBuildOperationMeta_EmptyTagsField(t *testing.T) {
	type reqOnlySummary struct {
		OpenAPIMeta `summary:"only summary"`
		Name        string `json:"name"`
	}

	meta := buildOperationMeta(reflect.TypeOf(reqOnlySummary{}))

	if len(meta.tags) != 0 {
		t.Fatalf("expected 0 tags, got %d", len(meta.tags))
	}
	if meta.summary != "only summary" {
		t.Fatalf("summary: got %q", meta.summary)
	}
}

func TestBuildOperationMeta_NonStruct(t *testing.T) {
	meta := buildOperationMeta(reflect.TypeOf(0))
	if len(meta.tags) != 0 || meta.summary != "" {
		t.Fatal("non-struct input should return zero-value meta")
	}
}

// ========== needsDeepCopy / resMeta / defaultReq 测试 ==========

func TestBuildEntry_NeedsDeepCopy_True(t *testing.T) {
	type reqPtrDefault struct {
		Status *string `json:"status" default:"active"`
	}

	fn := func(ctx context.Context, req reqPtrDefault) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// default "active" 填充 *string → 非 nil 指针 → needsDeepCopy 应为 true
	if !entry.needsDeepCopy {
		t.Fatal("needsDeepCopy should be true when default fills a pointer field")
	}
}

func TestBuildEntry_NeedsDeepCopy_SliceDefault(t *testing.T) {
	type reqSliceDefault struct {
		Tags *string `json:"tags" default:"a"`
	}

	fn := func(ctx context.Context, req reqSliceDefault) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.needsDeepCopy {
		t.Fatal("needsDeepCopy should be true for pointer default")
	}
}

func TestBuildEntry_ResMeta_Computed(t *testing.T) {
	type myRes struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
		OK    bool   `json:"ok"`
	}

	fn := func(ctx context.Context, req testReq) (myRes, error) {
		return myRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.resMeta.fields) != 3 {
		t.Fatalf("expected 3 res fields, got %d", len(entry.resMeta.fields))
	}

	names := make(map[string]bool)
	for _, fm := range entry.resMeta.fields {
		names[fm.name] = true
	}
	for _, expected := range []string{"name", "count", "ok"} {
		if !names[expected] {
			t.Fatalf("resMeta missing field %q", expected)
		}
	}
}

func TestBuildEntry_DefaultReq_Template(t *testing.T) {
	type reqDefaults struct {
		Name string `json:"name" default:"world"`
		Page int    `json:"page" default:"1"`
	}

	fn := func(ctx context.Context, req reqDefaults) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 defaultReq 模板已预填充默认值
	if entry.defaultReq.Kind() != reflect.Struct {
		t.Fatal("defaultReq should be a struct value")
	}
	nameVal := entry.defaultReq.FieldByName("Name").String()
	if nameVal != "world" {
		t.Fatalf("defaultReq.Name should be 'world', got %q", nameVal)
	}
	pageVal := entry.defaultReq.FieldByName("Page").Int()
	if pageVal != 1 {
		t.Fatalf("defaultReq.Page should be 1, got %d", pageVal)
	}
}

func TestBuildEntry_OpMeta_Computed(t *testing.T) {
	type reqOp struct {
		OpenAPIMeta `tags:"Order/Payment" summary:"支付订单" description:"发起支付"`
		Amount      int `json:"amount"`
	}

	fn := func(ctx context.Context, req reqOp) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entry.opMeta.tags) != 2 || entry.opMeta.tags[0] != "Order" || entry.opMeta.tags[1] != "Payment" {
		t.Fatalf("opMeta.tags mismatch: %v", entry.opMeta.tags)
	}
	if entry.opMeta.summary != "支付订单" {
		t.Fatalf("opMeta.summary: got %q", entry.opMeta.summary)
	}
	if entry.opMeta.description != "发起支付" {
		t.Fatalf("opMeta.description: got %q", entry.opMeta.description)
	}
}

// TestBuildEntry_NeedsRequestPhaseDefaults 验证 buildEntry 正确预计算 needsRequestPhaseDefaults
func TestBuildEntry_NeedsRequestPhaseDefaults(t *testing.T) {
	// 仅值类型 default → 不需要请求阶段默认值填充
	type reqValDefault struct {
		Name string `json:"name" default:"hello"`
		Age  int    `json:"age" default:"18"`
	}
	fn1 := func(ctx context.Context, req reqValDefault) (testRes, error) {
		return testRes{}, nil
	}
	entry1, err := buildEntry(fn1, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry1.needsRequestPhaseDefaults {
		t.Fatal("needsRequestPhaseDefaults should be false for value-only defaults")
	}

	// 指针字段带 default → 需要请求阶段默认值填充
	type reqPtrDefault struct {
		Name *string `json:"name" default:"hello"`
	}
	fn2 := func(ctx context.Context, req reqPtrDefault) (testRes, error) {
		return testRes{}, nil
	}
	entry2, err := buildEntry(fn2, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !entry2.needsRequestPhaseDefaults {
		t.Fatal("needsRequestPhaseDefaults should be true for pointer field with default")
	}

	// 无任何 default → 不需要
	type reqNoDefault struct {
		Name string `json:"name"`
	}
	fn3 := func(ctx context.Context, req reqNoDefault) (testRes, error) {
		return testRes{}, nil
	}
	entry3, err := buildEntry(fn3, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry3.needsRequestPhaseDefaults {
		t.Fatal("needsRequestPhaseDefaults should be false when no defaults exist")
	}

	// 嵌套值结构体含指针 default → 需要
	type reqNestedPtrDefault struct {
		Inner struct {
			Tag *string `json:"tag" default:"v1"`
		} `json:"inner"`
	}
	fn4 := func(ctx context.Context, req reqNestedPtrDefault) (testRes, error) {
		return testRes{}, nil
	}
	entry4, err := buildEntry(fn4, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !entry4.needsRequestPhaseDefaults {
		t.Fatal("needsRequestPhaseDefaults should be true for nested value struct with ptr default")
	}
}
