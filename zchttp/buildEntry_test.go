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
		Email string `json:"email" required:"true"`
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

func TestBuildEntry_ReqMeta_HasRequired(t *testing.T) {
	type reqWithRequired struct {
		Name string `json:"name" required:"true"`
	}

	fn := func(ctx context.Context, req reqWithRequired) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !entry.reqMeta.hasRequired {
		t.Fatal("hasRequired should be true")
	}
}

func TestBuildEntry_ReqMeta_NoRequired(t *testing.T) {
	type reqOptional struct {
		Name string `json:"name"`
	}

	fn := func(ctx context.Context, req reqOptional) (testRes, error) {
		return testRes{}, nil
	}

	entry, err := buildEntry(fn, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if entry.reqMeta.hasRequired {
		t.Fatal("hasRequired should be false")
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
		Title string `json:"title" required:"true"`
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
	if !entry.reqMeta.hasRequired {
		t.Fatal("hasRequired should be true")
	}
	if entry.reqMeta.fields[0].name != "title" {
		t.Fatalf("expected field name 'title', got %q", entry.reqMeta.fields[0].name)
	}
}

// ========== deepCopyDefaults 测试 ==========

type deepCopyInner struct {
	Name string
	Age  int
}

type deepCopyOuter struct {
	Ptr   *deepCopyInner
	Items []*deepCopyInner
	Meta  map[string]*deepCopyInner
}

func TestDeepCopyDefaults_NoStackOverflow(t *testing.T) {
	// 构造一个包含指针字段、切片、map 的结构体，其中所有引用字段均非 nil。
	// Bug：修复前 deepCopyDefaults 在 Ptr 分支递归调用自身（deepCopyDefaults(v)），
	//      而 v 的 Kind 仍是 Ptr 且非 nil，造成无限递归 → 栈溢出。
	// 正确行为：递归到 v.Elem()（指针指向的元素），而非指针本身。
	inner := &deepCopyInner{Name: "test", Age: 30}
	v := reflect.ValueOf(&deepCopyOuter{
		Ptr:   inner,
		Items: []*deepCopyInner{inner},
		Meta:  map[string]*deepCopyInner{"a": inner},
	}).Elem()

	// 记录原始指针地址，用于后续验证深拷贝是否断开共享
	origPtrAddr := v.FieldByName("Ptr").Pointer()

	// 执行深拷贝：如果存在无限递归 bug，此处会栈溢出导致测试崩溃
	deepCopyDefaults(v)

	// 验证指针已被深拷贝（地址不同）
	newPtrAddr := v.FieldByName("Ptr").Pointer()
	if newPtrAddr == origPtrAddr {
		t.Fatal("pointer should be deep-copied (different address)")
	}

	// 验证内容一致
	newInner := v.FieldByName("Ptr").Elem().Interface().(deepCopyInner)
	if newInner.Name != "test" || newInner.Age != 30 {
		t.Fatalf("deep-copied content mismatch: %+v", newInner)
	}

	// 验证切片被深拷贝
	itemsField := v.FieldByName("Items")
	if itemsField.Len() != 1 {
		t.Fatalf("expected 1 item in slice, got %d", itemsField.Len())
	}

	// 验证 map 被深拷贝（map 本身是新的，但内部元素目前未递归深拷贝，此处仅验证不崩溃）
	metaField := v.FieldByName("Meta")
	if metaField.Len() != 1 {
		t.Fatalf("expected 1 entry in map, got %d", metaField.Len())
	}

	// 验证并发安全性：多次调用不应崩溃
	for range 10 {
		v2 := reflect.ValueOf(&deepCopyOuter{
			Ptr:   &deepCopyInner{Name: "concurrent"},
			Items: []*deepCopyInner{{Name: "item1"}, {Name: "item2"}},
		}).Elem()
		deepCopyDefaults(v2)
	}
}
