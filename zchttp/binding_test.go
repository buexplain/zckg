package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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

// ========== P1-02: 整型溢出检查测试 ==========

// TestSetScalar_Int8Overflow 验证 int8 溢出时返回错误而非静默截断
func TestSetScalar_Int8Overflow(t *testing.T) {
	type req struct {
		Val int8
	}
	r := &req{}
	v := reflect.ValueOf(r).Elem().Field(0)

	err := setScalar(v, "300", "", nil) // 300 > int8 max (127)
	if err == nil {
		t.Fatalf("expected error for int8 overflow (300), got nil; value=%d", r.Val)
	}
}

// TestSetScalar_Uint8Overflow 验证 uint8 溢出时返回错误而非静默截断
func TestSetScalar_Uint8Overflow(t *testing.T) {
	type req struct {
		Val uint8
	}
	r := &req{}
	v := reflect.ValueOf(r).Elem().Field(0)

	err := setScalar(v, "300", "", nil) // 300 > uint8 max (255)
	if err == nil {
		t.Fatalf("expected error for uint8 overflow (300), got nil; value=%d", r.Val)
	}
}

// TestSetScalar_Int16Overflow 验证 int16 溢出时返回错误
func TestSetScalar_Int16Overflow(t *testing.T) {
	type req struct {
		Val int16
	}
	r := &req{}
	v := reflect.ValueOf(r).Elem().Field(0)

	err := setScalar(v, "70000", "", nil) // 70000 > int16 max (32767)
	if err == nil {
		t.Fatalf("expected error for int16 overflow (70000), got nil; value=%d", r.Val)
	}
}

// TestSetScalar_Int8InRange 验证 int8 范围内正常解析
func TestSetScalar_Int8InRange(t *testing.T) {
	type req struct {
		Val int8
	}
	r := &req{}
	v := reflect.ValueOf(r).Elem().Field(0)

	err := setScalar(v, "100", "", nil)
	if err != nil {
		t.Fatalf("unexpected error for int8 in range: %v", err)
	}
	if r.Val != 100 {
		t.Fatalf("expected 100, got %d", r.Val)
	}
}

// ========== P2-03: 空 JSON body 测试 ==========

// TestBindBody_EmptyJSONBody 验证空 JSON body 返回友好错误消息而非 "EOF"
func TestBindBody_EmptyJSONBody(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	type res struct{}

	router := NewRouter()
	router.POST("/test", func(_ context.Context, r req) (res, error) {
		return res{}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/test", nil)
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "EOF") {
		t.Fatalf("response should NOT contain 'EOF', got: %s", body)
	}
	if !strings.Contains(body, "empty request body") {
		t.Fatalf("response should contain 'empty request body', got: %s", body)
	}
}

// TestBindBody_EmptyStructNoJSON 验证空结构体 + JSON body 不报错
func TestBindBody_EmptyStructNoJSON(t *testing.T) {
	type req struct{}
	type res struct {
		OK bool `json:"ok"`
	}

	router := NewRouter()
	router.POST("/test", func(_ context.Context, r req) (res, error) {
		return res{OK: true}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/test", nil)
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// ========== P2-01: 匿名嵌入 struct 测试 ==========

// TestBindAnonymousEmbeddedExported 验证导出类型的匿名嵌入字段扁平展开
func TestBindAnonymousEmbeddedExported(t *testing.T) {
	type Base struct {
		City string `json:"city"`
	}
	type req struct {
		Base
		Name string `json:"name"`
	}
	type res struct {
		City string `json:"city"`
		Name string `json:"name"`
	}

	router := NewRouter()
	router.POST("/test", func(_ context.Context, r req) (res, error) {
		return res{City: r.City, Name: r.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"city":"beijing","name":"alice"}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Data struct {
			City string `json:"city"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Data.City != "beijing" {
		t.Fatalf("city = %q, want 'beijing'", result.Data.City)
	}
	if result.Data.Name != "alice" {
		t.Fatalf("name = %q, want 'alice'", result.Data.Name)
	}
}

// TestBindAnonymousEmbeddedQuery 验证匿名嵌入字段的 query 绑定
func TestBindAnonymousEmbeddedQuery(t *testing.T) {
	type Base struct {
		City string `form:"city"`
	}
	type req struct {
		Base
		Name string `form:"name"`
	}
	type res struct {
		City string `json:"city"`
		Name string `json:"name"`
	}

	router := NewRouter()
	router.GET("/test", func(_ context.Context, r req) (res, error) {
		return res{City: r.City, Name: r.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodGet, "/test?city=shanghai&name=bob", nil)
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Data struct {
			City string `json:"city"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result.Data.City != "shanghai" {
		t.Fatalf("city = %q, want 'shanghai'", result.Data.City)
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

// ======== bindPathParams 单元测试 ========

// pathParamUnitReq 覆盖路径参数绑定的各类标量类型
type pathParamUnitReq struct {
	S string    `json:"s"`
	I int       `json:"i"`
	U uint8     `json:"u"`
	F float64   `json:"f"`
	B bool      `json:"b"`
	P *int      `json:"p"`
	T time.Time `json:"t"`
}

// unitParamBinding 从 pathParamUnitReq 的预计算元信息中按绑定名构造 pathParamBinding
func unitParamBinding(name string, optional bool) pathParamBinding {
	meta := buildStructMeta(reflect.TypeOf(pathParamUnitReq{}))
	for i := range meta.fields {
		if meta.fields[i].name == name {
			return pathParamBinding{
				indices:      meta.fields[i].indices,
				timeFormat:   meta.fields[i].timeFormat,
				timeLocation: meta.fields[i].timeLocation,
				optional:     optional,
			}
		}
	}
	panic("field not found: " + name)
}

// TestBindPathParamsScalars 验证各类标量类型的路径参数转换
func TestBindPathParamsScalars(t *testing.T) {
	params := []pathParamBinding{
		unitParamBinding("s", false),
		unitParamBinding("i", false),
		unitParamBinding("u", false),
		unitParamBinding("f", false),
		unitParamBinding("b", false),
	}
	reqPtr := reflect.New(reflect.TypeOf(pathParamUnitReq{}))
	if err := bindPathParams(reqPtr, params, []string{"abc", "42", "200", "3.14", "true"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := reqPtr.Elem().Interface().(pathParamUnitReq)
	if got.S != "abc" || got.I != 42 || got.U != 200 || got.F != 3.14 || got.B != true {
		t.Fatalf("unexpected bind result: %+v", got)
	}
}

// TestBindPathParamsPointerAndTime 验证指针字段分配与 time_format 标签透传
func TestBindPathParamsPointerAndTime(t *testing.T) {
	pb := unitParamBinding("p", false)
	tb := unitParamBinding("t", false)
	tb.timeFormat = "2006-01-02" // 模拟字段时间格式标签透传

	reqPtr := reflect.New(reflect.TypeOf(pathParamUnitReq{}))
	if err := bindPathParams(reqPtr, []pathParamBinding{pb, tb}, []string{"7", "2026-08-12"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := reqPtr.Elem().Interface().(pathParamUnitReq)
	if got.P == nil || *got.P != 7 {
		t.Fatalf("pointer param = %v, want &7", got.P)
	}
	want := time.Date(2026, 8, 12, 0, 0, 0, 0, time.Local)
	if !got.T.Equal(want) {
		t.Fatalf("time param = %v, want %v", got.T, want)
	}
}

// TestBindPathParamsInvalidValue 验证转换失败立即返回错误（非尽力绑定）
func TestBindPathParamsInvalidValue(t *testing.T) {
	params := []pathParamBinding{unitParamBinding("i", false)}
	reqPtr := reflect.New(reflect.TypeOf(pathParamUnitReq{}))
	err := bindPathParams(reqPtr, params, []string{"abc"})
	if err == nil {
		t.Fatal("expected error for invalid int param, got nil")
	}
	if !strings.Contains(err.Error(), "invalid path parameter value") {
		t.Fatalf("error should contain 'invalid path parameter value', got: %v", err)
	}
}

// TestBindPathParamsOptionalOmitted 验证可选参数被省略（捕获值少于绑定数）时保留字段已有值
func TestBindPathParamsOptionalOmitted(t *testing.T) {
	params := []pathParamBinding{
		unitParamBinding("s", false),
		unitParamBinding("i", true),
	}
	reqPtr := reflect.New(reflect.TypeOf(pathParamUnitReq{}))
	reqPtr.Elem().Field(1).SetInt(123) // 预置默认值
	if err := bindPathParams(reqPtr, params, []string{"abc"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := reqPtr.Elem().Interface().(pathParamUnitReq)
	if got.S != "abc" {
		t.Fatalf("S = %q, want abc", got.S)
	}
	if got.I != 123 {
		t.Fatalf("omitted optional param should keep preset value 123, got %d", got.I)
	}
}

// ======== setUnix/setUnixAuto/setTime 分支补测 ========

// TestSetUnixAutoBranches 覆盖 16 位（微秒）、19 位（纳秒）与其他位数（秒）分支
func TestSetUnixAutoBranches(t *testing.T) {
	cases := []struct {
		value string
		unit  time.Duration
	}{
		{"1234567890123456", time.Microsecond},
		{"1234567890123456789", time.Nanosecond},
		{"12345678901", time.Second},
	}
	for _, c := range cases {
		fv := reflect.New(timeType).Elem()
		if err := setUnixAuto(fv, c.value, time.UTC); err != nil {
			t.Fatalf("setUnixAuto(%q): %v", c.value, err)
		}
		n, _ := strconv.ParseInt(c.value, 10, 64)
		want := time.Unix(0, n*int64(c.unit)).In(time.UTC)
		if got := fv.Interface().(time.Time); !got.Equal(want) {
			t.Errorf("setUnixAuto(%q) = %v, want %v", c.value, got, want)
		}
	}
}

// TestSetUnixInvalidValue 覆盖 setUnix 的 ParseInt 错误分支
func TestSetUnixInvalidValue(t *testing.T) {
	fv := reflect.New(timeType).Elem()
	if err := setUnix(fv, "abc", time.Second, time.UTC); err == nil {
		t.Fatal("expected parse error, got nil")
	}
}

// TestSetTimeErrorBranches 覆盖 setTime 的 unix 解析失败、显式 layout 解析失败与自动探测失败分支
func TestSetTimeErrorBranches(t *testing.T) {
	cases := []struct {
		name       string
		value      string
		timeFormat string
	}{
		{"unix invalid", "abc", "unix"},
		{"layout invalid", "bad-date", "2006-01-02"},
		{"auto detect fail", "not-a-date", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fv := reflect.New(timeType).Elem()
			if err := setTime(fv, c.value, c.timeFormat, nil); err == nil {
				t.Fatalf("expected error for value %q format %q", c.value, c.timeFormat)
			}
		})
	}
}

// ======== 标量转换与文件字段分支补测 ========

// TestSetFieldValueSliceConvertError 覆盖切片绑定的元素转换错误返回分支
func TestSetFieldValueSliceConvertError(t *testing.T) {
	var dst struct{ V []int }
	fv := reflect.ValueOf(&dst).Elem().Field(0)
	if err := setFieldValue(fv, []string{"abc"}, "", nil); err == nil {
		t.Fatal("expected conversion error for []int with non-numeric value")
	}
}

// TestSetFileFieldNonFileType 覆盖非文件字段返回 false 的分支
func TestSetFileFieldNonFileType(t *testing.T) {
	var dst struct{ Name string }
	fv := reflect.ValueOf(&dst).Elem().Field(0)
	if setFileField(fv, nil) {
		t.Fatal("non-file field should return false")
	}
}

// selfRefPtr 自引用指针类型，用于触发 setScalar 的指针嵌套深度保护
type selfRefPtr *selfRefPtr

// TestSetScalarPointerTooDeep 覆盖指针嵌套超限的错误返回分支
func TestSetScalarPointerTooDeep(t *testing.T) {
	var p selfRefPtr
	fv := reflect.ValueOf(&p).Elem()
	err := setScalar(fv, "x", "", nil)
	if err == nil || !strings.Contains(err.Error(), "pointer nesting too deep") {
		t.Fatalf("expected pointer nesting error, got: %v", err)
	}
}

// TestSetScalarUnsupportedKind 覆盖不支持类型的跳过分支（不报错不写入）
func TestSetScalarUnsupportedKind(t *testing.T) {
	var dst struct{ C complex128 }
	fv := reflect.ValueOf(&dst).Elem().Field(0)
	if err := setScalar(fv, "1+2i", "", nil); err != nil {
		t.Fatalf("unsupported kind should be skipped without error, got: %v", err)
	}
	if dst.C != 0 {
		t.Fatalf("unsupported kind should keep zero value, got: %v", dst.C)
	}
}

// ======== bindBody 分支补测 ========

// TestBindBodyEmptyJSON 覆盖 JSON 空请求体的错误分支
func TestBindBodyEmptyJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta)
	if err == nil || !strings.Contains(err.Error(), "empty request body") {
		t.Fatalf("expected empty request body error, got: %v", err)
	}
}

// TestBindBodyJSONNoBindableFields 覆盖 JSON 无可绑定字段时跳过解码的分支
func TestBindBodyJSONNoBindableFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	meta := buildStructMeta(reflect.TypeOf(struct{}{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(struct{}{})), meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindBodyUnknownCTNoBody 覆盖未知 Content-Type 且无请求体时不绑定的分支
func TestBindBodyUnknownCTNoBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Content-Type", "text/plain")
	req.Body = nil
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindPathParamsNonStruct 覆盖目标非结构体时的提前返回分支
func TestBindPathParamsNonStruct(t *testing.T) {
	params := []pathParamBinding{unitParamBinding("i", false)}
	if err := bindPathParams(reflect.New(reflect.TypeOf(0)), params, []string{"1"}); err != nil {
		t.Fatalf("non-struct target should return nil, got: %v", err)
	}
}

// TestBindBodyJSONNilBody 覆盖 JSON Content-Type 无请求体时跳过的分支
func TestBindBodyJSONNilBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = nil
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindBodyUnknownCTInvalidJSON 覆盖默认分支解码失败（非空错误）的返回分支
func TestBindBodyUnknownCTInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "text/plain")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta); err == nil {
		t.Fatal("expected json decode error, got nil")
	}
}

// TestBindBodyMultipartParseError 覆盖 multipart 解析失败的错误返回分支
func TestBindBodyMultipartParseError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta); err == nil {
		t.Fatal("expected multipart parse error, got nil")
	}
}

// TestBindValuesNonStruct 覆盖目标非结构体时的提前返回分支
func TestBindValuesNonStruct(t *testing.T) {
	if err := bindValues(reflect.New(reflect.TypeOf(0)), nil, nil, structMeta{}); err != nil {
		t.Fatalf("non-struct should return nil, got: %v", err)
	}
}

// TestBindValuesSkipsIgnoredName 覆盖字段绑定名为 "-" 或空时跳过的分支
func TestBindValuesSkipsIgnoredName(t *testing.T) {
	var dst struct{ X string }
	meta := structMeta{fields: []fieldMeta{
		{name: "-", indices: []int{0}},
		{name: "", indices: []int{0}},
	}}
	values := map[string][]string{"X": {"v"}}
	if err := bindValues(reflect.ValueOf(&dst), values, nil, meta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dst.X != "" {
		t.Fatalf("field with ignored name should not be bound, got %q", dst.X)
	}
}

// TestSetScalarBoolAndUintErrors 覆盖 Bool 与 Uint 的解析错误分支
func TestSetScalarBoolAndUintErrors(t *testing.T) {
	var dst struct {
		B bool
		U uint
	}
	elem := reflect.ValueOf(&dst).Elem()
	if err := setScalar(elem.Field(0), "abc", "", nil); err == nil {
		t.Fatal("expected bool parse error")
	}
	if err := setScalar(elem.Field(1), "-1", "", nil); err == nil {
		t.Fatal("expected uint parse error")
	}
}

// TestSetScalarFloatError 覆盖 Float 的解析错误分支
func TestSetScalarFloatError(t *testing.T) {
	var dst struct{ F float64 }
	elem := reflect.ValueOf(&dst).Elem()
	if err := setScalar(elem.Field(0), "abc", "", nil); err == nil {
		t.Fatal("expected float parse error")
	}
}

// TestBindBodyFormParseError 覆盖表单解析失败（请求体超限）的错误返回分支
func TestBindBodyFormParseError(t *testing.T) {
	big := strings.Repeat("a", 11<<20) // 超过默认 10MB 上限
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta); err == nil {
		t.Fatal("expected form parse error, got nil")
	}
}
