package zchttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ========== 类型与 handler 定义 ==========

// requiredReq 覆盖 required 校验：显式必填、必填+默认值、普通可选字段
type requiredReq struct {
	Name   string `json:"name" nonzero:"true"`
	Level  string `json:"level" nonzero:"true" default:"basic"`
	Remark string `json:"remark"`
}

type requiredRes struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

func requiredHandler(_ context.Context, req requiredReq) (requiredRes, error) {
	return requiredRes{Name: req.Name, Level: req.Level}, nil
}

// validateReq 实现 Validator，做跨字段业务校验：Start 必须早于 End
type validateReq struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func (r validateReq) Validate() error {
	if r.Start >= r.End {
		return &ValidationError{Field: "end", Message: "must be greater than start"}
	}
	return nil
}

type validateRes struct {
	OK bool `json:"ok"`
}

func validateHandler(_ context.Context, _ validateReq) (validateRes, error) {
	return validateRes{OK: true}, nil
}

// plainErrReq 的 Validate 返回普通 error，验证会被兜底包装为 *ValidationError
type plainErrReq struct {
	Name string `json:"name"`
}

var errPlainValidate = errors.New("name is not allowed")

func (r plainErrReq) Validate() error {
	return errPlainValidate
}

func plainErrHandler(_ context.Context, _ plainErrReq) (validateRes, error) {
	return validateRes{OK: true}, nil
}

// orderReq 覆盖嵌套结构体的 required 递归校验：
// Addr 必填且递归进入子字段，Invoice 选填但一旦填写则递归校验子字段

type orderAddress struct {
	City       string `json:"city" nonzero:"true"`
	PostalCode string `json:"postalCode" nonzero:"true"`
	Phone      string `json:"phone"`
}

type orderInvoice struct {
	Header string `json:"header" nonzero:"true"`
	Amount string `json:"amount" nonzero:"true"`
	Email  string `json:"email"`
}

// orderRemark 与 orderInvoice 校验要求一致（非 required 嵌套结构体，填了则校验子字段），
// 但类型为值类型而非指针，用于对比指针与非指针行为差异

type orderRemark struct {
	Title   string `json:"title" nonzero:"true"`
	Content string `json:"content" nonzero:"true"`
	Note    string `json:"note"`
}

type orderReq struct {
	Name    string        `json:"name" nonzero:"true"`
	Addr    orderAddress  `json:"addr" nonzero:"true"`
	Invoice *orderInvoice `json:"invoice"`
	Remark  orderRemark   `json:"remark"`
}

type orderRes struct {
	Name string `json:"name"`
	City string `json:"city"`
}

func orderHandler(_ context.Context, req orderReq) (orderRes, error) {
	return orderRes{Name: req.Name, City: req.Addr.City}, nil
}

// orderItem 作为切片元素，包含 required 子字段
type orderItem struct {
	ProductID string `json:"productId" nonzero:"true"`
	Qty       int    `json:"qty" nonzero:"true"`
	Note      string `json:"note"`
}

type orderSliceReq struct {
	OrderNo string       `json:"orderNo" nonzero:"true"`
	Items   []orderItem  `json:"items" nonzero:"true"`
	Extras  []*orderItem `json:"extras"`
}

type orderSliceRes struct {
	OrderNo string `json:"orderNo"`
	Count   int    `json:"count"`
}

func orderSliceHandler(_ context.Context, req orderSliceReq) (orderSliceRes, error) {
	return orderSliceRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}, nil
}

type orderMapReq struct {
	OrderNo string                `json:"orderNo" nonzero:"true"`
	Items   map[string]orderItem  `json:"items" nonzero:"true"`
	Extras  map[string]*orderItem `json:"extras"`
}

type orderMapRes struct {
	OrderNo string `json:"orderNo"`
	Count   int    `json:"count"`
}

func orderMapHandler(_ context.Context, req orderMapReq) (orderMapRes, error) {
	return orderMapRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}, nil
}

// ---- 自引用结构体 ----
type treeNode struct {
	Name   string    `json:"name" nonzero:"true"`
	Parent *treeNode `json:"parent"`
}
type treeReq struct {
	Root treeNode `json:"root" nonzero:"true"`
}
type treeRes struct {
	OK bool `json:"ok"`
}

func treeHandler(_ context.Context, _ treeReq) (treeRes, error) {
	return treeRes{OK: true}, nil
}

// ---- 嵌套路径校验 ----
type nestedPathCompany struct {
	Code string `json:"code" nonzero:"true"`
	Name string `json:"name" nonzero:"true"`
}
type nestedPathReq struct {
	Tag     string            `json:"tag"`
	Company nestedPathCompany `json:"company" nonzero:"true"`
}
type nestedPathRes struct {
	OK bool `json:"ok"`
}

func nestedPathHandler(_ context.Context, _ nestedPathReq) (nestedPathRes, error) {
	return nestedPathRes{OK: true}, nil
}

// ---- 首字段嵌套 struct（修复 visited map 地址冲突） ----
type FirstFieldBase struct {
	ID   int    `json:"id" nonzero:"true"`
	Name string `json:"name" nonzero:"true"`
}
type firstFieldReq struct {
	FirstFieldBase        // 匿名嵌入作为第一个字段，地址与父结构体相同
	Email          string `json:"email" nonzero:"true"`
}
type firstFieldRes struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func firstFieldHandler(_ context.Context, req firstFieldReq) (firstFieldRes, error) {
	return firstFieldRes{ID: req.ID, Name: req.Name, Email: req.Email}, nil
}

// ---- nonzero + default 共存 ----
type nonzeroDefaultReq struct {
	Level string `json:"level" nonzero:"true" default:"basic"`
}
type nonzeroDefaultRes struct {
	Level string `json:"level"`
}

func nonzeroDefaultHandler(_ context.Context, req nonzeroDefaultReq) (nonzeroDefaultRes, error) {
	return nonzeroDefaultRes{Level: req.Level}, nil
}

// ---- handler 返回普通 error ----
type plainErrRouteReq struct {
	Name string `json:"name"`
}
type plainErrRouteRes struct {
	OK bool `json:"ok"`
}

func plainErrRouteHandler(_ context.Context, _ plainErrRouteReq) (plainErrRouteRes, error) {
	return plainErrRouteRes{}, errors.New("service unavailable")
}

// ========== 基础 nonzero 校验 ==========

// TestValidateRequiredPass 验证 required 字段传入非零值时校验通过
func TestValidateRequiredPass(t *testing.T) {
	router := NewRouter()
	router.GET("/req", requiredHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/req?name=alice", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res requiredRes
	decodeData(t, rec, &res)
	if res.Name != "alice" {
		t.Fatalf("unexpected name: %s", res.Name)
	}
	// Level 带 default，未传时应回退为 basic
	if res.Level != "basic" {
		t.Fatalf("level should fall back to default basic, got %s", res.Level)
	}
}

// TestValidateRequiredMissing 验证 required 字段缺失（零值）时校验失败
func TestValidateRequiredMissing(t *testing.T) {
	router := NewRouter()
	router.GET("/req", requiredHandler)

	engine := NewEngine()
	engine.Router = router

	// 不传 name，name 为零值，required 校验应失败
	req := httptest.NewRequest(http.MethodGet, "/req", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required field, got %d", rec.Code)
	}
}

// TestValidateRequiredEmptyString 验证 required 字段传空字符串（零值）时校验失败
func TestValidateRequiredEmptyString(t *testing.T) {
	router := NewRouter()
	router.GET("/req", requiredHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/req?name=", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty required field, got %d", rec.Code)
	}
}

// ========== 自定义 Validator 接口 ==========

// TestValidateCustomPass 验证 Validate 通过时正常返回 200
func TestValidateCustomPass(t *testing.T) {
	router := NewRouter()
	router.GET("/v", validateHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/v?start=1&end=5", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// TestValidateCustomStructuredError 验证 Validate 返回 *ValidationError 时进入 400 回调
func TestValidateCustomStructuredError(t *testing.T) {
	router := NewRouter()
	router.GET("/v", validateHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/v?start=5&end=1", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for structured validation error, got %d", rec.Code)
	}
}

// TestValidateCustomPlainError 验证 Validate 返回普通 error 时被兜底包装并进入 400 回调
func TestValidateCustomPlainError(t *testing.T) {
	router := NewRouter()
	router.GET("/v", plainErrHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/v?name=bob", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for plain validation error, got %d", rec.Code)
	}
}

// ========== 嵌套结构体递归校验 ==========

// TestValidateRequiredNestedAllPass 全部字段正确填写，校验通过
func TestValidateRequiredNestedAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"invoice":{"header":"XX公司","amount":"100.00"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredNestedTopLevelMissing 顶层 required 字段缺失，校验失败
func TestValidateRequiredNestedTopLevelMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Addr 为 required，不传应报错
	body := `{"name":"张三"}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required addr, got %d", rec.Code)
	}
}

// TestValidateRequiredNestedChildMissing 嵌套 required 字段缺失，校验失败
func TestValidateRequiredNestedChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Addr 已填但 City 为空，应递归校验到子字段 City required
	body := `{"name":"张三","addr":{"postalCode":"100000"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing nested required city, got %d", rec.Code)
	}
}

// TestValidateRequiredNestedOptionalSkipped 选填嵌套字段为零值（nil），跳过不报错
func TestValidateRequiredNestedOptionalSkipped(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Invoice 为 nil/不传，应跳过不校验
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when invoice not provided, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredNestedOptionalChildMissing 选填嵌套字段已填但子字段缺失，校验失败
func TestValidateRequiredNestedOptionalChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Invoice 非 nil 但 Header 为空，应递归校验到 Header required
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"invoice":{"amount":"100.00"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing invoice header, got %d", rec.Code)
	}
}

// TestValidateRequiredNestedOptionalEmptyObject 选填字段传空对象 {}，指针非 nil 但子字段全零，触发子字段 required 校验
func TestValidateRequiredNestedOptionalEmptyObject(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Invoice 为 {} → 指针非 nil，但 Header/Amount 为零值，应报错
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"invoice":{}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty invoice object (nil ptr but required children missing), got %d", rec.Code)
	}
}

// TestValidateRequiredNestedValueTypeSkipped 值类型选填字段为零值时跳过，不报错
func TestValidateRequiredNestedValueTypeSkipped(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Remark 为值类型且非 required，JSON 未传时为零值 orderRemark{}，IsZero=true → 跳过
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when remark (value type) not provided, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredNestedValueTypeAllPass 值类型选填字段全部填写，校验通过
func TestValidateRequiredNestedValueTypeAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"remark":{"title":"加急","content":"请尽快发货"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredNestedValueTypeChildMissing 值类型选填字段已填但子字段缺失，校验失败
func TestValidateRequiredNestedValueTypeChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Remark 非零值但 Title 为空，应递归校验并报错
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"remark":{"content":"请尽快发货"}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing remark title, got %d", rec.Code)
	}
}

// TestValidateRequiredNestedValueTypeEmptyObject 值类型选填字段传空对象 {}，IsZero=true 跳过，与指针不同
func TestValidateRequiredNestedValueTypeEmptyObject(t *testing.T) {
	router := NewRouter()
	router.POST("/order", orderHandler)

	engine := NewEngine()
	engine.Router = router

	// Remark 为值类型，JSON 传 {} → IsZero=true（全字段零值）→ 跳过不校验
	// 这与指针类型不同：*Invoice 传 {} 时指针非 nil，IsZero=false，会递归报错
	body := `{"name":"张三","addr":{"city":"北京","postalCode":"100000"},"remark":{}}`
	req := httptest.NewRequest(http.MethodPost, "/order", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty remark (value type, IsZero=true → skip), got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ========== 切片元素递归校验 ==========

// TestValidateRequiredSliceAllPass 结构体切片每个元素 required 字段均满足，校验通过
func TestValidateRequiredSliceAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2},{"productId":"P2","qty":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredSliceChildMissing 结构体切片中某个元素缺少 required 字段，校验失败
func TestValidateRequiredSliceChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// items[1] 缺少 productId
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2},{"qty":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing productId in slice element, got %d", rec.Code)
	}
}

// TestValidateRequiredSliceNilRequired 必填切片为 nil，校验失败
func TestValidateRequiredSliceNilRequired(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// items 为 required 但未传
	body := `{"orderNo":"ORD-001"}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required slice, got %d", rec.Code)
	}
}

// TestValidateRequiredSlicePtrAllPass 结构体指针切片每个元素 required 字段均满足，校验通过
func TestValidateRequiredSlicePtrAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}],"extras":[{"productId":"E1","qty":1},{"productId":"E2","qty":3}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredSlicePtrChildMissing 结构体指针切片中某个元素缺少 required 字段，校验失败
func TestValidateRequiredSlicePtrChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// extras[1] 缺少 qty
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}],"extras":[{"productId":"E1","qty":1},{"productId":"E2"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing qty in slice ptr element, got %d", rec.Code)
	}
}

// TestValidateRequiredSlicePtrNilElement 结构体指针切片中包含 nil 元素，跳过 nil 不报错，校验其余元素
func TestValidateRequiredSlicePtrNilElement(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 包含一个 null → 跳过；另一个正常 → 校验通过
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}],"extras":[null,{"productId":"E1","qty":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when null element skipped, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredSliceOptionalSkipped 非 required 切片为 nil，跳过不报错
func TestValidateRequiredSliceOptionalSkipped(t *testing.T) {
	router := NewRouter()
	router.POST("/orderSlice", orderSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 非 required，不传应跳过
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when optional slice not provided, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ========== map value 递归校验 ==========

// TestValidateRequiredMapAllPass map[string]struct 每个 value 的 required 字段均满足，校验通过
func TestValidateRequiredMapAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":{"a":{"productId":"P1","qty":2},"b":{"productId":"P2","qty":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredMapValueMissing map 中某个 value 缺少 required 字段，校验失败
func TestValidateRequiredMapValueMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// items["b"] 缺少 productId
	body := `{"orderNo":"ORD-001","items":{"a":{"productId":"P1","qty":2},"b":{"qty":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing productId in map value, got %d", rec.Code)
	}
}

// TestValidateRequiredMapNilRequired 必填 map 为 nil，校验失败
func TestValidateRequiredMapNilRequired(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// items 为 required 但未传
	body := `{"orderNo":"ORD-001"}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing required map, got %d", rec.Code)
	}
}

// TestValidateRequiredMapPtrAllPass map[string]*struct 每个非 nil value 的 required 字段均满足，校验通过
func TestValidateRequiredMapPtrAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":{"x":{"productId":"P1","qty":2}},"extras":{"e1":{"productId":"E1","qty":1},"e2":{"productId":"E2","qty":3}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredMapPtrValueMissing map[string]*struct 中某个 value 缺少 required 字段，校验失败
func TestValidateRequiredMapPtrValueMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// extras["e2"] 缺少 qty
	body := `{"orderNo":"ORD-001","items":{"x":{"productId":"P1","qty":2}},"extras":{"e1":{"productId":"E1","qty":1},"e2":{"productId":"E2"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing qty in map ptr value, got %d", rec.Code)
	}
}

// TestValidateRequiredMapPtrNilValue map[string]*struct 中包含 nil value，跳过 nil 不报错，校验其余
func TestValidateRequiredMapPtrNilValue(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 包含一个 null → 跳过；另一个正常 → 校验通过
	body := `{"orderNo":"ORD-001","items":{"x":{"productId":"P1","qty":2}},"extras":{"e1":null,"e2":{"productId":"E2","qty":1}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when null map value skipped, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredMapOptionalSkipped 非 required map 为 nil，跳过不报错
func TestValidateRequiredMapOptionalSkipped(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 非 required，不传应跳过
	body := `{"orderNo":"ORD-001","items":{"x":{"productId":"P1","qty":2}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when optional map not provided, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidateRequiredMapOnlyRequiredFieldsChecked map value 中仅 required 子字段被校验，
// 无 required 标签的 Note 缺失不影响校验通过。验证 nonzero:"true" 只对被标记字段本身起作用。
func TestValidateRequiredMapOnlyRequiredFieldsChecked(t *testing.T) {
	router := NewRouter()
	router.POST("/orderMap", orderMapHandler)

	engine := NewEngine()
	engine.Router = router

	// Note 无 required 标签，缺失不应报错
	body := `{"orderNo":"ORD-001","items":{"a":{"productId":"P1","qty":2}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when non-required Note is missing, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// ========== 指针包裹容器 nonzero 递归校验 ==========

// ptrContainerValidateReq 包含 *[]Struct 和 *map[K]Struct 字段，用于验证指针不阻断 nonzero 校验
type ptrContainerValidateReq struct {
	OrderNo string                 `json:"orderNo" nonzero:"true"`
	Items   *[]orderItem           `json:"items" nonzero:"true"`
	Extras  *map[string]*orderItem `json:"extras"`
}

type ptrContainerValidateRes struct {
	OrderNo string `json:"orderNo"`
	Count   int    `json:"count"`
}

func ptrContainerValidateHandler(_ context.Context, req ptrContainerValidateReq) (ptrContainerValidateRes, error) {
	res := ptrContainerValidateRes{OrderNo: req.OrderNo}
	if req.Items != nil {
		res.Count += len(*req.Items)
	}
	if req.Extras != nil {
		res.Count += len(*req.Extras)
	}
	return res, nil
}

// TestValidatePtrSliceElemAllPass *[]Struct 每个元素 nonzero 字段均满足，校验通过
func TestValidatePtrSliceElemAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrValidate", ptrContainerValidateHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2},{"productId":"P2","qty":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/ptrValidate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidatePtrSliceElemChildMissing *[]Struct 中某个元素缺少 nonzero 字段，校验失败
func TestValidatePtrSliceElemChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrValidate", ptrContainerValidateHandler)

	engine := NewEngine()
	engine.Router = router

	// items[1] 缺少 productId
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2},{"qty":1}]}`
	req := httptest.NewRequest(http.MethodPost, "/ptrValidate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing productId in *[]Struct element, got %d", rec.Code)
	}
}

// TestValidatePtrMapValueAllPass *map[K]Struct 每个 value 的 nonzero 字段均满足，校验通过
func TestValidatePtrMapValueAllPass(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrValidate", ptrContainerValidateHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}],"extras":{"e1":{"productId":"E1","qty":1},"e2":{"productId":"E2","qty":3}}}`
	req := httptest.NewRequest(http.MethodPost, "/ptrValidate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidatePtrMapValueChildMissing *map[K]Struct 中某个 value 缺少 nonzero 字段，校验失败
func TestValidatePtrMapValueChildMissing(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrValidate", ptrContainerValidateHandler)

	engine := NewEngine()
	engine.Router = router

	// extras["e2"] 缺少 qty
	body := `{"orderNo":"ORD-001","items":[{"productId":"P1","qty":2}],"extras":{"e1":{"productId":"E1","qty":1},"e2":{"productId":"E2"}}}`
	req := httptest.NewRequest(http.MethodPost, "/ptrValidate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing qty in *map[K]Struct value, got %d", rec.Code)
	}
}

// ========== P0/P1 校验相关测试 ==========

// TestHandlerErrorReturns500 验证 handler 返回普通 error → OnError（500）
func TestHandlerErrorReturns500(t *testing.T) {
	router := NewRouter()
	router.GET("/fail", plainErrRouteHandler)
	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/fail", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for handler error, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestSelfRefStructNoStackOverflow 验证自引用结构体不会因循环递归导致栈溢出
func TestSelfRefStructNoStackOverflow(t *testing.T) {
	router := NewRouter()
	router.POST("/tree", treeHandler)
	engine := NewEngine()
	engine.Router = router

	// child.Parent 指向自身（构造循环引用），visited map 应阻止无限递归
	body := `{"root":{"name":"root","parent":{"name":"self"}}}`
	req := httptest.NewRequest(http.MethodPost, "/tree", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// 若 visited 机制缺失，此处会栈溢出导致测试进程崩溃
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestValidationNestedPathMessage 验证嵌套字段校验错误包含绑定名路径（如 "company.name"）
func TestValidationNestedPathMessage(t *testing.T) {
	router := NewRouter()
	router.POST("/biz", nestedPathHandler)
	engine := NewEngine()
	engine.Router = router

	// Code 非空使 company 整体非零，但 Name 为空 → 递归校验发现 company.name 缺失
	body := `{"company":{"code":"C001","name":""}}`
	httpReq := httptest.NewRequest(http.MethodPost, "/biz", strings.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body: %s", rec.Code, rec.Body.String())
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, "company.name") {
		t.Fatalf("error message should contain nested path 'company.name', got: %s", bodyStr)
	}
}

// TestNonzeroWithDefaultStillValidates 验证 nonzero+default 共存时，显式传零值仍触发校验失败
func TestNonzeroWithDefaultStillValidates(t *testing.T) {
	router := NewRouter()
	router.POST("/nd", nonzeroDefaultHandler)
	engine := NewEngine()
	engine.Router = router

	// 不传 level → default 填充 "basic" → nonzero 通过
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/nd", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("not passing level should use default and pass, got %d, body: %s", rec.Code, rec.Body.String())
	}

	// 显式传空字符串 → 覆盖默认值 → nonzero 校验失败
	body2 := `{"level":""}`
	req2 := httptest.NewRequest(http.MethodPost, "/nd", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	engine.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("explicit empty with nonzero+default should fail, got %d, body: %s", rec2.Code, rec2.Body.String())
	}
}

// TestValidateFirstFieldNestedStruct 验证匿名嵌入作为首字段时，
// 其内部 nonzero 字段不会被 visited map 误判跳过（修复地址冲突 bug）
func TestValidateFirstFieldNestedStruct(t *testing.T) {
	router := NewRouter()
	router.POST("/first", firstFieldHandler)
	engine := NewEngine()
	engine.Router = router

	// ID 缺失 → 应报 400（修复前会被跳过）
	body := `{"name":"alice","email":"a@b.com"}`
	req := httptest.NewRequest(http.MethodPost, "/first", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("first-field embedded struct ID should be validated, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "id") {
		t.Fatalf("error should mention 'id', got: %s", rec.Body.String())
	}

	// Name 缺失 → 应报 400
	body2 := `{"id":1,"email":"a@b.com"}`
	req2 := httptest.NewRequest(http.MethodPost, "/first", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	engine.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("first-field embedded struct Name should be validated, got %d, body: %s", rec2.Code, rec2.Body.String())
	}

	// 全部字段正常 → 200
	body3 := `{"id":1,"name":"alice","email":"a@b.com"}`
	req3 := httptest.NewRequest(http.MethodPost, "/first", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	engine.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Fatalf("all fields provided should pass, got %d, body: %s", rec3.Code, rec3.Body.String())
	}
}

// ========== BUG 验证测试 ==========

// --- BUG1: validateNonzeroWalk 对 map 值的循环引用检测无效 ---

// cyclicNode 自引用结构体，用于构造 map 循环引用
type cyclicNode struct {
	Name     string                 `json:"name" nonzero:"true"`
	Children map[string]*cyclicNode `json:"children"`
}

// TestValidateNonzeroWalk_MapCycle 验证 map 值存在循环引用时不会无限递归
func TestValidateNonzeroWalk_MapCycle(t *testing.T) {
	// 构造循环：nodeA.Children["b"] -> nodeB, nodeB.Children["a"] -> nodeA
	nodeA := &cyclicNode{Name: "A", Children: map[string]*cyclicNode{}}
	nodeB := &cyclicNode{Name: "B", Children: map[string]*cyclicNode{}}
	nodeA.Children["b"] = nodeB
	nodeB.Children["a"] = nodeA

	// 将循环结构放入一个包装结构体中进行校验
	type wrapReq struct {
		Root *cyclicNode `json:"root" nonzero:"true"`
	}

	meta := buildStructMeta(reflect.TypeOf(wrapReq{}))
	reqVal := reflect.ValueOf(&wrapReq{Root: nodeA})

	// 如果循环检测无效，此处会无限递归导致栈溢出
	// 设置 timeout 通过 goroutine + recover 捕获
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		err := validateNonzero(reqVal, meta)
		done <- err
	}()

	select {
	case err := <-done:
		// 正常返回（无论是 nil 还是校验错误）表示没有无限递归
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("BUG1 confirmed: validateNonzeroWalk infinite recursion on map cycle (timed out)")
	}
}

// cyclicValNode 非指针值类型的自引用结构体：map 值为 struct 值，
// 但 struct 内部的 map 字段是引用类型，复制后仍指向同一底层桶，可形成环
type cyclicValNode struct {
	Name     string                   `json:"name" nonzero:"true"`
	Children map[string]cyclicValNode `json:"children"`
}

// TestValidateNonzeroWalk_MapValueCycle 验证非指针值类型的 map 循环引用不会无限递归
func TestValidateNonzeroWalk_MapValueCycle(t *testing.T) {
	// 构造环：a.Children 与 b.Children 互相引用对方的 map 底层桶
	a := cyclicValNode{Name: "A", Children: map[string]cyclicValNode{}}
	b := cyclicValNode{Name: "B", Children: map[string]cyclicValNode{}}
	a.Children["b"] = b // 副本，但副本的 Children 指向 b.Children 底层桶
	b.Children["a"] = a // 环形成：a.Children -> b.Children -> a.Children

	type wrapValReq struct {
		Root cyclicValNode `json:"root"`
	}

	meta := buildStructMeta(reflect.TypeOf(wrapValReq{}))
	reqVal := reflect.ValueOf(&wrapValReq{Root: a})

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		err := validateNonzero(reqVal, meta)
		done <- err
	}()

	select {
	case err := <-done:
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed: validateNonzeroWalk infinite recursion on non-pointer map value cycle (timed out)")
	}
}

// TestValidateNonzeroWalk_MapMultipleValues 验证 map 中多个值类型 struct 均被逐一校验：
// map 值需复制为临时副本后递归，若副本地址被误计入 visited（临时地址可能被复用），
// 后续值可能被误判"已访问"而漏校验，此测试确保每个值的 nonzero 违规都能被发现
func TestValidateNonzeroWalk_MapMultipleValues(t *testing.T) {
	type wrapValReq struct {
		Root cyclicValNode `json:"root"`
	}

	// 构造多个 map 值，仅最后一个违反 nonzero 约束
	root := cyclicValNode{Name: "root", Children: map[string]cyclicValNode{}}
	for i := 0; i < 8; i++ {
		root.Children[fmt.Sprintf("ok%d", i)] = cyclicValNode{Name: fmt.Sprintf("n%d", i)}
	}
	root.Children["zzz_bad"] = cyclicValNode{Name: ""} // 违规值

	meta := buildStructMeta(reflect.TypeOf(wrapValReq{}))
	reqVal := reflect.ValueOf(&wrapValReq{Root: root})

	err := validateNonzero(reqVal, meta)
	if err == nil {
		t.Fatal("expected nonzero violation in map value to be detected, got nil")
	}
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	if ve.Field != "root.children.name" {
		t.Errorf("Field = %q, want %q", ve.Field, "root.children.name")
	}
}

// --- BUG2: isAllDigits("-") 返回 true ---

// TestIsAllDigits_SingleMinus 验证单个负号不应被认为是纯数字
func TestIsAllDigits_SingleMinus(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"-", false},
		{"-1", true},
		{"123", true},
		{"", false},
		{"abc", false},
		{"12-3", false},
		{"-0", true},
	}
	for _, c := range cases {
		got := isAllDigits(c.input)
		if got != c.want {
			t.Errorf("isAllDigits(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}
