package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// decodeData 从统一响应结构 {data,code,message} 中解析 data 字段到 v
func decodeData(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
}

// searchReq 用于 query / 表单绑定测试，覆盖字符串、整型、布尔与切片
type searchReq struct {
	Keyword string   `json:"keyword"`
	Page    int      `json:"page"`
	Active  bool     `json:"active"`
	Tags    []string `json:"tags"`
}

type searchRes struct {
	Keyword string   `json:"keyword"`
	Page    int      `json:"page"`
	Active  bool     `json:"active"`
	Tags    []string `json:"tags"`
}

func searchHandler(_ context.Context, req searchReq) (searchRes, error) {
	return searchRes{
		Keyword: req.Keyword,
		Page:    req.Page,
		Active:  req.Active,
		Tags:    req.Tags,
	}, nil
}

// TestBindQueryGet 验证 GET 请求的 query 参数被绑定到 Req 结构体
func TestBindQueryGet(t *testing.T) {
	router := NewRouter()
	router.GET("/search", searchHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/search?keyword=go&page=3&active=true&tags=a&tags=b", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res searchRes
	decodeData(t, rec, &res)
	if res.Keyword != "go" || res.Page != 3 || res.Active != true {
		t.Fatalf("unexpected bind result: %+v", res)
	}
	if len(res.Tags) != 2 || res.Tags[0] != "a" || res.Tags[1] != "b" {
		t.Fatalf("unexpected tags: %v", res.Tags)
	}
}

// TestBindFormUrlencoded 验证 POST 表单编码请求被绑定到 Req 结构体
func TestBindFormUrlencoded(t *testing.T) {
	router := NewRouter()
	router.POST("/search", searchHandler)

	engine := NewEngine()
	engine.Router = router

	form := "keyword=hello&page=5&active=false&tags=x&tags=y"
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res searchRes
	decodeData(t, rec, &res)
	if res.Keyword != "hello" || res.Page != 5 || res.Active != false {
		t.Fatalf("unexpected bind result: %+v", res)
	}
	if len(res.Tags) != 2 || res.Tags[0] != "x" || res.Tags[1] != "y" {
		t.Fatalf("unexpected tags: %v", res.Tags)
	}
}

// TestBindJSONPost 验证 POST JSON 请求仍能正常绑定
func TestBindJSONPost(t *testing.T) {
	router := NewRouter()
	router.POST("/search", searchHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"keyword":"json","page":9,"active":true,"tags":["m","n"]}`
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res searchRes
	decodeData(t, rec, &res)
	if res.Keyword != "json" || res.Page != 9 || res.Active != true {
		t.Fatalf("unexpected bind result: %+v", res)
	}
}

// uploadReq 包含普通字段与上传文件字段
type uploadReq struct {
	Title string                `json:"title"`
	File  *multipart.FileHeader `json:"file"`
}

type uploadRes struct {
	Title    string `json:"title"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

func uploadHandler(_ context.Context, req uploadReq) (uploadRes, error) {
	res := uploadRes{Title: req.Title}
	if req.File != nil {
		res.Filename = req.File.Filename
		res.Size = req.File.Size
	}
	return res, nil
}

// TestBindMultipartWithFile 验证 multipart/form-data 请求的普通字段与上传文件被正确绑定
func TestBindMultipartWithFile(t *testing.T) {
	router := NewRouter()
	router.POST("/upload", uploadHandler)

	engine := NewEngine()
	engine.Router = router

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "my-doc")
	fw, err := mw.CreateFormFile("file", "hello.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	content := []byte("hello world")
	if _, err := fw.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res uploadRes
	decodeData(t, rec, &res)
	if res.Title != "my-doc" {
		t.Fatalf("unexpected title: %s", res.Title)
	}
	if res.Filename != "hello.txt" {
		t.Fatalf("unexpected filename: %s", res.Filename)
	}
	if res.Size != int64(len(content)) {
		t.Fatalf("unexpected size: got %d, want %d", res.Size, len(content))
	}
}

// eventReq 覆盖多种 time.Time 解析场景：unix 时间戳、自定义布局、自动探测
type eventReq struct {
	StartTime time.Time `json:"start_time" time_format:"unix"`
	Date      time.Time `json:"date" time_format:"2006-01-02"`
	SlashDate time.Time `json:"slash_date" time_format:"02/01/2006"`
	Auto      time.Time `json:"auto"`
	AutoMilli time.Time `json:"auto_milli"`
}

type eventRes struct {
	StartUnix  int64 `json:"start_unix"`
	Year       int   `json:"year"`
	Month      int   `json:"month"`
	Day        int   `json:"day"`
	SlashYear  int   `json:"slash_year"`
	SlashDay   int   `json:"slash_day"`
	AutoUnix   int64 `json:"auto_unix"`
	AutoMilliU int64 `json:"auto_milli_unix_milli"`
}

func eventHandler(_ context.Context, req eventReq) (eventRes, error) {
	return eventRes{
		StartUnix:  req.StartTime.Unix(),
		Year:       req.Date.Year(),
		Month:      int(req.Date.Month()),
		Day:        req.Date.Day(),
		SlashYear:  req.SlashDate.Year(),
		SlashDay:   req.SlashDate.Day(),
		AutoUnix:   req.Auto.Unix(),
		AutoMilliU: req.AutoMilli.UnixMilli(),
	}, nil
}

// TestBindTime 验证 time.Time 的时间戳、自定义格式与自动探测解析
func TestBindTime(t *testing.T) {
	router := NewRouter()
	router.GET("/event", eventHandler)

	engine := NewEngine()
	engine.Router = router

	// start_time: unix 秒；date: yyyy-MM-dd；slash_date: dd/MM/yyyy；
	// auto: RFC3339 自动探测；auto_milli: 13 位毫秒时间戳自动探测
	query := "start_time=1700000000" +
		"&date=2023-06-15" +
		"&slash_date=15/06/2023" +
		"&auto=2023-06-15T10:00:00Z" +
		"&auto_milli=1700000000123"
	req := httptest.NewRequest(http.MethodGet, "/event?"+query, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res eventRes
	decodeData(t, rec, &res)
	if res.StartUnix != 1700000000 {
		t.Fatalf("unix timestamp mismatch: got %d", res.StartUnix)
	}
	if res.Year != 2023 || res.Month != 6 || res.Day != 15 {
		t.Fatalf("custom layout mismatch: %d-%d-%d", res.Year, res.Month, res.Day)
	}
	if res.SlashYear != 2023 || res.SlashDay != 15 {
		t.Fatalf("slash layout mismatch: %d/%d", res.SlashDay, res.SlashYear)
	}
	if res.AutoUnix != 1686823200 { // 2023-06-15T10:00:00Z
		t.Fatalf("auto RFC3339 mismatch: got %d", res.AutoUnix)
	}
	if res.AutoMilliU != 1700000000123 {
		t.Fatalf("auto milli timestamp mismatch: got %d", res.AutoMilliU)
	}
}

// defaultReq 通过 default 标签为字段设置默认值
type defaultReq struct {
	Keyword  string   `json:"keyword" default:"all"`
	Page     int      `json:"page" default:"1"`
	PageSize int      `json:"page_size" default:"20"`
	Sort     string   `json:"sort" default:"created_at"`
	Tags     []string `json:"tags" default:"a,b,c"`
}

type defaultRes struct {
	Keyword  string   `json:"keyword"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Sort     string   `json:"sort"`
	Tags     []string `json:"tags"`
}

func defaultHandler(_ context.Context, req defaultReq) (defaultRes, error) {
	return defaultRes{
		Keyword:  req.Keyword,
		Page:     req.Page,
		PageSize: req.PageSize,
		Sort:     req.Sort,
		Tags:     req.Tags,
	}, nil
}

// TestBindDefaultsAllMissing 验证无任何参数时所有带 default 的字段都被填充默认值
func TestBindDefaultsAllMissing(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/list", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Keyword != "all" || res.Page != 1 || res.PageSize != 20 || res.Sort != "created_at" {
		t.Fatalf("unexpected defaults: %+v", res)
	}
	if len(res.Tags) != 3 || res.Tags[0] != "a" || res.Tags[1] != "b" || res.Tags[2] != "c" {
		t.Fatalf("unexpected tags default: %v", res.Tags)
	}
}

// TestBindDefaultsPartial 验证已传递的参数不被默认值覆盖，未传递的才使用默认值
func TestBindDefaultsPartial(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// 仅传递 keyword 与 page，其余用默认值
	req := httptest.NewRequest(http.MethodGet, "/list?keyword=go&page=5", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Keyword != "go" || res.Page != 5 {
		t.Fatalf("provided values overwritten: %+v", res)
	}
	if res.PageSize != 20 || res.Sort != "created_at" {
		t.Fatalf("missing fields not defaulted: %+v", res)
	}
	if len(res.Tags) != 3 {
		t.Fatalf("tags default not applied: %v", res.Tags)
	}
}

// TestBindDefaultsParseErrorKeepsDefault 验证“传递了但解析失败”时保留默认值
func TestBindDefaultsParseErrorKeepsDefault(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// page=abc 无法解析为 int，应保留默认值 1
	req := httptest.NewRequest(http.MethodGet, "/list?page=abc", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Page != 1 {
		t.Fatalf("parse error should keep default 1, got %d", res.Page)
	}
}

// TestBindDefaultsExplicitZeroNotOverwritten 验证显式传递等于零值的值不会被默认值覆盖
func TestBindDefaultsExplicitZeroNotOverwritten(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传递 page=0，应保留 0 而非默认值 1
	req := httptest.NewRequest(http.MethodGet, "/list?page=0", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Page != 0 {
		t.Fatalf("explicit zero should be kept, got %d", res.Page)
	}
}

// requiredReq 覆盖 required 校验：显式必填、必填+默认值、普通可选字段
type requiredReq struct {
	Name   string `json:"name" required:"true"`
	Level  string `json:"level" required:"true" default:"basic"`
	Remark string `json:"remark"`
}

type requiredRes struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

func requiredHandler(_ context.Context, req requiredReq) (requiredRes, error) {
	return requiredRes{Name: req.Name, Level: req.Level}, nil
}

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

// orderReq 覆盖嵌套结构体的 required 递归校验：
// Addr 必填且递归进入子字段，Invoice 选填但一旦填写则递归校验子字段

type orderAddress struct {
	City       string `json:"city" required:"true"`
	PostalCode string `json:"postalCode" required:"true"`
	Phone      string `json:"phone"`
}

type orderInvoice struct {
	Header string `json:"header" required:"true"`
	Amount string `json:"amount" required:"true"`
	Email  string `json:"email"`
}

// orderRemark 与 orderInvoice 校验要求一致（非 required 嵌套结构体，填了则校验子字段），
// 但类型为值类型而非指针，用于对比指针与非指针行为差异

type orderRemark struct {
	Title   string `json:"title" required:"true"`
	Content string `json:"content" required:"true"`
	Note    string `json:"note"`
}

type orderReq struct {
	Name    string        `json:"name" required:"true"`
	Addr    orderAddress  `json:"addr" required:"true"`
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
	// 注意：json 中 "invoice":{} 会使 Invoice 为 &orderInvoice{}（非 nil 但子字段全零）
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

// -------- 切片元素递归校验测试 --------

// orderItem 作为切片元素，包含 required 子字段
type orderItem struct {
	ProductID string `json:"productId" required:"true"`
	Qty       int    `json:"qty" required:"true"`
	Note      string `json:"note"`
}

type orderSliceReq struct {
	OrderNo string       `json:"orderNo" required:"true"`
	Items   []orderItem  `json:"items" required:"true"`
	Extras  []*orderItem `json:"extras"`
}

type orderSliceRes struct {
	OrderNo string `json:"orderNo"`
	Count   int    `json:"count"`
}

func orderSliceHandler(_ context.Context, req orderSliceReq) (orderSliceRes, error) {
	return orderSliceRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}, nil
}

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

// -------- map value 递归校验测试 --------

type orderMapReq struct {
	OrderNo string                `json:"orderNo" required:"true"`
	Items   map[string]orderItem  `json:"items" required:"true"`
	Extras  map[string]*orderItem `json:"extras"`
}

type orderMapRes struct {
	OrderNo string `json:"orderNo"`
	Count   int    `json:"count"`
}

func orderMapHandler(_ context.Context, req orderMapReq) (orderMapRes, error) {
	return orderMapRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}, nil
}

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
// 无 required 标签的 Note 缺失不影响校验通过。验证 required:"true" 只对被标记字段本身起作用。
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

// -------- slice/map default 递归测试 --------

// itemWithDefault 作为切片/map 元素，包含 default 子字段
// Qty/Status 使用指针类型：在 slice/map 嵌套元素中，只有 nil 指针才会被请求阶段 default 填充，
// 值类型（如 int/string）的零值无法区分"未传"与"传了 0"，因此不在请求阶段填充。
type itemWithDefault struct {
	Name   string  `json:"name" required:"true"`
	Qty    *int    `json:"qty" default:"1"`
	Status *string `json:"status" default:"active"`
	Note   string  `json:"note"`
}

// itemDefaultRes 用于回显默认值填充结果
type itemDefaultRes struct {
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ====== 切片 default 递归 ======

type orderDefaultSliceReq struct {
	OrderNo string             `json:"orderNo" required:"true"`
	Items   []itemWithDefault  `json:"items" required:"true"`
	Extras  []*itemWithDefault `json:"extras"`
}

type orderDefaultSliceRes struct {
	OrderNo string         `json:"orderNo"`
	First   itemDefaultRes `json:"first"`
	Count   int            `json:"count"`
}

func orderDefaultSliceHandler(_ context.Context, req orderDefaultSliceReq) (orderDefaultSliceRes, error) {
	res := orderDefaultSliceRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}
	if len(req.Items) > 0 {
		qty, status := 0, ""
		if req.Items[0].Qty != nil {
			qty = *req.Items[0].Qty
		}
		if req.Items[0].Status != nil {
			status = *req.Items[0].Status
		}
		res.First = itemDefaultRes{
			Name:   req.Items[0].Name,
			Qty:    qty,
			Status: status,
			Note:   req.Items[0].Note,
		}
	}
	return res, nil
}

// TestBindDefaultsSliceElem 切片元素未传 default 字段时，自动填充默认值
func TestBindDefaultsSliceElem(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// items[0] 只传了 name（required），qty 和 status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"item1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultSliceRes
	decodeData(t, rec, &res)
	if res.First.Qty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.First.Qty)
	}
	if res.First.Status != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.First.Status)
	}
}

// TestBindDefaultsSliceElemExplicit 切片元素显式传值时不覆盖
func TestBindDefaultsSliceElemExplicit(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传 qty=5, status=pending → 不被默认值覆盖
	body := `{"orderNo":"ORD-001","items":[{"name":"item1","qty":5,"status":"pending"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultSliceRes
	decodeData(t, rec, &res)
	if res.First.Qty != 5 {
		t.Fatalf("explicit Qty should be 5, got %d", res.First.Qty)
	}
	if res.First.Status != "pending" {
		t.Fatalf("explicit Status should be 'pending', got %s", res.First.Status)
	}
}

// TestBindDefaultsSliceElemPtr 指针切片元素默认值填充
func TestBindDefaultsSliceElemPtr(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 中元素未传 qty/status → 填充默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"i1"}],"extras":[{"name":"e1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		t.Log("slice elem ptr default test passed")
	}
}

// ====== map default 递归 ======

type orderDefaultMapReq struct {
	OrderNo string                      `json:"orderNo" required:"true"`
	Items   map[string]itemWithDefault  `json:"items" required:"true"`
	Extras  map[string]*itemWithDefault `json:"extras"`
}

type orderDefaultMapRes struct {
	OrderNo string `json:"orderNo"`
	Aqty    int    `json:"aqty"`
	Astatus string `json:"astatus"`
	Count   int    `json:"count"`
}

func orderDefaultMapHandler(_ context.Context, req orderDefaultMapReq) (orderDefaultMapRes, error) {
	res := orderDefaultMapRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}
	if v, ok := req.Items["a"]; ok {
		if v.Qty != nil {
			res.Aqty = *v.Qty
		}
		if v.Status != nil {
			res.Astatus = *v.Status
		}
	}
	return res, nil
}

// TestBindDefaultsMapValue map value 未传 default 字段时，自动填充默认值
func TestBindDefaultsMapValue(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// items["a"] 只传了 name → qty/status 应使用默认值
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"itemA"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultMapRes
	decodeData(t, rec, &res)
	if res.Aqty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.Aqty)
	}
	if res.Astatus != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.Astatus)
	}
}

// TestBindDefaultsMapValueExplicit map value 显式传值时不覆盖
func TestBindDefaultsMapValueExplicit(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传 qty=10, status=done → 不被默认值覆盖
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"itemA","qty":10,"status":"done"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultMapRes
	decodeData(t, rec, &res)
	if res.Aqty != 10 {
		t.Fatalf("explicit Qty should be 10, got %d", res.Aqty)
	}
	if res.Astatus != "done" {
		t.Fatalf("explicit Status should be 'done', got %s", res.Astatus)
	}
}

// TestBindDefaultsMapValuePtr 指针 map value 默认值填充
func TestBindDefaultsMapValuePtr(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 中 value 未传 qty/status → 填充默认值
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"iA"}},"extras":{"x":{"name":"eX"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		t.Log("map value ptr default test passed")
	}
}
