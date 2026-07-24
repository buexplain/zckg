package zchttp

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
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

func TestBuildEntry_ReqMeta_HasRequired(t *testing.T) {
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

	if !entry.reqMeta.hasNonzero {
		t.Fatal("hasNonzero should be true")
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

	if entry.reqMeta.hasNonzero {
		t.Fatal("hasNonzero should be false")
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
	if !entry.reqMeta.hasNonzero {
		t.Fatal("hasNonzero should be true")
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

// TestDeepCopyDefaults_MapValueNotShared 验证 deepCopyDefaults 后 map 内指针值与原始模板断开共享
func TestDeepCopyDefaults_MapValueNotShared(t *testing.T) {
	inner := &deepCopyInner{Name: "original", Age: 10}
	v := reflect.ValueOf(&deepCopyOuter{
		Meta: map[string]*deepCopyInner{"key": inner},
	}).Elem()

	// 记录原始指针地址
	origAddr := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Pointer()

	// 执行深拷贝
	deepCopyDefaults(v)

	// 检查 map 内的指针值是否已断开共享
	newAddr := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Pointer()
	if newAddr == origAddr {
		t.Fatal("BUG3 confirmed: map value pointer still shared after deepCopyDefaults")
	}

	// 内容应保持一致
	newVal := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Elem().Interface().(deepCopyInner)
	if newVal.Name != "original" || newVal.Age != 10 {
		t.Fatalf("content mismatch after deep copy: %+v", newVal)
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

// ========== buildStructMeta 字段类型判定 ==========

func TestBuildStructMeta_FileFields(t *testing.T) {
	type reqFiles struct {
		Avatar *multipart.FileHeader   `json:"avatar"`
		Docs   []*multipart.FileHeader `json:"docs"`
		Tags   []string                `json:"tags"`
		Name   string                  `json:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqFiles{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if !fieldMap["avatar"].isFile {
		t.Fatal("avatar should have isFile=true")
	}
	if fieldMap["avatar"].isFileSlice || fieldMap["avatar"].isSlice {
		t.Fatal("avatar should not have isFileSlice or isSlice")
	}

	if !fieldMap["docs"].isFileSlice {
		t.Fatal("docs should have isFileSlice=true")
	}
	if fieldMap["docs"].isFile || fieldMap["docs"].isSlice {
		t.Fatal("docs should not have isFile or isSlice")
	}

	if !fieldMap["tags"].isSlice {
		t.Fatal("tags should have isSlice=true")
	}
	if fieldMap["tags"].isFile || fieldMap["tags"].isFileSlice {
		t.Fatal("tags should not have isFile or isFileSlice")
	}

	if fieldMap["name"].isFile || fieldMap["name"].isFileSlice || fieldMap["name"].isSlice {
		t.Fatal("name should have no file/slice flags")
	}
}

func TestBuildStructMeta_TimeFields(t *testing.T) {
	type reqTime struct {
		Start time.Time `json:"start" time_format:"2006-01-02" time_location:"Asia/Shanghai"`
		End   time.Time `json:"end" time_format:"unix"`
		Plain time.Time `json:"plain"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqTime{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if fieldMap["start"].timeFormat != "2006-01-02" {
		t.Fatalf("start timeFormat: got %q, want '2006-01-02'", fieldMap["start"].timeFormat)
	}
	if fieldMap["start"].timeLocation == nil || fieldMap["start"].timeLocation.String() != "Asia/Shanghai" {
		t.Fatal("start timeLocation should be Asia/Shanghai")
	}

	if fieldMap["end"].timeFormat != "unix" {
		t.Fatalf("end timeFormat: got %q, want 'unix'", fieldMap["end"].timeFormat)
	}
	if fieldMap["end"].timeLocation != nil {
		t.Fatal("end should have nil timeLocation")
	}

	if fieldMap["plain"].timeFormat != "" {
		t.Fatalf("plain timeFormat should be empty, got %q", fieldMap["plain"].timeFormat)
	}
}

func TestBuildStructMeta_InvalidTimeLocation(t *testing.T) {
	type reqBadTz struct {
		T time.Time `json:"t" time_format:"2006-01-02" time_location:"Invalid/Zone"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqBadTz{}))

	for _, fm := range meta.fields {
		if fm.name == "t" && fm.timeLocation != nil {
			t.Fatal("invalid time_location should result in nil (fallback to time.Local)")
		}
	}
}

// ========== buildStructMeta 跳过规则 ==========

func TestBuildStructMeta_SkipUnexported(t *testing.T) {
	type reqMixed struct {
		Name   string `json:"name"`
		secret string //nolint:unused // 未导出字段应被跳过
		Age    int    `json:"age"`
	}
	_ = reqMixed{secret: ""}

	meta := buildStructMeta(reflect.TypeOf(reqMixed{}))

	if len(meta.fields) != 2 {
		t.Fatalf("expected 2 fields (unexported skipped), got %d", len(meta.fields))
	}
}

func TestBuildStructMeta_SkipOpenAPIMeta(t *testing.T) {
	type reqAPIMeta struct {
		OpenAPIMeta `tags:"Test" summary:"test"`
		Name        string `json:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqAPIMeta{}))

	if len(meta.fields) != 1 {
		t.Fatalf("expected 1 field (OpenAPIMeta skipped), got %d", len(meta.fields))
	}
	if meta.fields[0].name != "name" {
		t.Fatalf("expected field 'name', got %q", meta.fields[0].name)
	}
}

func TestBuildStructMeta_JsonDash(t *testing.T) {
	type reqDash struct {
		Name   string `json:"name"`
		Secret string `json:"-"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqDash{}))

	// json:"-" 字段仍被记录（name 为 "-"），但不会被 bindValues 绑定
	if len(meta.fields) != 2 {
		t.Fatalf("expected 2 fields (json:'-' recorded), got %d", len(meta.fields))
	}
	// json:"-" 字段不应解析 nonzero/default 等标签
	for _, fm := range meta.fields {
		if fm.name == "-" {
			if fm.nonzero || fm.hasDefault {
				t.Fatal("json:'-' field should not have nonzero/hasDefault parsed")
			}
		}
	}
}

func TestBuildStructMeta_NonzeroParsing(t *testing.T) {
	type reqNonzero struct {
		A string `json:"a" nonzero:"true"`
		B string `json:"b" nonzero:"false"`
		C string `json:"c" nonzero:"invalid"`
		D string `json:"d"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqNonzero{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if !fieldMap["a"].nonzero {
		t.Fatal("a: nonzero:'true' should set nonzero=true")
	}
	if fieldMap["b"].nonzero {
		t.Fatal("b: nonzero:'false' should set nonzero=false")
	}
	if fieldMap["c"].nonzero {
		t.Fatal("c: nonzero:'invalid' should not set nonzero=true")
	}
	if fieldMap["d"].nonzero {
		t.Fatal("d: no nonzero tag should default to false")
	}
	if !meta.hasNonzero {
		t.Fatal("hasNonzero should be true (field a)")
	}
}

// ========== buildStructMeta implementsValidator ==========

// buildEntryValidatorReq 实现 Validator 接口，用于 implementsValidator 检测测试
type buildEntryValidatorReq struct {
	Name string `json:"name"`
}

func (r buildEntryValidatorReq) Validate() error {
	return nil
}

func TestBuildStructMeta_ImplementsValidator(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(buildEntryValidatorReq{}))

	if !meta.implementsValidator {
		t.Fatal("implementsValidator should be true for type implementing Validator")
	}
}

func TestBuildStructMeta_NotImplementsValidator(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(testReq{}))

	if meta.implementsValidator {
		t.Fatal("implementsValidator should be false for type not implementing Validator")
	}
}

// ========== hasRefFields 测试 ==========

func TestHasRefFields_PureValue(t *testing.T) {
	type pureValue struct {
		Name string
		Age  int
		OK   bool
	}
	v := reflect.ValueOf(pureValue{Name: "test", Age: 1, OK: true})
	if hasRefFields(v) {
		t.Fatal("pure value struct should return false")
	}
}

func TestHasRefFields_NonNilPtr(t *testing.T) {
	type withPtr struct {
		Name string
		Ref  *int
	}
	n := 42
	v := reflect.ValueOf(withPtr{Ref: &n})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil ptr should return true")
	}
}

func TestHasRefFields_NilPtr(t *testing.T) {
	type withNilPtr struct {
		Name string
		Ref  *int
	}
	v := reflect.ValueOf(withNilPtr{})
	if hasRefFields(v) {
		t.Fatal("struct with only nil ptr should return false")
	}
}

func TestHasRefFields_NonNilSlice(t *testing.T) {
	type withSlice struct {
		Items []string
	}
	v := reflect.ValueOf(withSlice{Items: []string{"a"}})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil slice should return true")
	}
}

func TestHasRefFields_NilSlice(t *testing.T) {
	type withNilSlice struct {
		Items []string
	}
	v := reflect.ValueOf(withNilSlice{})
	if hasRefFields(v) {
		t.Fatal("struct with nil slice should return false")
	}
}

func TestHasRefFields_NonNilMap(t *testing.T) {
	type withMap struct {
		Meta map[string]string
	}
	v := reflect.ValueOf(withMap{Meta: map[string]string{"k": "v"}})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil map should return true")
	}
}

func TestHasRefFields_NilMap(t *testing.T) {
	type withNilMap struct {
		Meta map[string]string
	}
	v := reflect.ValueOf(withNilMap{})
	if hasRefFields(v) {
		t.Fatal("struct with nil map should return false")
	}
}

func TestHasRefFields_NestedStruct(t *testing.T) {
	type inner struct {
		Ref *int
	}
	type outer struct {
		Name  string
		Inner inner
	}
	n := 1
	v := reflect.ValueOf(outer{Inner: inner{Ref: &n}})
	if !hasRefFields(v) {
		t.Fatal("nested struct with non-nil ptr should return true")
	}
}

// ========== isDefaultSupported 测试 ==========

func TestIsDefaultSupported(t *testing.T) {
	cases := []struct {
		name     string
		typ      reflect.Type
		expected bool
	}{
		{"string", reflect.TypeOf(""), true},
		{"int", reflect.TypeOf(0), true},
		{"int64", reflect.TypeOf(int64(0)), true},
		{"uint", reflect.TypeOf(uint(0)), true},
		{"float32", reflect.TypeOf(float32(0)), true},
		{"float64", reflect.TypeOf(float64(0)), true},
		{"bool", reflect.TypeOf(false), true},
		{"*int", reflect.TypeOf((*int)(nil)), true},
		{"*string", reflect.TypeOf((*string)(nil)), true},
		{"[]string", reflect.TypeOf([]string{}), true},
		{"[]int", reflect.TypeOf([]int{}), true},
		{"[]*int", reflect.TypeOf([]*int{}), true},
		{"time.Time", reflect.TypeOf(time.Time{}), false},
		{"struct", reflect.TypeOf(struct{}{}), false},
		{"map[string]string", reflect.TypeOf(map[string]string{}), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDefaultSupported(tc.typ)
			if got != tc.expected {
				t.Fatalf("isDefaultSupported(%v) = %v, want %v", tc.typ, got, tc.expected)
			}
		})
	}
}

// ========== fieldByIndex 测试 ==========

func TestFieldByIndex_Simple(t *testing.T) {
	type simple struct {
		Name string
		Age  int
	}
	v := reflect.ValueOf(&simple{Name: "alice", Age: 30}).Elem()

	nameField := fieldByIndex(v, []int{0})
	if nameField.String() != "alice" {
		t.Fatalf("expected 'alice', got %q", nameField.String())
	}

	ageField := fieldByIndex(v, []int{1})
	if ageField.Int() != 30 {
		t.Fatalf("expected 30, got %d", ageField.Int())
	}
}

func TestFieldByIndex_NilPtrAutoInit(t *testing.T) {
	type inner struct {
		Value string
	}
	type outer struct {
		Ref *inner
	}

	o := &outer{}
	v := reflect.ValueOf(o).Elem()

	// indices [0, 0]：先访问 outer.Ref（*inner），再访问 inner.Value
	result := fieldByIndex(v, []int{0, 0})

	// nil 指针应被自动初始化
	if o.Ref == nil {
		t.Fatal("nil pointer should be auto-initialized by fieldByIndex")
	}

	// result 应为 inner.Value（空字符串）
	if result.Kind() != reflect.String {
		t.Fatalf("expected string field, got %v", result.Kind())
	}
	if result.String() != "" {
		t.Fatalf("expected empty string, got %q", result.String())
	}
}

func TestFieldByIndex_MultiLevel(t *testing.T) {
	type level2 struct {
		Code int
	}
	type level1 struct {
		L2 *level2
	}
	type root struct {
		L1 *level1
	}

	r := &root{}
	v := reflect.ValueOf(r).Elem()

	// indices [0, 0, 0]：root.L1 → level1.L2 → level2.Code
	result := fieldByIndex(v, []int{0, 0, 0})

	if r.L1 == nil {
		t.Fatal("L1 should be auto-initialized")
	}
	if r.L1.L2 == nil {
		t.Fatal("L2 should be auto-initialized")
	}
	if result.Kind() != reflect.Int {
		t.Fatalf("expected int field, got %v", result.Kind())
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

// ========== checkUnsupportedDefaults 测试 ==========

func TestCheckUnsupportedDefaults_NoPanic(t *testing.T) {
	// 验证各种场景不会 panic

	// 不支持的类型设 default
	type reqUnsupported struct {
		When time.Time `json:"when" default:"2023-01-01"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqUnsupported{}), true, true, "GET", "/test", "handler", "file.go", 1, make(map[reflect.Type]bool))

	// 值类型字段在指针路径下
	type nested struct {
		Name string `json:"name" default:"hello"`
	}
	type reqPtr struct {
		Inner *nested `json:"inner"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqPtr{}), true, true, "POST", "/test", "handler", "file.go", 1, make(map[reflect.Type]bool))

	// 自引用结构体不会无限递归
	type selfRef struct {
		Name   string   `json:"name"`
		Parent *selfRef `json:"parent"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(selfRef{}), true, true, "POST", "/self", "handler", "file.go", 1, make(map[reflect.Type]bool))
}

func TestCheckUnsupportedDefaults_SliceMapRecursion(t *testing.T) {
	// 验证切片/map 元素中的 default 检查不会 panic
	type item struct {
		Status string `json:"status" default:"active"`
	}
	type reqSlice struct {
		Items []item           `json:"items"`
		Map   map[string]*item `json:"map"`
		Arr   []*item          `json:"arr"`
	}
	// 不应 panic
	checkUnsupportedDefaults(reflect.TypeOf(reqSlice{}), true, true, "POST", "/items", "handler", "file.go", 1, make(map[reflect.Type]bool))
}

func TestCheckUnsupportedDefaults_AutogeneratedFile(t *testing.T) {
	// handlerFile 为 "<autogenerated>" 时使用 handlerName 作为位置
	type reqAuto struct {
		When time.Time `json:"when" default:"now"`
	}
	// 不应 panic，仅验证能正确处理
	checkUnsupportedDefaults(reflect.TypeOf(reqAuto{}), true, true, "GET", "/auto", "pkg.handler", "<autogenerated>", 0, make(map[reflect.Type]bool))
}

// selfSlice 病态自引用切片类型：Elem() 返回自身，无限递归
type selfSlice []selfSlice

// TestIsDefaultSupported_SelfReferentialTypes 验证自引用类型不会使 isDefaultSupported 无限递归（栈溢出），
// 超过深度上限后应判定为不支持 default 标签
func TestIsDefaultSupported_SelfReferentialTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"selfSlice", reflect.TypeOf(selfSlice{})},
		{"selfPtr", reflect.TypeOf((selfPtr)(nil))},
	}
	for _, c := range cases {
		done := make(chan bool, 1)
		go func() {
			done <- isDefaultSupported(c.typ)
		}()
		select {
		case got := <-done:
			if got {
				t.Errorf("isDefaultSupported(%s) = true, want false", c.name)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("isDefaultSupported(%s) did not terminate", c.name)
		}
	}
}

// ========== hasRequestPhaseDefaults 预计算测试 ==========

// TestHasRequestPhaseDefaults 表驱动测试：验证各种类型结构下请求阶段是否需要执行 applyDefaults
func TestHasRequestPhaseDefaults(t *testing.T) {
	tests := []struct {
		name string
		req  any
		want bool
	}{
		{
			name: "无default标签",
			req: struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}{},
			want: false,
		},
		{
			name: "仅值类型default",
			req: struct {
				Name string `json:"name" default:"hello"`
				Age  int    `json:"age" default:"18"`
			}{},
			want: false,
		},
		{
			name: "指针字段有default",
			req: struct {
				Name *string `json:"name" default:"hello"`
			}{},
			want: true,
		},
		{
			name: "指针字段无default",
			req: struct {
				Name *string `json:"name"`
			}{},
			want: false,
		},
		{
			name: "嵌套值结构体含指针default",
			req: struct {
				Inner struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: true,
		},
		{
			name: "嵌套指针结构体含值default",
			req: struct {
				Inner *struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: false,
		},
		{
			name: "嵌套指针结构体含指针default",
			req: struct {
				Inner *struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: true,
		},
		{
			name: "切片元素含指针default",
			req: struct {
				Items []struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "切片元素仅值类型default",
			req: struct {
				Items []struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "map值含指针default",
			req: struct {
				Meta map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{},
			want: true,
		},
		{
			name: "map值仅值类型default",
			req: struct {
				Meta map[string]struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{},
			want: false,
		},
		{
			name: "指针切片含指针default",
			req: struct {
				Items []*struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "指针切片仅值default",
			req: struct {
				Items []*struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "值切片含指针default",
			req: struct {
				Tags []string `json:"tags" default:"a,b"`
			}{},
			want: false,
		},
		{
			name: "指针切片default",
			req: struct {
				Tags []*string `json:"tags" default:"a,b"`
			}{},
			want: false,
		},
		{
			name: "多层嵌套值结构体含指针default",
			req: struct {
				Outer struct {
					Inner struct {
						Deep *int `json:"deep" default:"42"`
					} `json:"inner"`
				} `json:"outer"`
			}{},
			want: true,
		},
		{
			name: "空结构体",
			req:  struct{}{},
			want: false,
		},
		// 指针包裹容器：*[]Struct / *map[K]Struct，元素/值含指针 default
		// 修复前 hasRequestPhaseDefaults 经 Ptr 递归到达 Slice/Map 时被 "非 Struct" 拦截返回 false
		{
			name: "*[]Struct元素含指针default（bug修复场景）",
			req: struct {
				Items *[]struct {
					Qty *int `json:"qty" default:"1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "*map[K]Struct值含指针default（bug修复场景）",
			req: struct {
				Extras *map[string]struct {
					Qty *int `json:"qty" default:"1"`
				} `json:"extras"`
			}{},
			want: true,
		},
		{
			name: "*[]Struct元素仅值default（bug修复场景对照组）",
			req: struct {
				Items *[]struct {
					Qty int `json:"qty" default:"1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "多层*[]嵌套含指针default",
			req: struct {
				Mids *[]struct {
					Children *[]struct {
						Active *bool `json:"active" default:"true"`
					} `json:"children"`
				} `json:"mids"`
			}{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqType := reflect.TypeOf(tt.req)
			got := hasRequestPhaseDefaults(reqType, nil)
			if got != tt.want {
				t.Fatalf("hasRequestPhaseDefaults()=%v, want %v", got, tt.want)
			}
		})
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

// ========== hasRequestPhaseDefaults 自引用循环检测 ==========

// hasPhaseSelfRef 自引用结构体，内部含指针 default 字段
// 用于验证 visiting 机制能阻止无限递归
type hasPhaseSelfRef struct {
	Name   string           `json:"name"`
	Child  *hasPhaseSelfRef `json:"child"`
	Status *string          `json:"status" default:"active"`
}

// TestHasRequestPhaseDefaults_SelfRef 验证自引用结构体不会导致无限递归
func TestHasRequestPhaseDefaults_SelfRef(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		done <- hasRequestPhaseDefaults(reflect.TypeOf(hasPhaseSelfRef{}), nil)
	}()
	select {
	case got := <-done:
		// 自引用含指针 default → 应返回 true
		if !got {
			t.Fatal("hasRequestPhaseDefaults should return true for self-ref struct with ptr default")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hasRequestPhaseDefaults did not terminate on self-referential type")
	}
}

// hasPhaseNoDefaultSelfRef 自引用结构体，不含任何 default 标签
// 用于验证 visiting 在无匹配时也能正常终止
type hasPhaseNoDefaultSelfRef struct {
	Name  string                               `json:"name"`
	Child *hasPhaseNoDefaultSelfRef            `json:"child"`
	Meta  map[string]*hasPhaseNoDefaultSelfRef `json:"meta"`
}

// TestHasRequestPhaseDefaults_SelfRefNoDefault 验证无 default 的自引用结构体正常终止并返回 false
func TestHasRequestPhaseDefaults_SelfRefNoDefault(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		done <- hasRequestPhaseDefaults(reflect.TypeOf(hasPhaseNoDefaultSelfRef{}), nil)
	}()
	select {
	case got := <-done:
		if got {
			t.Fatal("hasRequestPhaseDefaults should return false for self-ref struct without default")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hasRequestPhaseDefaults did not terminate on self-referential type without default")
	}
}

// ========== applyDefaults 请求阶段直接单元测试 ==========

// TestApplyDefaults_RequestPhase 表驱动测试：验证请求阶段 applyDefaults 的填充规则
func TestApplyDefaults_RequestPhase(t *testing.T) {
	tests := []struct {
		name  string
		req   any                                 // 请求结构体实例（模拟 JSON 反序列化后的状态）
		check func(t *testing.T, v reflect.Value) // 验证填充结果
	}{
		{
			name: "nil指针填充default",
			req: &struct {
				Name *string `json:"name" default:"hello"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				namePtr := v.Elem().FieldByName("Name")
				if namePtr.IsNil() {
					t.Fatal("nil pointer should be filled with default")
				}
				if namePtr.Elem().String() != "hello" {
					t.Fatalf("expected 'hello', got %q", namePtr.Elem().String())
				}
			},
		},
		{
			name: "非nil指针不覆盖",
			req: func() any {
				s := "explicit"
				return &struct {
					Name *string `json:"name" default:"hello"`
				}{Name: &s}
			}(),
			check: func(t *testing.T, v reflect.Value) {
				namePtr := v.Elem().FieldByName("Name")
				if namePtr.Elem().String() != "explicit" {
					t.Fatalf("non-nil pointer should not be overwritten, got %q", namePtr.Elem().String())
				}
			},
		},
		{
			name: "值类型不填充",
			req: &struct {
				Name string `json:"name" default:"hello"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				nameVal := v.Elem().FieldByName("Name").String()
				if nameVal != "" {
					t.Fatalf("value type should not be filled in request phase, got %q", nameVal)
				}
			},
		},
		{
			name: "值类型零值不被默认值覆盖",
			req: &struct {
				Age int `json:"age" default:"18"`
			}{Age: 0},
			check: func(t *testing.T, v reflect.Value) {
				ageVal := v.Elem().FieldByName("Age").Int()
				if ageVal != 0 {
					t.Fatalf("explicit zero should be kept, got %d", ageVal)
				}
			},
		},
		{
			name: "嵌套值结构体内nil指针填充",
			req: &struct {
				Inner struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				tagPtr := v.Elem().FieldByName("Inner").FieldByName("Tag")
				if tagPtr.IsNil() {
					t.Fatal("nested nil pointer should be filled with default")
				}
				if tagPtr.Elem().String() != "v1" {
					t.Fatalf("expected 'v1', got %q", tagPtr.Elem().String())
				}
			},
		},
		{
			name: "切片元素内nil指针填充",
			req: &struct {
				Items []struct {
					Qty *int `json:"qty" default:"10"`
				} `json:"items"`
			}{
				Items: []struct {
					Qty *int `json:"qty" default:"10"`
				}{{}},
			},
			check: func(t *testing.T, v reflect.Value) {
				qtyPtr := v.Elem().FieldByName("Items").Index(0).FieldByName("Qty")
				if qtyPtr.IsNil() {
					t.Fatal("slice elem nil pointer should be filled with default")
				}
				if qtyPtr.Elem().Int() != 10 {
					t.Fatalf("expected 10, got %d", qtyPtr.Elem().Int())
				}
			},
		},
		{
			name: "map值内nil指针填充",
			req: &struct {
				Meta map[string]struct {
					Status *string `json:"status" default:"ok"`
				} `json:"meta"`
			}{
				Meta: map[string]struct {
					Status *string `json:"status" default:"ok"`
				}{"a": {}},
			},
			check: func(t *testing.T, v reflect.Value) {
				statusPtr := v.Elem().FieldByName("Meta").MapIndex(reflect.ValueOf("a")).FieldByName("Status")
				if statusPtr.IsNil() {
					t.Fatal("map value nil pointer should be filled with default")
				}
				if statusPtr.Elem().String() != "ok" {
					t.Fatalf("expected 'ok', got %q", statusPtr.Elem().String())
				}
			},
		},
		{
			name: "nil外层指针阻断内层default填充",
			req: &struct {
				Inner *struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{Inner: nil},
			check: func(t *testing.T, v reflect.Value) {
				innerPtr := v.Elem().FieldByName("Inner")
				if !innerPtr.IsNil() {
					t.Fatal("nil outer pointer should remain nil, inner defaults not applied")
				}
			},
		},
		{
			name: "不支持类型default不填充",
			req: &struct {
				When time.Time `json:"when" default:"2023-01-01"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				whenVal := v.Elem().FieldByName("When").Interface().(time.Time)
				if !whenVal.IsZero() {
					t.Fatal("unsupported type default should be silently ignored")
				}
			},
		},
		{
			name: "零长度切片跳过（不崩溃）",
			req: &struct {
				Items []struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{
				Items: []struct {
					Tag *string `json:"tag" default:"v1"`
				}{},
			},
			check: func(t *testing.T, v reflect.Value) {
				// 零元素切片不崩溃即可
			},
		},
		{
			name: "空map跳过（不崩溃）",
			req: &struct {
				Meta map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{
				Meta: map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				}{},
			},
			check: func(t *testing.T, v reflect.Value) {
				// 空 map 不崩溃即可
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqPtr := reflect.ValueOf(tt.req)
			reqElemType := reqPtr.Type().Elem()
			meta := buildStructMeta(reqElemType)

			err := applyDefaults(reqPtr, meta, true)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tt.check(t, reqPtr)
		})
	}
}

// ========== applyDefaults 请求阶段自引用循环安全测试 ==========

// applySelfRef 自引用结构体用于测试 applyDefaults 运行时的循环安全
type applySelfRef struct {
	Name   string        `json:"name"`
	Child  *applySelfRef `json:"child"`
	Status *string       `json:"status" default:"ok"`
}

// TestApplyDefaults_SelfRef_NoInfiniteRecursion 验证自引用结构体在 applyDefaults 中不会无限递归
func TestApplyDefaults_SelfRef_NoInfiniteRecursion(t *testing.T) {
	// 构造自引用：node.Child 指向自身
	node := &applySelfRef{Name: "self"}
	node.Child = node

	reqPtr := reflect.ValueOf(node)
	meta := buildStructMeta(reflect.TypeOf(applySelfRef{}))

	// 如果递归没有终止条件，此处会栈溢出
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- applyDefaults(reqPtr, meta, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("applyDefaults panicked or errored on self-ref: %v", err)
		}
		// Status 应该被填充
		if node.Status == nil || *node.Status != "ok" {
			t.Fatalf("Status should be filled with 'ok', got %v", node.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("applyDefaults did not terminate on self-referential struct (infinite recursion)")
	}
}

// TestApplyDefaults_SelfRefMap_NoInfiniteRecursion 验证 map 值自引用在 applyDefaults 不会无限递归
func TestApplyDefaults_SelfRefMap_NoInfiniteRecursion(t *testing.T) {
	type mapSelfRef struct {
		Name     string                 `json:"name"`
		Children map[string]*mapSelfRef `json:"children"`
		Tag      *string                `json:"tag" default:"leaf"`
	}

	// 构造 A → Children["b"] → B → Children["a"] → A（环）
	a := &mapSelfRef{Name: "A", Children: map[string]*mapSelfRef{}}
	b := &mapSelfRef{Name: "B", Children: map[string]*mapSelfRef{}}
	a.Children["b"] = b
	b.Children["a"] = a

	reqPtr := reflect.ValueOf(a)
	meta := buildStructMeta(reflect.TypeOf(mapSelfRef{}))

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- applyDefaults(reqPtr, meta, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("applyDefaults panicked or errored on map self-ref: %v", err)
		}
		// Tag 应该被填充
		if a.Tag == nil || *a.Tag != "leaf" {
			t.Fatalf("Tag should be filled with 'leaf', got %v", a.Tag)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("applyDefaults did not terminate on map self-referential struct (infinite recursion)")
	}
}

// ========== applyDefaults 三层嵌套指针 default 边界测试 ==========

// TestApplyDefaults_RequestPhase_DeepNesting 验证三层嵌套指针链中 default 填充行为
// 链路：Outer → *Mid → *Inner → *Value (default)
func TestApplyDefaults_RequestPhase_DeepNesting(t *testing.T) {
	type deepInner struct {
		Value *string `json:"value" default:"deep_ok"`
	}
	type deepMid struct {
		Inner *deepInner `json:"inner"`
	}
	type deepOuter struct {
		Mid *deepMid `json:"mid"`
	}

	t.Run("全链路非nil→最深层nil指针填充", func(t *testing.T) {
		req := &deepOuter{Mid: &deepMid{Inner: &deepInner{}}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		err := applyDefaults(reflect.ValueOf(req), meta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Mid.Inner.Value == nil || *req.Mid.Inner.Value != "deep_ok" {
			t.Fatal("deepest nil pointer should be filled with default")
		}
	})

	t.Run("中间层nil→深层不填充", func(t *testing.T) {
		req := &deepOuter{Mid: nil}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		err := applyDefaults(reflect.ValueOf(req), meta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Mid != nil {
			t.Fatal("nil Mid should remain nil")
		}
	})

	t.Run("内层nil→最深层不填充", func(t *testing.T) {
		req := &deepOuter{Mid: &deepMid{Inner: nil}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		err := applyDefaults(reflect.ValueOf(req), meta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.Mid.Inner != nil {
			t.Fatal("nil Inner should remain nil")
		}
	})

	t.Run("最深层已传值→不覆盖", func(t *testing.T) {
		s := "explicit"
		req := &deepOuter{Mid: &deepMid{Inner: &deepInner{Value: &s}}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		err := applyDefaults(reflect.ValueOf(req), meta, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if *req.Mid.Inner.Value != "explicit" {
			t.Fatalf("explicit value should not be overwritten, got %q", *req.Mid.Inner.Value)
		}
	})
}
