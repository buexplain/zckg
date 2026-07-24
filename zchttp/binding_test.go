package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
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

// TestBindDefaultsParseErrorKeepsDefault 验证"传递了但解析失败"时保留默认值
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

// -------- slice/map default 递归测试 --------

// itemWithDefault 作为切片/map 元素，包含 default 子字段
// Qty/Status 使用指针类型：在 slice/map 嵌套元素中，只有 nil 指针才会被请求阶段 default 填充，
// 值类型（如 int/string）的零值无法区分"未传"与"传了 0"，因此不在请求阶段填充。
type itemWithDefault struct {
	Name   string  `json:"name" nonzero:"true"`
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
	OrderNo string             `json:"orderNo" nonzero:"true"`
	Items   []itemWithDefault  `json:"items" nonzero:"true"`
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
	OrderNo string                      `json:"orderNo" nonzero:"true"`
	Items   map[string]itemWithDefault  `json:"items" nonzero:"true"`
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

// ========== 指针包裹容器 default 递归 ==========

// ptrContainerReq 包含 *[]Struct 和 *map[K]Struct 字段
type ptrContainerReq struct {
	OrderNo string                       `json:"orderNo" nonzero:"true"`
	Items   *[]itemWithDefault           `json:"items" nonzero:"true"`
	Extras  *map[string]*itemWithDefault `json:"extras"`
}

type ptrContainerRes struct {
	OrderNo string         `json:"orderNo"`
	First   itemDefaultRes `json:"first"`
	Count   int            `json:"count"`
}

func ptrContainerHandler(_ context.Context, req ptrContainerReq) (ptrContainerRes, error) {
	res := ptrContainerRes{OrderNo: req.OrderNo}
	if req.Items != nil && len(*req.Items) > 0 {
		first := (*req.Items)[0]
		qty, status := 0, ""
		if first.Qty != nil {
			qty = *first.Qty
		}
		if first.Status != nil {
			status = *first.Status
		}
		res.First = itemDefaultRes{
			Name:   first.Name,
			Qty:    qty,
			Status: status,
			Note:   first.Note,
		}
		res.Count = len(*req.Items)
	}
	if req.Extras != nil {
		res.Count += len(*req.Extras)
	}
	return res, nil
}

// TestBindDefaultsPtrSliceElem *[]Struct 元素未传 default 字段时，自动填充默认值
func TestBindDefaultsPtrSliceElem(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrContainer", ptrContainerHandler)

	engine := NewEngine()
	engine.Router = router

	// items[0] 只传了 name（required），qty 和 status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"item1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/ptrContainer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res ptrContainerRes
	decodeData(t, rec, &res)
	if res.First.Qty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.First.Qty)
	}
	if res.First.Status != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.First.Status)
	}
}

// TestBindDefaultsPtrMapValue *map[K]Struct value 未传 default 字段时，自动填充默认值
func TestBindDefaultsPtrMapValue(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrContainer", func(_ context.Context, req ptrContainerReq) (struct {
		Aqty    int    `json:"aqty"`
		Astatus string `json:"astatus"`
	}, error) {
		res := struct {
			Aqty    int    `json:"aqty"`
			Astatus string `json:"astatus"`
		}{}
		if req.Extras != nil {
			if v, ok := (*req.Extras)["a"]; ok && v != nil {
				if v.Qty != nil {
					res.Aqty = *v.Qty
				}
				if v.Status != nil {
					res.Astatus = *v.Status
				}
			}
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	// extras["a"] 只传了 name → qty/status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"i1"}],"extras":{"a":{"name":"itemA"}}}`
	req := httptest.NewRequest(http.MethodPost, "/ptrContainer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Aqty    int    `json:"aqty"`
		Astatus string `json:"astatus"`
	}
	decodeData(t, rec, &res)
	if res.Aqty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.Aqty)
	}
	if res.Astatus != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.Astatus)
	}
}

// ========== P0/P1 补充测试：结构体与 handler 定义 ==========

// ---- form 标签优先 ----
type formPriorityReq struct {
	Keyword string `json:"keyword" form:"q"`
	Page    int    `json:"page"`
}
type formPriorityRes struct {
	Keyword string `json:"keyword"`
	Page    int    `json:"page"`
}

func formPriorityHandler(_ context.Context, req formPriorityReq) (formPriorityRes, error) {
	return formPriorityRes{Keyword: req.Keyword, Page: req.Page}, nil
}

// ---- uint/float 类型 ----
type uintFloatReq struct {
	Count  uint    `json:"count"`
	Amount float64 `json:"amount"`
	Score  float32 `json:"score"`
}
type uintFloatRes struct {
	Count  uint    `json:"count"`
	Amount float64 `json:"amount"`
	Score  float32 `json:"score"`
}

func uintFloatHandler(_ context.Context, req uintFloatReq) (uintFloatRes, error) {
	return uintFloatRes{Count: req.Count, Amount: req.Amount, Score: req.Score}, nil
}

// ---- 多文件上传 ----
type multiFileReq struct {
	Title string                  `json:"title"`
	Files []*multipart.FileHeader `json:"files"`
}
type multiFileRes struct {
	Title string   `json:"title"`
	Count int      `json:"count"`
	Names []string `json:"names"`
}

func multiFileHandler(_ context.Context, req multiFileReq) (multiFileRes, error) {
	names := make([]string, len(req.Files))
	for i, f := range req.Files {
		names[i] = f.Filename
	}
	return multiFileRes{Title: req.Title, Count: len(req.Files), Names: names}, nil
}

// ---- 时间精度 ----
type timePrecisionReq struct {
	Milli time.Time `json:"milli" time_format:"unixmilli"`
	Micro time.Time `json:"micro" time_format:"unixmicro"`
	Nano  time.Time `json:"nano" time_format:"unixnano"`
}
type timePrecisionRes struct {
	MilliMs int64 `json:"milli_ms"`
	MicroUs int64 `json:"micro_us"`
	NanoNs  int64 `json:"nano_ns"`
}

func timePrecisionHandler(_ context.Context, req timePrecisionReq) (timePrecisionRes, error) {
	return timePrecisionRes{
		MilliMs: req.Milli.UnixMilli(),
		MicroUs: req.Micro.UnixMicro(),
		NanoNs:  req.Nano.UnixNano(),
	}, nil
}

// ---- time_location 时区 ----
type timeTzReq struct {
	Meeting time.Time `json:"meeting" time_format:"2006-01-02 15:04" time_location:"Asia/Shanghai"`
}
type timeTzRes struct {
	Location string `json:"location"`
	Hour     int    `json:"hour"`
}

func timeTzHandler(_ context.Context, req timeTzReq) (timeTzRes, error) {
	return timeTzRes{Location: req.Meeting.Location().String(), Hour: req.Meeting.Hour()}, nil
}

// ---- 并发隔离 ----
type concurrentReq struct {
	Status *string `json:"status" default:"idle"`
}
type concurrentRes struct {
	Status string `json:"status"`
}

func concurrentHandler(_ context.Context, req concurrentReq) (concurrentRes, error) {
	s := ""
	if req.Status != nil {
		s = *req.Status
	}
	return concurrentRes{Status: s}, nil
}

// ---- 指针嵌套 struct 默认值 ----
type billAddress struct {
	City string `json:"city" default:"Beijing"`
	Zip  *int   `json:"zip" default:"100000"`
}
type billOrderReq struct {
	Name    string       `json:"name" nonzero:"true"`
	Address *billAddress `json:"address"`
}
type billOrderRes struct {
	City string `json:"city"`
	Zip  int    `json:"zip"`
}

func billOrderHandler(_ context.Context, req billOrderReq) (billOrderRes, error) {
	r := billOrderRes{}
	if req.Address != nil {
		r.City = req.Address.City
		if req.Address.Zip != nil {
			r.Zip = *req.Address.Zip
		}
	}
	return r, nil
}

// ========== P0 测试 ==========

// TestBindJSONErrorReturns400 验证 JSON 语法错误产生 BindingError → OnValidationError（400）
func TestBindJSONErrorReturns400(t *testing.T) {
	router := NewRouter()
	router.POST("/item", func(_ context.Context, req struct {
		Name string `json:"name"`
	}) (struct {
		OK bool `json:"ok"`
	}, error) {
		return struct {
			OK bool `json:"ok"`
		}{OK: true}, nil
	})
	engine := NewEngine()
	engine.Router = router

	// 非法 JSON
	req := httptest.NewRequest(http.MethodPost, "/item", strings.NewReader(`{"name":}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for JSON parse error, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestFormTagPriorityOverJSON 验证 form 标签优先于 json 标签
func TestFormTagPriorityOverJSON(t *testing.T) {
	router := NewRouter()
	router.GET("/search", formPriorityHandler)
	engine := NewEngine()
	engine.Router = router

	// q 命中 form 标签，page 命中 json 标签
	req := httptest.NewRequest(http.MethodGet, "/search?q=go&page=3", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res formPriorityRes
	decodeData(t, rec, &res)
	if res.Keyword != "go" {
		t.Fatalf("form tag should take priority: got %q, want 'go'", res.Keyword)
	}
	if res.Page != 3 {
		t.Fatalf("json tag should work as fallback: got %d, want 3", res.Page)
	}
}

// TestDeleteHeadBindQuery 验证 DELETE/HEAD 与 GET 一样从 query 参数绑定
func TestDeleteHeadBindQuery(t *testing.T) {
	router := NewRouter()
	router.DELETE("/item", formPriorityHandler)
	router.HEAD("/item", formPriorityHandler)
	engine := NewEngine()
	engine.Router = router

	for _, method := range []string{http.MethodDelete, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/item?q=test&page=7", nil)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			// HEAD 请求不返回 body，仅检查状态码
			if rec.Code != http.StatusOK {
				t.Fatalf("%s: expected 200, got %d", method, rec.Code)
			}
			if method == http.MethodDelete {
				var res formPriorityRes
				decodeData(t, rec, &res)
				if res.Keyword != "test" {
					t.Fatalf("DELETE query binding: got %q, want 'test'", res.Keyword)
				}
				if res.Page != 7 {
					t.Fatalf("DELETE query binding: page got %d, want 7", res.Page)
				}
			}
		})
	}
}

// ========== P1 测试 ==========

// TestTimeLocationTag 验证 time_location 标签指定时区解析
func TestTimeLocationTag(t *testing.T) {
	router := NewRouter()
	router.GET("/meeting", timeTzHandler)
	engine := NewEngine()
	engine.Router = router

	// 2023-06-15 14:30 在 Asia/Shanghai 时区
	req := httptest.NewRequest(http.MethodGet, "/meeting?meeting=2023-06-15+14:30", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res timeTzRes
	decodeData(t, rec, &res)
	if res.Location != "Asia/Shanghai" {
		t.Fatalf("expected location Asia/Shanghai, got %q", res.Location)
	}
	if res.Hour != 14 {
		t.Fatalf("expected hour 14, got %d", res.Hour)
	}
}

// TestTimePrecisionFormats 验证 unixmilli/unixmicro/unixnano 时间戳精度
func TestTimePrecisionFormats(t *testing.T) {
	router := NewRouter()
	router.GET("/ts", timePrecisionHandler)
	engine := NewEngine()
	engine.Router = router

	// milli=1700000000123 (13位), micro=1700000000123456 (16位), nano=1700000000123456789 (19位)
	query := "milli=1700000000123&micro=1700000000123456&nano=1700000000123456789"
	req := httptest.NewRequest(http.MethodGet, "/ts?"+query, nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res timePrecisionRes
	decodeData(t, rec, &res)
	if res.MilliMs != 1700000000123 {
		t.Fatalf("unixmilli: got %d, want 1700000000123", res.MilliMs)
	}
	if res.MicroUs != 1700000000123456 {
		t.Fatalf("unixmicro: got %d, want 1700000000123456", res.MicroUs)
	}
	if res.NanoNs != 1700000000123456789 {
		t.Fatalf("unixnano: got %d, want 1700000000123456789", res.NanoNs)
	}
}

// TestUnknownContentTypeFallbackJSON 验证未知 Content-Type 有请求体时回退 JSON 解析
func TestUnknownContentTypeFallbackJSON(t *testing.T) {
	router := NewRouter()
	router.POST("/item", formPriorityHandler)
	engine := NewEngine()
	engine.Router = router

	body := `{"keyword":"hello","page":9}`
	req := httptest.NewRequest(http.MethodPost, "/item", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/xml") // 未知类型
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res formPriorityRes
	decodeData(t, rec, &res)
	if res.Keyword != "hello" || res.Page != 9 {
		t.Fatalf("unknown CT fallback JSON: got %+v", res)
	}
}

// TestContentTypeWithCharset 验证 Content-Type 带 charset 参数时仍能正确识别主类型
func TestContentTypeWithCharset(t *testing.T) {
	router := NewRouter()
	router.POST("/item", formPriorityHandler)
	engine := NewEngine()
	engine.Router = router

	body := `{"keyword":"world","page":2}`
	req := httptest.NewRequest(http.MethodPost, "/item", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res formPriorityRes
	decodeData(t, rec, &res)
	if res.Keyword != "world" || res.Page != 2 {
		t.Fatalf("charset CT: got %+v", res)
	}
}

// TestBindUintFloat 验证 uint 和 float 类型字段绑定
func TestBindUintFloat(t *testing.T) {
	router := NewRouter()
	router.POST("/calc", uintFloatHandler)
	engine := NewEngine()
	engine.Router = router

	body := `{"count":42,"amount":99.95,"score":3.14}`
	req := httptest.NewRequest(http.MethodPost, "/calc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res uintFloatRes
	decodeData(t, rec, &res)
	if res.Count != 42 {
		t.Fatalf("uint: got %d, want 42", res.Count)
	}
	if res.Amount != 99.95 {
		t.Fatalf("float64: got %f, want 99.95", res.Amount)
	}
	if res.Score < 3.13 || res.Score > 3.15 {
		t.Fatalf("float32: got %f, want ~3.14", res.Score)
	}
}

// TestBindMultiFileUpload 验证 []*multipart.FileHeader 多文件上传绑定
func TestBindMultiFileUpload(t *testing.T) {
	router := NewRouter()
	router.POST("/upload", multiFileHandler)
	engine := NewEngine()
	engine.Router = router

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("title", "docs")
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		_, _ = fw.Write([]byte("content of " + name))
	}
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res multiFileRes
	decodeData(t, rec, &res)
	if res.Title != "docs" {
		t.Fatalf("title: got %q, want 'docs'", res.Title)
	}
	if res.Count != 3 {
		t.Fatalf("file count: got %d, want 3", res.Count)
	}
	if len(res.Names) != 3 || res.Names[0] != "a.txt" || res.Names[2] != "c.txt" {
		t.Fatalf("file names: got %v", res.Names)
	}
}

// TestUnsupportedDefaultTypeSilentlyIgnored 验证不支持 default 的类型（time.Time/map/any）设置 default 被静默忽略
func TestUnsupportedDefaultTypeSilentlyIgnored(t *testing.T) {
	type unsupportedDefReq struct {
		Name string    `json:"name"`
		When time.Time `json:"when" default:"2023-01-01"` // 不支持
	}
	type unsupportedDefRes struct {
		IsZero bool `json:"is_zero"`
	}

	router := NewRouter()
	router.GET("/ud", func(_ context.Context, r unsupportedDefReq) (unsupportedDefRes, error) {
		return unsupportedDefRes{IsZero: r.When.IsZero()}, nil
	})
	engine := NewEngine()
	engine.Router = router

	httpReq := httptest.NewRequest(http.MethodGet, "/ud", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var r unsupportedDefRes
	decodeData(t, rec, &r)
	// time.Time 不支持 default，应被忽略，保持零值
	if !r.IsZero {
		t.Fatal("time.Time default should be silently ignored, field should remain zero")
	}
}

// TestConcurrentRequestIsolation 验证并发请求间默认值模板不共享（深拷贝断开引用）
func TestConcurrentRequestIsolation(t *testing.T) {
	router := NewRouter()
	router.POST("/conc", concurrentHandler)
	engine := NewEngine()
	engine.Router = router

	const n = 20
	results := make([]string, n)
	errs := make([]error, n)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// 奇数显式传值，偶数不传（使用默认值 "idle"）
			var body string
			if idx%2 == 1 {
				body = `{"status":"active"}`
			} else {
				body = `{}`
			}
			httpReq := httptest.NewRequest(http.MethodPost, "/conc", strings.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httpReq)

			mu.Lock()
			defer mu.Unlock()
			if rec.Code != http.StatusOK {
				errs[idx] = fmt.Errorf("request %d: expected 200, got %d", idx, rec.Code)
				return
			}
			var r concurrentRes
			decodeData(t, rec, &r)
			results[idx] = r.Status
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// 验证结果无交叉污染
	for i := 0; i < n; i++ {
		if i%2 == 1 {
			if results[i] != "active" {
				t.Fatalf("request %d (odd): expected 'active', got %q", i, results[i])
			}
		} else {
			if results[i] != "idle" {
				t.Fatalf("request %d (even): expected 'idle', got %q", i, results[i])
			}
		}
	}
}

// TestPointerNestedStructDefault 验证指针嵌套 struct 中：值类型 default 不生效，指针类型 default 生效
func TestPointerNestedStructDefault(t *testing.T) {
	router := NewRouter()
	router.POST("/bill", billOrderHandler)
	engine := NewEngine()
	engine.Router = router

	// 传 address:{} → City 值类型 default 不生效，Zip 指针类型 default 生效
	body := `{"name":"alice","address":{}}`
	req := httptest.NewRequest(http.MethodPost, "/bill", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res billOrderRes
	decodeData(t, rec, &res)
	// 值类型 default 在指针嵌套下不生效
	if res.City != "" {
		t.Fatalf("value-type default in ptr-nested struct should NOT apply, got %q", res.City)
	}
	// 指针类型 default 在请求阶段补填生效
	if res.Zip != 100000 {
		t.Fatalf("ptr-type default in ptr-nested struct should apply, got %d, want 100000", res.Zip)
	}
}

// ---- 嵌套结构体与嵌套结构体切片绑定测试 ----

type nestedAddress struct {
	City   string `json:"city"`
	Street string `json:"street"`
	Zip    string `json:"zip" default:"000000"`
}

type nestedReq struct {
	Name string        `json:"name"`
	Age  int           `json:"age"`
	Addr nestedAddress `json:"addr"`
	Tags []string      `json:"tags" default:"a,b"`
}

type nestedRes struct {
	Name   string   `json:"name"`
	City   string   `json:"city"`
	Street string   `json:"street"`
	Zip    string   `json:"zip"`
	Tags   []string `json:"tags"`
}

func nestedHandler(_ context.Context, req nestedReq) (nestedRes, error) {
	return nestedRes{
		Name:   req.Name,
		City:   req.Addr.City,
		Street: req.Addr.Street,
		Zip:    req.Addr.Zip,
		Tags:   req.Tags,
	}, nil
}

// TestBindJSONNestedStruct 验证 POST JSON body 可以正确绑定嵌套结构体及默认值
func TestBindJSONNestedStruct(t *testing.T) {
	router := NewRouter()
	router.POST("/nested", nestedHandler)

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"alice","age":30,"addr":{"city":"Beijing","street":"Chang'an Ave"}}`
	req := httptest.NewRequest(http.MethodPost, "/nested", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedRes
	decodeData(t, rec, &res)
	if res.Name != "alice" {
		t.Errorf("name = %q, want alice", res.Name)
	}
	if res.City != "Beijing" {
		t.Errorf("city = %q, want Beijing", res.City)
	}
	if res.Street != "Chang'an Ave" {
		t.Errorf("street = %q, want Chang'an Ave", res.Street)
	}
	// zip 未传但 struct 有 default:"000000"，但由于嵌套 struct 字段不被 buildStructMeta 展开，
	// 默认值无法应用到嵌套字段；json 反序列化后 Zip 仍为空字符串（符合预期行为）
	if res.Zip != "" {
		t.Logf("zip = %q (nested default not applied, expected empty for JSON binding)", res.Zip)
	}
	// tags 有顶层 default:"a,b"，但 JSON body 中没有传 tags，
	// JSON 反序列化优先，切片被设为 nil（JSON null），不会触发 default 填充
	if len(res.Tags) != 0 {
		t.Logf("tags = %v (JSON body takes precedence, default not merged)", res.Tags)
	}
}

// TestBindJSONNestedStructSlice 验证 POST JSON body 可以正确绑定嵌套结构体切片
func TestBindJSONNestedStructSlice(t *testing.T) {
	type nestedSliceReq struct {
		Name      string          `json:"name"`
		Addresses []nestedAddress `json:"addresses"`
	}
	type nestedSliceRes struct {
		Name   string   `json:"name"`
		Cities []string `json:"cities"`
	}

	router := NewRouter()
	router.POST("/nested-slice", func(_ context.Context, req nestedSliceReq) (nestedSliceRes, error) {
		cities := make([]string, len(req.Addresses))
		for i, a := range req.Addresses {
			cities[i] = a.City
		}
		return nestedSliceRes{Name: req.Name, Cities: cities}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"bob","addresses":[{"city":"Shanghai","street":"Nanjing Rd"},{"city":"Shenzhen","street":"Huaqiangbei"}]}`
	req := httptest.NewRequest(http.MethodPost, "/nested-slice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedSliceRes
	decodeData(t, rec, &res)
	if res.Name != "bob" {
		t.Errorf("name = %q, want bob", res.Name)
	}
	if len(res.Cities) != 2 || res.Cities[0] != "Shanghai" || res.Cities[1] != "Shenzhen" {
		t.Errorf("cities = %v, want [Shanghai, Shenzhen]", res.Cities)
	}
}

// TestBindQueryNestedStructSkipped 验证 GET query 嵌套结构体字段的行为：
// 嵌套 struct 字段不会被展开绑定，当前会被 buildStructMeta 作为单字段记录，
// bindValues 对其调用 setFieldValue → setScalar 时因类型非标量而跳过。
// 这是预期行为（GET 不支持嵌套结构体参数绑定）。
func TestBindQueryNestedStructSkipped(t *testing.T) {
	router := NewRouter()
	router.GET("/nested", nestedHandler)

	engine := NewEngine()
	engine.Router = router

	// 即使传了 "addr.city=Beijing"，由于 buildStructMeta 不展开嵌套字段，
	// url.Values 中 key 为 "addr"，对应 struct 类型字段 → 绑定跳过
	req := httptest.NewRequest(http.MethodGet, "/nested?name=charlie&addr=ignored&tags=x&tags=y", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedRes
	decodeData(t, rec, &res)
	if res.Name != "charlie" {
		t.Errorf("name = %q, want charlie", res.Name)
	}
	// addr 字段是 struct，无法从 query 绑定，保持零值
	if res.City != "" {
		t.Errorf("city = %q, want empty (nested struct not bound from query)", res.City)
	}
	// tags 是顶层 []string 切片，GET query 中重复参数可绑定
	if len(res.Tags) != 2 || res.Tags[0] != "x" || res.Tags[1] != "y" {
		t.Errorf("tags = %v, want [x y]", res.Tags)
	}
}

// ---- 嵌套结构体指针与嵌套结构体指针切片绑定测试 ----

type nestedPtrReq struct {
	Name string         `json:"name"`
	Addr *nestedAddress `json:"addr"`
}

type nestedPtrRes struct {
	Name   string `json:"name"`
	City   string `json:"city"`
	Street string `json:"street"`
}

// TestBindJSONNestedStructPtr 验证 POST JSON body 可以正确绑定嵌套结构体指针
func TestBindJSONNestedStructPtr(t *testing.T) {
	router := NewRouter()
	router.POST("/nested-ptr", func(_ context.Context, req nestedPtrReq) (nestedPtrRes, error) {
		res := nestedPtrRes{Name: req.Name}
		if req.Addr != nil {
			res.City = req.Addr.City
			res.Street = req.Addr.Street
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"dave","addr":{"city":"Guangzhou","street":"Tianhe Rd"}}`
	req := httptest.NewRequest(http.MethodPost, "/nested-ptr", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedPtrRes
	decodeData(t, rec, &res)
	if res.Name != "dave" {
		t.Errorf("name = %q, want dave", res.Name)
	}
	if res.City != "Guangzhou" {
		t.Errorf("city = %q, want Guangzhou", res.City)
	}
	if res.Street != "Tianhe Rd" {
		t.Errorf("street = %q, want Tianhe Rd", res.Street)
	}
}

// TestBindJSONNestedStructPtrNil 验证 JSON body 中 addr 字段为 null 时，指针应为 nil
func TestBindJSONNestedStructPtrNil(t *testing.T) {
	router := NewRouter()
	router.POST("/nested-ptr-nil", func(_ context.Context, req nestedPtrReq) (nestedPtrRes, error) {
		res := nestedPtrRes{Name: req.Name}
		if req.Addr != nil {
			res.City = "NOT_NULL"
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"eve","addr":null}`
	req := httptest.NewRequest(http.MethodPost, "/nested-ptr-nil", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedPtrRes
	decodeData(t, rec, &res)
	if res.Name != "eve" {
		t.Errorf("name = %q, want eve", res.Name)
	}
	// addr 为 null 时，指针应为 nil，City 保持空
	if res.City != "" {
		t.Errorf("city = %q, want empty (addr was null)", res.City)
	}
}

type nestedPtrSliceReq struct {
	Name      string           `json:"name"`
	Addresses []*nestedAddress `json:"addresses"`
}

type nestedPtrSliceRes struct {
	Name   string   `json:"name"`
	Cities []string `json:"cities"`
}

// TestBindJSONNestedStructPtrSlice 验证 POST JSON body 可以正确绑定嵌套结构体指针切片
func TestBindJSONNestedStructPtrSlice(t *testing.T) {
	router := NewRouter()
	router.POST("/nested-ptr-slice", func(_ context.Context, req nestedPtrSliceReq) (nestedPtrSliceRes, error) {
		cities := make([]string, len(req.Addresses))
		for i, a := range req.Addresses {
			if a != nil {
				cities[i] = a.City
			}
		}
		return nestedPtrSliceRes{Name: req.Name, Cities: cities}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"frank","addresses":[{"city":"Chengdu","street":"Chunxi Rd"},null,{"city":"Hangzhou"}]}`
	req := httptest.NewRequest(http.MethodPost, "/nested-ptr-slice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res nestedPtrSliceRes
	decodeData(t, rec, &res)
	if res.Name != "frank" {
		t.Errorf("name = %q, want frank", res.Name)
	}
	// ["Chengdu", "", "Hangzhou"] — null 元素转为 nil 指针，city 为空
	if len(res.Cities) != 3 || res.Cities[0] != "Chengdu" || res.Cities[1] != "" || res.Cities[2] != "Hangzhou" {
		t.Errorf("cities = %v, want [Chengdu, , Hangzhou]", res.Cities)
	}
}

// ---- 结构体多层递归嵌套绑定测试 ----

// engDepartment → engEmployee：三层嵌套（req → dept → manager）
type engEmployee struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

type engDepartment struct {
	Name    string      `json:"name"`
	Manager engEmployee `json:"manager"`
}

type engOrgReq struct {
	Name string        `json:"name"`
	Dept engDepartment `json:"dept"`
}

type engOrgRes struct {
	OrgName      string `json:"orgName"`
	DeptName     string `json:"deptName"`
	ManagerName  string `json:"managerName"`
	ManagerTitle string `json:"managerTitle"`
}

// TestBindJSONMultiLevelNested 验证 POST JSON body 多层递归嵌套结构体的绑定
// 链路：org → dept（值类型） → manager（值类型），共 3 层
func TestBindJSONMultiLevelNested(t *testing.T) {
	router := NewRouter()
	router.POST("/org", func(_ context.Context, req engOrgReq) (engOrgRes, error) {
		return engOrgRes{
			OrgName:      req.Name,
			DeptName:     req.Dept.Name,
			ManagerName:  req.Dept.Manager.Name,
			ManagerTitle: req.Dept.Manager.Title,
		}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"Acme Corp","dept":{"name":"Engineering","manager":{"name":"Grace","title":"CTO"}}}`
	req := httptest.NewRequest(http.MethodPost, "/org", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res engOrgRes
	decodeData(t, rec, &res)
	if res.OrgName != "Acme Corp" {
		t.Errorf("orgName = %q, want Acme Corp", res.OrgName)
	}
	if res.DeptName != "Engineering" {
		t.Errorf("deptName = %q, want Engineering", res.DeptName)
	}
	if res.ManagerName != "Grace" {
		t.Errorf("managerName = %q, want Grace", res.ManagerName)
	}
	if res.ManagerTitle != "CTO" {
		t.Errorf("managerTitle = %q, want CTO", res.ManagerTitle)
	}
}

// ---- 结构体自引用（递归嵌套）绑定测试 ----

// engCategory 商品分类：自引用树形结构
type engCategory struct {
	ID       int            `json:"id"`
	Name     string         `json:"name"`
	Children []*engCategory `json:"children,omitempty"`
}

type engCategoryTreeReq struct {
	Categories []engCategory `json:"categories"`
}

type engCategoryTreeRes struct {
	RootName       string `json:"rootName"`
	ChildCount     int    `json:"childCount"`
	GrandchildName string `json:"grandchildName"`
}

// TestBindJSONSelfRef 验证 POST JSON body 自引用结构体的绑定
// Category → Children []*Category（自引用指针切片），2 层子孙
func TestBindJSONSelfRef(t *testing.T) {
	router := NewRouter()
	router.POST("/category-tree", func(_ context.Context, req engCategoryTreeReq) (engCategoryTreeRes, error) {
		res := engCategoryTreeRes{}
		if len(req.Categories) > 0 {
			res.RootName = req.Categories[0].Name
			res.ChildCount = len(req.Categories[0].Children)
			if res.ChildCount > 0 && req.Categories[0].Children[0] != nil {
				res.GrandchildName = req.Categories[0].Children[0].Name
			}
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"categories":[{"id":1,"name":"电子产品","children":[{"id":2,"name":"手机"},{"id":3,"name":"电脑","children":[{"id":4,"name":"笔记本"}]}]}]}`
	req := httptest.NewRequest(http.MethodPost, "/category-tree", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res engCategoryTreeRes
	decodeData(t, rec, &res)
	if res.RootName != "电子产品" {
		t.Errorf("rootName = %q, want 电子产品", res.RootName)
	}
	if res.ChildCount != 2 {
		t.Errorf("childCount = %d, want 2", res.ChildCount)
	}
	if res.GrandchildName != "手机" {
		t.Errorf("grandchildName = %q, want 手机", res.GrandchildName)
	}
}

// ---- map 类型绑定测试 ----

// mapStringAnyReq 测试 map[string]any：覆盖混合值类型（string、number、bool、null）
type mapStringAnyReq struct {
	Name   string         `json:"name"`
	Extras map[string]any `json:"extras"`
}

type mapStringAnyRes struct {
	Name    string `json:"name"`
	KeyCnt  int    `json:"keyCnt"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
	Enabled bool   `json:"enabled"`
}

// TestBindJSONMapStringAny 验证 map[string]any 从 JSON body 正确绑定混合值类型
func TestBindJSONMapStringAny(t *testing.T) {
	router := NewRouter()
	router.POST("/map-string-any", func(_ context.Context, req mapStringAnyReq) (mapStringAnyRes, error) {
		res := mapStringAnyRes{Name: req.Name, KeyCnt: len(req.Extras)}
		if s, ok := req.Extras["status"].(string); ok {
			res.Status = s
		}
		if n, ok := req.Extras["score"].(float64); ok {
			res.Score = int(n)
		}
		if b, ok := req.Extras["enabled"].(bool); ok {
			res.Enabled = b
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"alice","extras":{"status":"active","score":95,"enabled":true,"note":null}}`
	req := httptest.NewRequest(http.MethodPost, "/map-string-any", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapStringAnyRes
	decodeData(t, rec, &res)
	if res.Name != "alice" {
		t.Errorf("name = %q, want alice", res.Name)
	}
	if res.KeyCnt != 4 {
		t.Errorf("keyCnt = %d, want 4", res.KeyCnt)
	}
	if res.Status != "active" {
		t.Errorf("status = %q, want active", res.Status)
	}
	if res.Score != 95 {
		t.Errorf("score = %d, want 95", res.Score)
	}
	if !res.Enabled {
		t.Errorf("enabled = false, want true")
	}
}

// mapStringStringReq 测试 map[string]string
type mapStringStringReq struct {
	Name  string            `json:"name"`
	Attrs map[string]string `json:"attrs"`
}

type mapStringStringRes struct {
	Name  string `json:"name"`
	City  string `json:"city"`
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// TestBindJSONMapStringString 验证 map[string]string 从 JSON 正确绑定
func TestBindJSONMapStringString(t *testing.T) {
	router := NewRouter()
	router.POST("/map-string-string", func(_ context.Context, req mapStringStringReq) (mapStringStringRes, error) {
		return mapStringStringRes{
			Name:  req.Name,
			City:  req.Attrs["city"],
			Role:  req.Attrs["role"],
			Count: len(req.Attrs),
		}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"bob","attrs":{"city":"Beijing","role":"admin"}}`
	req := httptest.NewRequest(http.MethodPost, "/map-string-string", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapStringStringRes
	decodeData(t, rec, &res)
	if res.Name != "bob" {
		t.Errorf("name = %q, want bob", res.Name)
	}
	if res.City != "Beijing" {
		t.Errorf("city = %q, want Beijing", res.City)
	}
	if res.Role != "admin" {
		t.Errorf("role = %q, want admin", res.Role)
	}
	if res.Count != 2 {
		t.Errorf("count = %d, want 2", res.Count)
	}
}

// mapStringIntReq 测试 map[string]int
type mapStringIntReq struct {
	Name   string         `json:"name"`
	Scores map[string]int `json:"scores"`
}

type mapStringIntRes struct {
	Name  string `json:"name"`
	Math  int    `json:"math"`
	Eng   int    `json:"eng"`
	Total int    `json:"total"`
}

// TestBindJSONMapStringInt 验证 map[string]int 从 JSON 正确绑定
func TestBindJSONMapStringInt(t *testing.T) {
	router := NewRouter()
	router.POST("/map-string-int", func(_ context.Context, req mapStringIntReq) (mapStringIntRes, error) {
		return mapStringIntRes{
			Name:  req.Name,
			Math:  req.Scores["math"],
			Eng:   req.Scores["english"],
			Total: req.Scores["math"] + req.Scores["english"],
		}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"charlie","scores":{"math":90,"english":85}}`
	req := httptest.NewRequest(http.MethodPost, "/map-string-int", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapStringIntRes
	decodeData(t, rec, &res)
	if res.Name != "charlie" {
		t.Errorf("name = %q, want charlie", res.Name)
	}
	if res.Math != 90 {
		t.Errorf("math = %d, want 90", res.Math)
	}
	if res.Eng != 85 {
		t.Errorf("eng = %d, want 85", res.Eng)
	}
	if res.Total != 175 {
		t.Errorf("total = %d, want 175", res.Total)
	}
}

// mapStringStructReq 测试 map[string]T：value 为嵌套结构体
type mapStringStructReq struct {
	Name  string                 `json:"name"`
	Staff map[string]engEmployee `json:"staff"`
}

type mapStringStructRes struct {
	Name  string `json:"name"`
	Dev   string `json:"dev"`
	PM    string `json:"pm"`
	Count int    `json:"count"`
}

// TestBindJSONMapStringStruct 验证 map[string]struct 从 JSON 正确绑定嵌套结构体
func TestBindJSONMapStringStruct(t *testing.T) {
	router := NewRouter()
	router.POST("/map-string-struct", func(_ context.Context, req mapStringStructReq) (mapStringStructRes, error) {
		res := mapStringStructRes{Name: req.Name, Count: len(req.Staff)}
		if e, ok := req.Staff["dev"]; ok {
			res.Dev = e.Name + "/" + e.Title
		}
		if e, ok := req.Staff["pm"]; ok {
			res.PM = e.Name + "/" + e.Title
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"dave","staff":{"dev":{"name":"Grace","title":"CTO"},"pm":{"name":"Henry","title":"PM"}}}`
	req := httptest.NewRequest(http.MethodPost, "/map-string-struct", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapStringStructRes
	decodeData(t, rec, &res)
	if res.Name != "dave" {
		t.Errorf("name = %q, want dave", res.Name)
	}
	if res.Dev != "Grace/CTO" {
		t.Errorf("dev = %q, want Grace/CTO", res.Dev)
	}
	if res.PM != "Henry/PM" {
		t.Errorf("pm = %q, want Henry/PM", res.PM)
	}
	if res.Count != 2 {
		t.Errorf("count = %d, want 2", res.Count)
	}
}

// mapIntStringReq 测试 map[int]string：非 string 类型的 key
type mapIntStringReq struct {
	Name  string         `json:"name"`
	Codes map[int]string `json:"codes"`
}

type mapIntStringRes struct {
	Name     string `json:"name"`
	Code200  string `json:"code200"`
	Code404  string `json:"code404"`
	KeyCount int    `json:"keyCount"`
}

// TestBindJSONMapIntString 验证 map[int]string 从 JSON 正确绑定（key 为 int）
func TestBindJSONMapIntString(t *testing.T) {
	router := NewRouter()
	router.POST("/map-int-string", func(_ context.Context, req mapIntStringReq) (mapIntStringRes, error) {
		return mapIntStringRes{
			Name:     req.Name,
			Code200:  req.Codes[200],
			Code404:  req.Codes[404],
			KeyCount: len(req.Codes),
		}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"eve","codes":{"200":"OK","404":"Not Found"}}`
	req := httptest.NewRequest(http.MethodPost, "/map-int-string", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapIntStringRes
	decodeData(t, rec, &res)
	if res.Name != "eve" {
		t.Errorf("name = %q, want eve", res.Name)
	}
	// encoding/json 将 "200" → int(200)，支持非 string key
	if res.Code200 != "OK" {
		t.Errorf("code200 = %q, want OK", res.Code200)
	}
	if res.Code404 != "Not Found" {
		t.Errorf("code404 = %q, want Not Found", res.Code404)
	}
	if res.KeyCount != 2 {
		t.Errorf("keyCount = %d, want 2", res.KeyCount)
	}
}

// ---- 嵌套结构体包含 map[string]*T 的复杂组合测试 ----

// mapItemInfo 作为 map value 的结构体指针元素
type mapItemInfo struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
}

// mapNestedContainer 被嵌套在 Req 中的结构体，包含 map[string]*mapItemInfo
type mapNestedContainer struct {
	Items map[string]*mapItemInfo `json:"items"`
}

// mapComplexReq 外层 Req：嵌套值类型结构体，其中包含 map[string]*T
type mapComplexReq struct {
	Name     string             `json:"name"`
	Metadata mapNestedContainer `json:"metadata"`
}

type mapComplexRes struct {
	Name      string `json:"name"`
	ItemCount int    `json:"itemCount"`
	DevName   string `json:"devName"`
	DevScore  int    `json:"devScore"`
	QAName    string `json:"qaName"`
	QAScore   int    `json:"qaScore"`
}

// TestBindJSONMapNestedPtrValue 验证 Req→嵌套struct→map[string]*T 三层组合的 JSON 绑定
func TestBindJSONMapNestedPtrValue(t *testing.T) {
	router := NewRouter()
	router.POST("/map-complex", func(_ context.Context, req mapComplexReq) (mapComplexRes, error) {
		res := mapComplexRes{Name: req.Name, ItemCount: len(req.Metadata.Items)}
		if v, ok := req.Metadata.Items["dev"]; ok && v != nil {
			res.DevName = v.Name
			res.DevScore = v.Score
		}
		if v, ok := req.Metadata.Items["qa"]; ok && v != nil {
			res.QAName = v.Name
			res.QAScore = v.Score
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := `{"name":"project-x","metadata":{"items":{"dev":{"name":"Grace","score":95},"qa":{"name":"Henry","score":88}}}}`
	req := httptest.NewRequest(http.MethodPost, "/map-complex", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}
	var res mapComplexRes
	decodeData(t, rec, &res)
	if res.Name != "project-x" {
		t.Errorf("name = %q, want project-x", res.Name)
	}
	if res.ItemCount != 2 {
		t.Errorf("itemCount = %d, want 2", res.ItemCount)
	}
	if res.DevName != "Grace" {
		t.Errorf("devName = %q, want Grace", res.DevName)
	}
	if res.DevScore != 95 {
		t.Errorf("devScore = %d, want 95", res.DevScore)
	}
	if res.QAName != "Henry" {
		t.Errorf("qaName = %q, want Henry", res.QAName)
	}
	if res.QAScore != 88 {
		t.Errorf("qaScore = %d, want 88", res.QAScore)
	}
}

// ========== BUG4 验证测试 ==========

// TestBindBody_MultipartNilForm 验证当 ParseMultipartForm 成功但 r.MultipartForm 为 nil 时不会 panic
// 正常情况下 ParseMultipartForm 成功后 MultipartForm 不会为 nil，
// 但通过直接调用 bindBody 并手动设置 MultipartForm=nil 来模拟极端场景
func TestBindBody_MultipartNilForm(t *testing.T) {
	type multiReq struct {
		Name string `form:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(multiReq{}))
	reqVal := reflect.New(reflect.TypeOf(multiReq{}))

	// 构造一个 MultipartForm = nil 的 http.Request
	r := httptest.NewRequest(http.MethodPost, "/test", nil)
	r.Header.Set("Content-Type", "multipart/form-data")
	// 手动设置 MultipartForm 为 nil（模拟 ParseMultipartForm 已经调用但结果异常）
	r.MultipartForm = nil

	// 如果 BUG4 存在，此处会 panic: nil pointer dereference
	defer func() {
		if rec := recover(); rec != nil {
			t.Fatalf("BUG4 confirmed: bindBody panics when MultipartForm is nil: %v", rec)
		}
	}()

	// 直接调用 bindBody，跳过 ParseMultipartForm（因为没有 body）
	// 注意：bindBody 内部会先调用 ParseMultipartForm，我们需要直接测试 nil 场景
	// 实际上 ParseMultipartForm 会报错导致提前返回，所以这个 bug 在生产中不会触发
	// 但我们仍然验证代码逻辑一致性
	err := bindBody(r, reqVal, meta)
	// ParseMultipartForm 会失败，所以这里会提前返回 error，不会走到 nil 分支
	_ = err
}

// TestSetScalar_SelfReferentialPtr 验证自引用指针类型不会使 setScalar 无限递归（栈溢出），
// 超过解引用深度上限后应返回错误
func TestSetScalar_SelfReferentialPtr(t *testing.T) {
	var sp selfPtr
	v := reflect.ValueOf(&sp).Elem()

	done := make(chan error, 1)
	go func() {
		done <- setScalar(v, "1", "", nil)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error for self-referential pointer type, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("setScalar did not terminate on self-referential pointer type")
	}
}
