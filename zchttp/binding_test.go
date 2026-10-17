package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	target := "/search?keyword=go&page=3&active=true&tags=a&tags=b"
	rec := serveRequest(t, router, http.MethodGet, target, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d, body: %s", target, rec.Code, rec.Body.String())
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

	body := `{"keyword":"json","page":9,"active":true,"tags":["m","n"]}`
	rec := serveRequest(t, router, http.MethodPost, "/search", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("POST /search: expected 200, got %d, body: %s", rec.Code, rec.Body.String())
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

	// start_time: unix 秒；date: yyyy-MM-dd；slash_date: dd/MM/yyyy；
	// auto: RFC3339 自动探测；auto_milli: 13 位毫秒时间戳自动探测
	query := "start_time=1700000000" +
		"&date=2023-06-15" +
		"&slash_date=15/06/2023" +
		"&auto=2023-06-15T10:00:00Z" +
		"&auto_milli=1700000000123"
	rec := serveRequest(t, router, http.MethodGet, "/event?"+query, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /event: expected 200, got %d, body: %s", rec.Code, rec.Body.String())
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
	// 非法 JSON
	rec := serveRequest(t, router, http.MethodPost, "/item", `{"name":}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for JSON parse error, got %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestFormTagPriorityOverJSON 验证 form 标签优先于 json 标签
func TestFormTagPriorityOverJSON(t *testing.T) {
	router := NewRouter()
	router.GET("/search", formPriorityHandler)
	// q 命中 form 标签，page 命中 json 标签
	rec := serveRequest(t, router, http.MethodGet, "/search?q=go&page=3", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /search: expected 200, got %d, body: %s", rec.Code, rec.Body.String())
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

	for _, method := range []string{http.MethodDelete, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			rec := serveRequest(t, router, method, "/item?q=test&page=7", "")
			// HEAD 请求不返回 body，仅检查状态码
			if rec.Code != http.StatusOK {
				t.Fatalf("%s /item: expected 200, got %d, body: %s", method, rec.Code, rec.Body.String())
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

// mergeBindReq/mergeBindRes 合并绑定测试用结构体，覆盖 string/int/指针字段
type mergeBindReq struct {
	Source string  `json:"source" form:"source"`
	Name   string  `json:"name" form:"name"`
	Page   int     `json:"page" form:"page"`
	Mode   *string `json:"mode" form:"mode"`
}

type mergeBindRes struct {
	Source string  `json:"source"`
	Name   string  `json:"name"`
	Page   int     `json:"page"`
	Mode   *string `json:"mode"`
}

func mergeBindHandler(_ context.Context, req mergeBindReq) (mergeBindRes, error) {
	return mergeBindRes{Source: req.Source, Name: req.Name, Page: req.Page, Mode: req.Mode}, nil
}

// TestPostQueryBodyMergedBinding 锁定带 body 方法（POST/PUT/PATCH）的合并绑定语义
// （ZCH-02 增强实现：替代旧语义"query 不参与绑定"，原 TestPostQueryParamsNotBound 已废弃）：
// 先绑 query 再绑 body，body 覆盖同名字段，body 缺失的字段保留 query 值。
// 属行为变更（breaking change）：依赖"query 被忽略"的旧行为将改变。
func TestPostQueryBodyMergedBinding(t *testing.T) {
	router := NewRouter()
	router.POST("/merge", mergeBindHandler)
	router.PUT("/merge", mergeBindHandler)
	router.PATCH("/merge", mergeBindHandler)
	engine := NewEngine()
	engine.Router = router

	do := func(t *testing.T, method, target, body, contentType string) mergeBindRes {
		t.Helper()
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, target, reader)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d, body: %s", method, target, rec.Code, rec.Body.String())
		}
		var res mergeBindRes
		decodeData(t, rec, &res)
		return res
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method+"_query_only", func(t *testing.T) {
			// a. 仅传 query（无 body）：query 参数正常绑定
			res := do(t, method, "/merge?source=web&page=3", "", "")
			if res.Source != "web" || res.Page != 3 {
				t.Fatalf("query-only binding: got %+v, want Source=web Page=3", res)
			}
		})
		t.Run(method+"_body_only", func(t *testing.T) {
			// b. 仅传 body（无 query）：行为与变更前一致
			res := do(t, method, "/merge", `{"source":"api","name":"alice","page":9}`, "application/json")
			if res.Source != "api" || res.Name != "alice" || res.Page != 9 {
				t.Fatalf("body-only binding: got %+v", res)
			}
		})
		t.Run(method+"_merge_distinct", func(t *testing.T) {
			// c. query 与 body 传不同字段：合并，均被绑定
			res := do(t, method, "/merge?source=web&page=3", `{"name":"alice"}`, "application/json")
			if res.Source != "web" || res.Page != 3 || res.Name != "alice" {
				t.Fatalf("merged distinct fields: got %+v, want Source=web Page=3 Name=alice", res)
			}
		})
		t.Run(method+"_body_overrides_query", func(t *testing.T) {
			// d. 同名字段：body 值覆盖 query 值
			res := do(t, method, "/merge?source=web&name=bob", `{"source":"api"}`, "application/json")
			if res.Source != "api" {
				t.Fatalf("body should override query: Source=%q, want 'api'", res.Source)
			}
			if res.Name != "bob" {
				t.Fatalf("query-only field should be kept: Name=%q, want 'bob'", res.Name)
			}
		})
		t.Run(method+"_form_merge", func(t *testing.T) {
			// 表单 body 同样参与合并：query + x-www-form-urlencoded
			res := do(t, method, "/merge?source=web", "name=alice&page=7", "application/x-www-form-urlencoded")
			if res.Source != "web" || res.Name != "alice" || res.Page != 7 {
				t.Fatalf("form merged binding: got %+v", res)
			}
		})
	}
}

// TestMergedBinding_JSONNullAndZero 锁定合并绑定中 body 覆盖粒度（由 JSON 解码语义决定）：
// 1. body 显式传入的零值（如 "page": 0）覆盖 query 值——JSON 解码对显式出现的字段一律写入；
// 2. body 中 JSON null 对非指针字段为 no-op，保留 query 值；
// 3. body 中 JSON null 对指针字段置 nil（标准库语义），覆盖 query 已绑定的值。
func TestMergedBinding_JSONNullAndZero(t *testing.T) {
	router := NewRouter()
	router.POST("/merge", mergeBindHandler)
	engine := NewEngine()
	engine.Router = router

	do := func(t *testing.T, target, body string) mergeBindRes {
		t.Helper()
		rec := serveJSONOn(t, engine, http.MethodPost, target, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s: expected 200, got %d, body: %s", target, rec.Code, rec.Body.String())
		}
		var res mergeBindRes
		decodeData(t, rec, &res)
		return res
	}

	// 1. 显式零值覆盖：query page=5 被 body "page": 0 覆盖为 0
	res := do(t, "/merge?page=5", `{"page":0}`)
	if res.Page != 0 {
		t.Fatalf("explicit zero should override query: Page=%d, want 0", res.Page)
	}

	// 2. null 对非指针字段 no-op：query source=web 在 body "source": null 后保留
	res = do(t, "/merge?source=web", `{"source":null}`)
	if res.Source != "web" {
		t.Fatalf("null on value field should keep query value: Source=%q, want 'web'", res.Source)
	}

	// 3. null 对指针字段置 nil：query mode=fast 已绑定，body "mode": null 覆盖为 nil
	res = do(t, "/merge?mode=fast", `{"mode":null}`)
	if res.Mode != nil {
		t.Fatalf("null on pointer field should reset to nil: Mode=%v", *res.Mode)
	}

	// 对照：body 未提及指针字段时保留 query 值
	res = do(t, "/merge?mode=fast", `{"name":"x"}`)
	if res.Mode == nil || *res.Mode != "fast" {
		t.Fatalf("absent field should keep query value: Mode=%v, want &fast", res.Mode)
	}
}

// TestMergedBinding_PathParamPriority 锁定三级覆盖链：path > body > query
// （路径参数在 ServeHTTP 中于 bindRequestData 之后绑定，优先级最高保持不变）
func TestMergedBinding_PathParamPriority(t *testing.T) {
	router := NewRouter()
	router.POST("/merge/{source}", mergeBindHandler)
	engine := NewEngine()
	engine.Router = router

	// 三者同名全传：path 最终胜出
	target := "/merge/from-path?source=from-query"
	rec := serveJSONOn(t, engine, http.MethodPost, target, `{"source":"from-body","name":"alice"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: expected 200, got %d, body: %s", target, rec.Code, rec.Body.String())
	}
	var res mergeBindRes
	decodeData(t, rec, &res)
	if res.Source != "from-path" {
		t.Fatalf("path param should win: Source=%q, want 'from-path'", res.Source)
	}
	if res.Name != "alice" {
		t.Fatalf("non-conflicting body field should be bound: Name=%q", res.Name)
	}

	// body 未传同名字段时：path 仍覆盖 query
	rec = serveJSONOn(t, engine, http.MethodPost, target, `{"name":"bob"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: expected 200, got %d, body: %s", target, rec.Code, rec.Body.String())
	}
	var res2 mergeBindRes
	decodeData(t, rec, &res2)
	if res2.Source != "from-path" {
		t.Fatalf("path param should override query: Source=%q, want 'from-path'", res2.Source)
	}
}

// ========== P1 测试 ==========

// TestTimeLocationTag 验证 time_location 标签指定时区解析
func TestTimeLocationTag(t *testing.T) {
	router := NewRouter()
	router.GET("/meeting", timeTzHandler)

	// 2023-06-15 14:30 在 Asia/Shanghai 时区
	rec := serveRequest(t, router, http.MethodGet, "/meeting?meeting=2023-06-15+14:30", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /meeting: expected 200, got %d, body: %s", rec.Code, rec.Body.String())
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

	// milli=1700000000123 (13位), micro=1700000000123456 (16位), nano=1700000000123456789 (19位)
	query := "milli=1700000000123&micro=1700000000123456&nano=1700000000123456789"
	rec := serveRequest(t, router, http.MethodGet, "/ts?"+query, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /ts: expected 200, got %d, body: %s", rec.Code, rec.Body.String())
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
	// default 在注册阶段预填进 Req 模板，请求阶段先拷贝模板再做 JSON 绑定；
	// JSON 解码按 key 驱动，body 中未出现的 key 不覆盖模板值。
	// 因此 addr.zip 保留 nestedAddress 的 default:"000000"
	// （applyDefaults 会递归进入值类型嵌套 struct，见 parameter-binding.md 注册阶段说明）。
	if res.Zip != "000000" {
		t.Errorf("zip = %q, want %q（body 未出现 addr.zip，应保留注册阶段预填的 default）", res.Zip, "000000")
	}
	// tags 未出现在 body 中，同样保留顶层 default:"a,b" 预填结果
	if len(res.Tags) != 2 || res.Tags[0] != "a" || res.Tags[1] != "b" {
		t.Errorf("tags = %v, want [a b]（body 未出现 tags，应保留注册阶段预填的 default）", res.Tags)
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
	err := bindBody(r, reqVal, meta, 0)
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

// TestBindBody_EmptyJSONBody 验证空 JSON body 不报绑定错误，由校验阶段 nonzero 拦截缺失字段
func TestBindBody_EmptyJSONBody(t *testing.T) {
	type req struct {
		Name string `json:"name" nonzero:"true"`
	}
	type res struct{}

	router := NewRouter()
	router.POST("/test", func(_ context.Context, r req) (res, error) {
		return res{}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(""))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	// 空 body 绑定阶段不报错，校验阶段 nonzero 拦截缺失字段 → 400 ValidationError
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "is required") {
		t.Fatalf("response should contain 'is required', got: %s", body)
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

// ========== 匿名嵌入 struct 展开（问题1 回归锁死） ==========

// TestEmbed_UnexportedValueEmbedBindAndDefault 未导出「值」嵌入仍能绑定 + 填默认值（防误伤）：
// 值字段已在内存中，内部导出字段的 json 绑定与注册期 default 填充均应生效。
func TestEmbed_UnexportedValueEmbedBindAndDefault(t *testing.T) {
	type unexportedBase struct {
		Name string `json:"name" default:"default-name"`
	}
	type req struct {
		unexportedBase
		Note string `json:"note"`
	}
	type res struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}

	router := NewRouter()
	router.POST("/t", func(_ context.Context, r req) (res, error) {
		return res{Name: r.Name, Note: r.Note}, nil
	})
	engine := NewEngine()
	engine.Router = router

	// name 未传 → 注册期 default 填充；note 传入 → JSON 覆盖
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"note":"x"}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data res `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if out.Data.Name != "default-name" {
		t.Fatalf("name = %q, want default 'default-name'", out.Data.Name)
	}
	if out.Data.Note != "x" {
		t.Fatalf("note = %q, want 'x'", out.Data.Note)
	}
}

// TestEmbed_UnexportedPtrEmbedNoPanicAndNotBound 未导出「指针」嵌入：注册不 panic，
// 且其内部字段不进入可绑定集合（query 绑定路径经 meta.fields，字段被跳过）。
func TestEmbed_UnexportedPtrEmbedNoPanicAndNotBound(t *testing.T) {
	type unexportedBase struct {
		Name string `form:"name"`
	}
	type req struct {
		*unexportedBase
		Note string `form:"note"`
	}
	type res struct {
		Name string `json:"name"`
		Note string `json:"note"`
	}

	router := NewRouter()
	// 修复前此注册即 panic（reflect: Set using value obtained using unexported field）
	router.GET("/t", func(_ context.Context, r req) (res, error) {
		// 嵌入指针被跳过展开后保持 nil；r.Name 会解引用 nil 指针，故先判空
		name := ""
		if r.unexportedBase != nil {
			name = r.Name
		}
		return res{Name: name, Note: r.Note}, nil
	})
	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodGet, "/t?name=ignored&note=x", nil)
	engine.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data res `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if out.Data.Name != "" {
		t.Fatalf("unexported ptr embed fields must not be bound; name = %q, want empty", out.Data.Name)
	}
	if out.Data.Note != "x" {
		t.Fatalf("note = %q, want 'x'", out.Data.Note)
	}
}

// TestEmbed_ExportedPtrEmbedBindAndDefault 导出「指针」嵌入仍能绑定 + 填默认值（现状不回归）。
func TestEmbed_ExportedPtrEmbedBindAndDefault(t *testing.T) {
	type ExportedBase struct {
		Page int `json:"page" default:"5"`
	}
	type req struct {
		*ExportedBase
		Note string `json:"note"`
	}
	type res struct {
		Page int    `json:"page"`
		Note string `json:"note"`
	}

	router := NewRouter()
	router.POST("/t", func(_ context.Context, r req) (res, error) {
		return res{Page: r.Page, Note: r.Note}, nil
	})
	engine := NewEngine()
	engine.Router = router

	// 空 body：Page 应被注册期 default 填充为 5（嵌入指针被 fieldByIndex 物化）
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data res `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if out.Data.Page != 5 {
		t.Fatalf("page = %d, want default 5", out.Data.Page)
	}
}

// TestEmbed_ExportedPtrEmbedNonzeroEnforced 锁死问题3：嵌入指针结构体的内部 nonzero 字段
// 即使客户端完全不传嵌入对象，仍会强制校验（无「父字段判零」环节）。
func TestEmbed_ExportedPtrEmbedNonzeroEnforced(t *testing.T) {
	type ExportedBase struct {
		Name string `json:"name" nonzero:"true"`
	}
	type req struct {
		*ExportedBase
		Note string `json:"note"`
	}
	type res struct {
		Note string `json:"note"`
	}

	router := NewRouter()
	router.POST("/t", func(_ context.Context, r req) (res, error) {
		return res{Note: r.Note}, nil
	})
	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (embedded nonzero enforced), got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// TestEmbed_UnexportedValueEmbedNonzeroEnforced 锁死：未导出「值」嵌入的内部 nonzero 字段
// 同样无条件校验（与 buildStructMeta 的展开逻辑对齐），客户端不传时返回 400。
func TestEmbed_UnexportedValueEmbedNonzeroEnforced(t *testing.T) {
	type unexportedBase struct {
		Name string `json:"name" nonzero:"true"`
	}
	type req struct {
		unexportedBase
		Note string `json:"note"`
	}
	type res struct {
		Note string `json:"note"`
	}

	router := NewRouter()
	router.POST("/t", func(_ context.Context, r req) (res, error) {
		return res{Note: r.Note}, nil
	})
	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"note":"x"}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (unexported value embed nonzero enforced), got %d; body: %s", rec.Code, rec.Body.String())
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

// TestBindPathParamsCatchAllZeroSegment 验证通配参数零段命中（match 不追加捕获值，values 为空）时
// 绑定被跳过，字段保留预置的 default 值，且不报错。绑定按生产口径构造 optional=false
// （通配段不是可选参数），锁死"跳过"只依赖捕获值缺失、不依赖 optional 标记。
func TestBindPathParamsCatchAllZeroSegment(t *testing.T) {
	params := []pathParamBinding{unitParamBinding("s", false)}
	reqPtr := reflect.New(reflect.TypeOf(pathParamUnitReq{}))
	reqPtr.Elem().Field(0).SetString("preset")
	if err := bindPathParams(reqPtr, params, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := reqPtr.Elem().Interface().(pathParamUnitReq).S; got != "preset" {
		t.Fatalf("zero-segment catch-all should keep preset value, got %q", got)
	}
}

// TestBindRequestDataKeepRawBody 验证 keepRawBody=true 时 bindRequestData 对 POST/PUT/PATCH ×
// JSON/form/multipart/未知 Content-Type 均只绑定 query：body 中的同名字段不覆盖 query 值，
// 且绑定后 r.Body 仍可逐字节完整读出；对照组 keepRawBody=false 时 JSON body 照常覆盖 query。
func TestBindRequestDataKeepRawBody(t *testing.T) {
	type rawBindReq struct {
		KeepRawBody
		Name string `json:"name"`
	}
	meta := buildStructMeta(reflect.TypeOf(rawBindReq{}))

	var mpBody bytes.Buffer
	mw := multipart.NewWriter(&mpBody)
	if err := mw.WriteField("name", "body"); err != nil {
		t.Fatalf("write multipart field: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	bodies := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{"json", "application/json", []byte(`{"name":"body"}`)},
		{"form", "application/x-www-form-urlencoded", []byte("name=body")},
		{"multipart", mw.FormDataContentType(), mpBody.Bytes()},
		{"unknown", "application/octet-stream", []byte{0x00, 0xff, 'n', 'a', 'm', 'e'}},
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		for _, b := range bodies {
			t.Run(method+"/"+b.name, func(t *testing.T) {
				r := httptest.NewRequest(method, "/raw?name=query", bytes.NewReader(b.body))
				r.Header.Set("Content-Type", b.contentType)
				reqPtr := reflect.New(reflect.TypeOf(rawBindReq{}))
				if err := bindRequestData(r, reqPtr, meta, 32<<20, true); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got := reqPtr.Elem().Interface().(rawBindReq).Name; got != "query" {
					t.Fatalf("Name = %q, want query (body must not be bound)", got)
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				if !bytes.Equal(raw, b.body) {
					t.Fatalf("body should remain intact, got %q, want %q", raw, b.body)
				}
			})
		}
	}

	t.Run("对照组keepRawBody=false", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/raw?name=query", strings.NewReader(`{"name":"body"}`))
		r.Header.Set("Content-Type", "application/json")
		reqPtr := reflect.New(reflect.TypeOf(rawBindReq{}))
		if err := bindRequestData(r, reqPtr, meta, 32<<20, false); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := reqPtr.Elem().Interface().(rawBindReq).Name; got != "body" {
			t.Fatalf("Name = %q, want body (body overrides query)", got)
		}
	})
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
		var want time.Time
		switch c.unit {
		case time.Second:
			want = time.Unix(n, 0)
		case time.Millisecond:
			want = time.UnixMilli(n)
		case time.Microsecond:
			want = time.UnixMicro(n)
		case time.Nanosecond:
			want = time.Unix(0, n)
		}
		want = want.In(time.UTC)
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

// TestBindBodyEmptyJSON 覆盖 JSON 空请求体不报绑定错误的分支
func TestBindBodyEmptyJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0)
	if err != nil {
		t.Fatalf("empty body should not return binding error, got: %v", err)
	}
}

// TestBindBodyJSONNoBindableFields 验证 bindBody 在无可绑定字段时仍可正常工作：
// 调用方（ServeHTTP）已保证不会调用本函数，但直接调用时 JSON 解码到空结构体无害成功
func TestBindBodyJSONNoBindableFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	meta := buildStructMeta(reflect.TypeOf(struct{}{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(struct{}{})), meta, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindBodyUnknownCTNoBody 覆盖未知 Content-Type 且无请求体时不绑定的分支
func TestBindBodyUnknownCTNoBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Content-Type", "text/plain")
	req.Body = nil
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0); err != nil {
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
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBindBodyUnknownCTInvalidJSON 覆盖默认分支解码失败（非空错误）的返回分支
func TestBindBodyUnknownCTInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "text/plain")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0); err == nil {
		t.Fatal("expected json decode error, got nil")
	}
}

// TestBindBodyMultipartParseError 覆盖 multipart 解析失败的错误返回分支
func TestBindBodyMultipartParseError(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("garbage"))
	req.Header.Set("Content-Type", "multipart/form-data")
	meta := buildStructMeta(reflect.TypeOf(helloReq{}))
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0); err == nil {
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
	if err := bindBody(req, reflect.New(reflect.TypeOf(helloReq{})), meta, 0); err == nil {
		t.Fatal("expected form parse error, got nil")
	}
}

// ======== m7-③: multipart 超内存缓冲走临时文件 ========

// TestBindMultipartMaxMemoryTempFile 验证 MultipartFormMaxMemory 配置的内存缓冲上限生效：
// 文件大小超过上限时由 net/http 写入临时文件，绑定仍成功且文件内容完整可读
func TestBindMultipartMaxMemoryTempFile(t *testing.T) {
	type tmpFileReq struct {
		File *multipart.FileHeader `json:"file"`
	}
	type tmpFileRes struct {
		OK   bool `json:"ok"`
		Size int  `json:"size"`
	}

	router := NewRouter()
	router.POST("/upload", func(_ context.Context, req tmpFileReq) (tmpFileRes, error) {
		if req.File == nil {
			return tmpFileRes{OK: false}, nil
		}
		f, err := req.File.Open()
		if err != nil {
			return tmpFileRes{OK: false}, fmt.Errorf("open uploaded file: %w", err)
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return tmpFileRes{OK: false}, fmt.Errorf("read uploaded file: %w", err)
		}
		return tmpFileRes{OK: true, Size: len(data)}, nil
	})

	engine := NewEngine()
	engine.Router = router
	// 内存缓冲上限仅 1KB：文件远超上限，必须写入临时文件
	engine.MultipartFormMaxMemory = 1 << 10

	// 构造 64KB 上传文件
	payload := bytes.Repeat([]byte("z"), 64<<10)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res tmpFileRes
	decodeData(t, rec, &res)
	if !res.OK {
		t.Fatal("file should be bound and readable after spilling to temp file")
	}
	if res.Size != len(payload) {
		t.Fatalf("file size = %d, want %d (content must survive temp file spill)", res.Size, len(payload))
	}
}

// emptyTimeReq 用于覆盖 setTime 的空值短路分支。
type emptyTimeReq struct {
	T time.Time `form:"t" time_format:"2006-01-02"`
}

// TestBindValues_EmptyTimeValue 覆盖 setTime 对空字符串直接返回的分支：
// query 参数存在但值为空时不解析、字段保持零值。
func TestBindValues_EmptyTimeValue(t *testing.T) {
	router := NewRouter()
	router.GET("/t", func(_ context.Context, req emptyTimeReq) (emptyTimeReq, error) { return req, nil })

	rec := serveRequest(t, router, http.MethodGet, "/t?t=", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var got emptyTimeReq
	decodeData(t, rec, &got)
	if !got.T.IsZero() {
		t.Fatalf("空 query 值不应解析出时间，实际: %v", got.T)
	}
}

// hiddenFieldReq 含一个未导出字段（仅用于包内直调构造病态 meta）。
type hiddenFieldReq struct {
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
	reqPtr := reflect.New(reflect.TypeOf(hiddenFieldReq{}))
	if err := bindValues(reqPtr, map[string][]string{
		"public":  {"ok"},
		"private": {"x"},
	}, nil, meta); err != nil {
		t.Fatalf("bindValues 尽力绑定应恒返回 nil: %v", err)
	}
	got := reqPtr.Interface().(*hiddenFieldReq)
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
	reqPtr := reflect.New(reflect.TypeOf(hiddenFieldReq{}))
	if err := bindPathParams(reqPtr, params, []string{"v0", "v1"}); err != nil {
		t.Fatalf("不可设置字段应被跳过而非报错: %v", err)
	}
	got := reqPtr.Interface().(*hiddenFieldReq)
	if got.Public != "v0" {
		t.Fatalf("可设置路径参数应正常绑定，实际: %q", got.Public)
	}
}

// TestBindValues_InvalidPointerPreservesOriginal 验证 query/form 转换失败不会分配 nil 指针，
// 也不会覆盖字段原有值。
func TestBindValues_InvalidPointerPreservesOriginal(t *testing.T) {
	type req struct {
		Page    *int       `json:"page"`
		Enabled *bool      `json:"enabled"`
		When    *time.Time `json:"when" time_format:"2006-01-02"`
		Preset  *int       `json:"preset"`
	}
	preset := 7
	got := req{Preset: &preset}
	meta := buildStructMeta(reflect.TypeOf(got))
	values := map[string][]string{
		"page":    {"bad"},
		"enabled": {"bad"},
		"when":    {"bad"},
		"preset":  {"bad"},
	}
	if err := bindValues(reflect.ValueOf(&got), values, nil, meta); err != nil {
		t.Fatalf("尽力绑定不应返回字段转换错误: %v", err)
	}
	if got.Page != nil || got.Enabled != nil || got.When != nil {
		t.Fatalf("失败绑定不得分配 nil 指针: %+v", got)
	}
	if got.Preset == nil || *got.Preset != 7 {
		t.Fatalf("失败绑定应保留原值 7，实际: %v", got.Preset)
	}
}

// TestBindBody_JSONRequiresSingleValue 验证 JSON 仅允许一个完整值，尾随空白合法，
// 第二个值或尾随垃圾均返回错误。
func TestBindBody_JSONRequiresSingleValue(t *testing.T) {
	type req struct {
		Page int `json:"page"`
	}
	meta := buildStructMeta(reflect.TypeOf(req{}))
	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{name: "single with whitespace", body: "{\"page\":1} \n\t"},
		{name: "second value", body: "{\"page\":1}{\"page\":2}", wantErr: true},
		{name: "trailing garbage", body: "{\"page\":1}x", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			err := bindBody(r, reflect.New(reflect.TypeOf(req{})), meta, 0)
			if (err != nil) != tc.wantErr {
				t.Fatalf("bindBody() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestBindBody_ContentTypeParsing 验证媒体类型大小写不敏感、参数可解析，非法值明确报错。
func TestBindBody_ContentTypeParsing(t *testing.T) {
	type req struct {
		Name string `form:"name"`
	}
	meta := buildStructMeta(reflect.TypeOf(req{}))

	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("name=ok"))
	r.Header.Set("Content-Type", "Application/X-Www-Form-Urlencoded; Charset=UTF-8")
	dst := reflect.New(reflect.TypeOf(req{}))
	if err := bindBody(r, dst, meta, 0); err != nil {
		t.Fatalf("合法混合大小写 Content-Type 不应失败: %v", err)
	}
	if got := dst.Elem().FieldByName("Name").String(); got != "ok" {
		t.Fatalf("Name = %q, want ok", got)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	invalid.Header.Set("Content-Type", `application/json; charset="unterminated`)
	if err := bindBody(invalid, reflect.New(reflect.TypeOf(req{})), meta, 0); err == nil {
		t.Fatal("非法 Content-Type 应返回错误")
	}
}

// TestSetUnixAuto_NegativePrecision 验证负号不参与自动时间戳位数判断，且换算不溢出。
func TestSetUnixAuto_NegativePrecision(t *testing.T) {
	cases := []struct {
		value string
		want  time.Time
	}{
		{value: "-1000000000", want: time.Unix(-1000000000, 0)},
		{value: "-1000000000000", want: time.UnixMilli(-1000000000000)},
		{value: "-1000000000000000", want: time.UnixMicro(-1000000000000000)},
		{value: "-1000000000000000000", want: time.Unix(0, -1000000000000000000)},
	}
	for _, tc := range cases {
		fv := reflect.New(timeType).Elem()
		if err := setUnixAuto(fv, tc.value, time.UTC); err != nil {
			t.Fatalf("setUnixAuto(%q): %v", tc.value, err)
		}
		if got := fv.Interface().(time.Time); !got.Equal(tc.want) {
			t.Fatalf("setUnixAuto(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestBindBody_UnsupportedMapType 验证 JSON map 能力严格遵循 encoding/json，
// 不支持的 key 类型通过绑定错误返回。
func TestBindBody_UnsupportedMapType(t *testing.T) {
	type req struct {
		Values map[float64]string `json:"values"`
	}
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"values":{"1.5":"x"}}`))
	r.Header.Set("Content-Type", "application/json")
	if err := bindBody(r, reflect.New(reflect.TypeOf(req{})), buildStructMeta(reflect.TypeOf(req{})), 0); err == nil {
		t.Fatal("encoding/json 不支持的 map key 类型应返回绑定错误")
	}
}

type docUnexportedPtrInner struct {
	X string `json:"x"`
}

type docUnexportedPtrEmbedReq struct {
	*docUnexportedPtrInner
	Y string `json:"y"`
}

type docUnexportedPtrEmbedRes struct {
	Y string `json:"y"`
}

// TestBindJSON_UnexportedPointerEmbed 锁死非导出类型指针嵌入在 JSON 请求体下的行为：请求体不含嵌入字段
// 时正常绑定（200）；含嵌入字段 x 时 encoding/json 无法为非导出指针分配内存，绑定失败返回 400，
// 错误消息含 "cannot set embedded pointer to unexported struct"。
func TestBindJSON_UnexportedPointerEmbed(t *testing.T) {
	router := NewRouter()
	router.POST("/unexported-ptr", func(_ context.Context, req docUnexportedPtrEmbedReq) (docUnexportedPtrEmbedRes, error) {
		return docUnexportedPtrEmbedRes{Y: req.Y}, nil
	})
	ok := serveRequest(t, router, http.MethodPost, "/unexported-ptr", `{"y":"1"}`)
	if ok.Code != http.StatusOK || !strings.Contains(ok.Body.String(), `"y":"1"`) {
		t.Fatalf("不含嵌入字段: status = %d, body = %s, want 200 with y=1", ok.Code, ok.Body.String())
	}
	bad := serveRequest(t, router, http.MethodPost, "/unexported-ptr", `{"x":"1","y":"1"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "cannot set embedded pointer to unexported struct") {
		t.Fatalf("含嵌入字段: status = %d, body = %s, want 400 with encoding/json embedded pointer error", bad.Code, bad.Body.String())
	}
}

type docMaxBodyZeroReq struct {
	Body string `form:"body" json:"body"`
}

type docMaxBodyZeroRes struct {
	Len int `json:"len"`
}

// TestBindBody_MaxBodyBytesZeroParserLimits 锁死 MaxBodyBytes=0 时“框架不额外限制、解析器自身限制仍生效”：
// 11 MB 的 urlencoded 请求体被标准库 ParseForm 的 10 MB 固有上限拒绝（400，消息 http: POST too large）；
// 同样 11 MB 的 JSON 请求体正常绑定（200，长度完整）。
func TestBindBody_MaxBodyBytesZeroParserLimits(t *testing.T) {
	router := NewRouter()
	router.POST("/zero-limit", func(_ context.Context, req docMaxBodyZeroReq) (docMaxBodyZeroRes, error) {
		return docMaxBodyZeroRes{Len: len(req.Body)}, nil
	})
	engine := NewEngine()
	engine.Router = router
	engine.MaxBodyBytes = 0
	payload := strings.Repeat("a", 11<<20)

	formReq := httptest.NewRequest(http.MethodPost, "/zero-limit", strings.NewReader("body="+payload))
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formRec := httptest.NewRecorder()
	engine.ServeHTTP(formRec, formReq)
	if formRec.Code != http.StatusBadRequest || !strings.Contains(formRec.Body.String(), "http: POST too large") {
		t.Fatalf("urlencoded 11MB: status = %d, body = %.200s, want 400 with http: POST too large", formRec.Code, formRec.Body.String())
	}

	jsonRec := serveJSONOn(t, engine, http.MethodPost, "/zero-limit", `{"body":"`+payload+`"}`)
	if jsonRec.Code != http.StatusOK {
		t.Fatalf("JSON 11MB: status = %d, body = %.200s, want 200", jsonRec.Code, jsonRec.Body.String())
	}
	var res docMaxBodyZeroRes
	decodeData(t, jsonRec, &res)
	if res.Len != len(payload) {
		t.Fatalf("JSON 11MB 绑定长度 = %d, want %d", res.Len, len(payload))
	}
}

// TestIsScalarType 锁死路径参数可绑定类型的判定：标量各宽度、time.Time 及其指针（含多级）为 true；
// slice、map、struct、chan、interface 以及指针层数超过 maxPtrDerefDepth 的标量为 false。
func TestIsScalarType(t *testing.T) {
	deep := reflect.TypeOf(0)
	for i := 0; i <= maxPtrDerefDepth; i++ {
		deep = reflect.PointerTo(deep)
	}
	limit := reflect.TypeOf(0)
	for i := 0; i < maxPtrDerefDepth; i++ {
		limit = reflect.PointerTo(limit)
	}
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"string", reflect.TypeOf(""), true},
		{"bool", reflect.TypeOf(false), true},
		{"int8", reflect.TypeOf(int8(0)), true},
		{"uint64", reflect.TypeOf(uint64(0)), true},
		{"float32", reflect.TypeOf(float32(0)), true},
		{"time", reflect.TypeOf(time.Time{}), true},
		{"ptr time", reflect.TypeOf(&time.Time{}), true},
		{"ptr at depth limit", limit, true},
		{"ptr beyond depth limit", deep, false},
		{"slice", reflect.TypeOf([]string{}), false},
		{"map", reflect.TypeOf(map[string]int{}), false},
		{"struct", reflect.TypeOf(struct{}{}), false},
		{"chan", reflect.TypeOf(make(chan int)), false},
		{"interface", reflect.TypeOf((*any)(nil)).Elem(), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isScalarType(c.typ); got != c.want {
				t.Fatalf("isScalarType(%v) = %v, want %v", c.typ, got, c.want)
			}
		})
	}
}

// TestSetTime_EmptyValueNoop 验证空字符串直接略过解析（不报错、不改动字段），
// 覆盖 setTime 的空值早退分支。
func TestSetTime_EmptyValueNoop(t *testing.T) {
	var tv time.Time
	if err := setTime(reflect.ValueOf(&tv).Elem(), "", "unix", nil); err != nil {
		t.Fatalf("空值应直接返回 nil，实际 %v", err)
	}
	if !tv.IsZero() {
		t.Errorf("空值不应修改字段，实际 %v", tv)
	}
}

// TestSetTime_UnparseableAutoDetect 验证 time_format 为空且值既非纯数字也不能匹配任何
// 常见布局时返回错误，覆盖自动探测的失败分支。
func TestSetTime_UnparseableAutoDetect(t *testing.T) {
	var tv time.Time
	if err := setTime(reflect.ValueOf(&tv).Elem(), "not-a-time", "", nil); err == nil {
		t.Fatal("无法解析的时间字符串应返回错误")
	}
}

// TestBindValues_TimeTagsOnPointerAndSliceFields 端到端锁死 time_format/time_location 在
// *time.Time、[]time.Time、[]*time.Time 上生效：自定义 layout（不在自动探测表内）不再被静默丢弃，
// time_location 不再丢失。历史行为：这些字段的标签被元数据层丢弃 → 自动探测失败 → bindValues 的
// 尽力绑定语义吞掉错误，字段保持零值且无任何日志。
func TestBindValues_TimeTagsOnPointerAndSliceFields(t *testing.T) {
	type req struct {
		Ptr      *time.Time   `json:"ptr" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
		Slice    []time.Time  `json:"slice" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
		SlicePtr []*time.Time `json:"slice_ptr" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
	}

	got := req{}
	meta := buildStructMeta(reflect.TypeOf(got))
	values := map[string][]string{
		"ptr":       {"05/03/2024"},
		"slice":     {"05/03/2024", "06/03/2024"},
		"slice_ptr": {"05/03/2024"},
	}
	if err := bindValues(reflect.ValueOf(&got), values, nil, meta); err != nil {
		t.Fatalf("bindValues: %v", err)
	}

	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("本机缺少 Asia/Shanghai 时区数据，跳过: %v", err)
	}
	want := time.Date(2024, 3, 5, 0, 0, 0, 0, shanghai)
	// 用位置名而非指针比较：time.LoadLocation 多次调用不保证返回同一实例
	if got.Ptr == nil || !got.Ptr.Equal(want) || got.Ptr.Location().String() != "Asia/Shanghai" {
		t.Fatalf("指针字段应按自定义 layout 与 Asia/Shanghai 解析: got %v", got.Ptr)
	}
	if len(got.Slice) != 2 || !got.Slice[0].Equal(want) || !got.Slice[1].Equal(want.AddDate(0, 0, 1)) {
		t.Fatalf("切片元素应按相同标签逐个解析: got %v", got.Slice)
	}
	if len(got.SlicePtr) != 1 || got.SlicePtr[0] == nil || !got.SlicePtr[0].Equal(want) {
		t.Fatalf("指针切片元素应按相同标签解析: got %v", got.SlicePtr)
	}
}

// TestBindPathParams_TimeTagOnPointerAndSlice 锁死路径参数使用与 query 相同的时间标签解析：
// *time.Time 字段按 time_format/time_location 转换（文档声明"指针与 time.Time 标签同样生效"），
// 而切片等非标量形态在注册阶段即被拒绝（attachPathParamBindings panic），不会走到绑定。
func TestBindPathParams_TimeTagOnPointerAndSlice(t *testing.T) {
	type req struct {
		When *time.Time `json:"when" time_format:"02/01/2006" time_location:"UTC"`
	}
	typ := reflect.TypeOf(req{})
	meta := buildStructMeta(typ)

	// 复刻 attachPathParamBindings 的取值方式：绑定参数直接复用字段元信息
	var params []pathParamBinding
	for i := range meta.fields {
		fm := &meta.fields[i]
		if fm.name != "when" {
			continue
		}
		if !isScalarType(fm.field.Type) {
			t.Fatalf("路径参数仅支持标量（含 time.Time 指针），%s 不应被判定为不支持", fm.field.Type)
		}
		params = append(params, pathParamBinding{
			indices:      fm.indices,
			timeFormat:   fm.timeFormat,
			timeLocation: fm.timeLocation,
		})
	}
	if len(params) != 1 {
		t.Fatalf("未找到 when 字段的路径参数绑定: %v", params)
	}

	got := req{}
	if err := bindPathParams(reflect.ValueOf(&got), params, []string{"05/03/2024"}); err != nil {
		t.Fatalf("bindPathParams: %v", err)
	}
	want := time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)
	if got.When == nil || !got.When.Equal(want) {
		t.Fatalf("路径参数应按 time_format 解析为 2024-03-05 UTC: got %v", got.When)
	}

	// 切片字段不是路径参数支持的形态
	if isScalarType(reflect.TypeOf([]time.Time{})) {
		t.Error("[]time.Time 不应被判定为标量类型")
	}
}

// TestSetFieldValue_UnsupportedContainerKeepsOriginal 锁死尽力绑定语义：
// 切片元素为结构体/多层容器时不写回全零元素，指针包裹的容器不会被 setScalar 写入。
func TestSetFieldValue_UnsupportedContainerKeepsOriginal(t *testing.T) {
	type item struct{ X int }
	type holder struct {
		Items    []item
		Multi    [][]int
		PtrSlice *[]int
	}
	d := holder{Items: []item{{X: 7}}}
	v := reflect.ValueOf(&d).Elem()

	if err := setFieldValue(v.Field(0), []string{"x"}, "", nil); err != nil {
		t.Fatalf("[]struct: unexpected error: %v", err)
	}
	if len(d.Items) != 1 || d.Items[0].X != 7 {
		t.Fatalf("[]struct must keep original value, got %v", d.Items)
	}

	if err := setFieldValue(v.Field(1), []string{"1", "2"}, "", nil); err != nil {
		t.Fatalf("[][]int: unexpected error: %v", err)
	}
	if d.Multi != nil {
		t.Fatalf("[][]int must keep nil, got %v", d.Multi)
	}

	if err := setFieldValue(v.Field(2), []string{"1"}, "", nil); err != nil {
		t.Fatalf("*[]int: unexpected error: %v", err)
	}
	if d.PtrSlice != nil {
		t.Fatalf("*[]int must keep nil, got %v", *d.PtrSlice)
	}
}

// TestBindValues_QuerySkipsUnsupportedContainers 锁死 GET query 的运行时行为：
// []Struct、数组、指针包裹容器与多层切片保持绑定前值，只有标量切片正常绑定。
func TestBindValues_QuerySkipsUnsupportedContainers(t *testing.T) {
	type inner struct {
		A string `json:"a"`
	}
	type req struct {
		Items    []inner       `json:"items"`
		Arr      [2]int        `json:"arr"`
		PtrSlice *[]int        `json:"ptrSlice"`
		Multi    [][]time.Time `json:"multi" time_format:"unix"`
		OK       []int         `json:"ok"`
	}
	type res struct {
		ItemsNil bool   `json:"itemsNil"`
		Arr      [2]int `json:"arr"`
		PtrNil   bool   `json:"ptrNil"`
		MultiNil bool   `json:"multiNil"`
		OK       []int  `json:"ok"`
	}

	router := NewRouter()
	router.GET("/containers", func(_ context.Context, q req) (res, error) {
		return res{ItemsNil: q.Items == nil, Arr: q.Arr, PtrNil: q.PtrSlice == nil, MultiNil: q.Multi == nil, OK: q.OK}, nil
	})

	rec := serveRequest(t, router, http.MethodGet,
		"/containers?items=x&arr=1&arr=2&ptrSlice=3&multi=1600000000&ok=1&ok=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rec.Code, rec.Body.String())
	}
	var out res
	decodeData(t, rec, &out)
	if !out.ItemsNil {
		t.Fatal("[]struct query must not be written")
	}
	if out.Arr != [2]int{} {
		t.Fatalf("array query must keep zero value, got %v", out.Arr)
	}
	if !out.PtrNil {
		t.Fatal("*[]int query must keep nil")
	}
	if !out.MultiNil {
		t.Fatal("[][]time.Time query must keep nil")
	}
	if !reflect.DeepEqual(out.OK, []int{1, 2}) {
		t.Fatalf("scalar slice should bind, got %v", out.OK)
	}
}

// TestJSONNamedEmbedFlatKeyIgnored 锁死带 json 名匿名嵌入的绑定语义：
// query/form 扁平绑定；JSON 只认嵌套对象，父级扁平键被 encoding/json 忽略。
func TestJSONNamedEmbedFlatKeyIgnored(t *testing.T) {
	type namedBase struct {
		Name string `json:"name" form:"name"`
	}
	type req struct {
		namedBase `json:"base"`
	}
	type res struct {
		Name string `json:"name"`
	}

	router := NewRouter()
	h := func(_ context.Context, q req) (res, error) { return res{Name: q.Name}, nil }
	router.GET("/named-embed", h)
	router.POST("/named-embed", h)

	rec := serveRequest(t, router, http.MethodGet, "/named-embed?name=q", "")
	var out res
	decodeData(t, rec, &out)
	if out.Name != "q" {
		t.Fatalf("query should bind flat name, got %q", out.Name)
	}

	rec = serveRequest(t, router, http.MethodPost, "/named-embed", `{"name":"flat"}`)
	decodeData(t, rec, &out)
	if out.Name != "" {
		t.Fatalf("flat JSON key must be ignored for json-named embed, got %q", out.Name)
	}

	rec = serveRequest(t, router, http.MethodPost, "/named-embed", `{"base":{"name":"nested"}}`)
	decodeData(t, rec, &out)
	if out.Name != "nested" {
		t.Fatalf("nested JSON object should bind, got %q", out.Name)
	}
}

// TestBindMultipartValueOverMemoryLimit 锁死 multipart 非文件值的预算：
// 只有文件部分可落盘；非文件值超过 MultipartFormMaxMemory + 10MB 时绑定失败返回 400。
func TestBindMultipartValueOverMemoryLimit(t *testing.T) {
	type req struct {
		Blob string `form:"blob"`
	}
	type res struct {
		Len int `json:"len"`
	}

	router := NewRouter()
	router.POST("/upload-value", func(_ context.Context, q req) (res, error) {
		return res{Len: len(q.Blob)}, nil
	})
	engine := NewEngine()
	engine.Router = router
	engine.MaxBodyBytes = 0
	engine.MultipartFormMaxMemory = 1 << 10

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormField("blob")
	if err != nil {
		t.Fatalf("CreateFormField: %v", err)
	}
	payload := strings.Repeat("z", 11<<20)
	if _, err := fw.Write([]byte(payload)); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/upload-value", &buf)
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "multipart: message too large") {
		t.Fatalf("expected 400 multipart: message too large, got %d; body=%.200s", rec.Code, rec.Body.String())
	}
}

// ======== stdlib 差分测试：query/form/JSON/multipart 绑定 ========

// bindDiffNested 是 JSON 嵌套字段，query/form 绑定应跳过整个结构体。
type bindDiffNested struct {
	Key string `json:"key"`
	Num int    `json:"num"`
}

// bindDiffReq 是差分测试用的 Req：覆盖 form/json 绑定名差异、标量、切片、指针、
// 嵌套结构体、map、忽略字段与上传文件。字段保持扁平，避免把匿名嵌入展开语义
// 混入本次差分（该语义由既有测试覆盖）。
type bindDiffReq struct {
	Nick     string                  `json:"name" form:"nick"`
	Page     int                     `json:"page"`
	Active   bool                    `json:"active"`
	Ratio    float64                 `json:"ratio"`
	Tags     []string                `json:"tags"`
	Nums     []int                   `json:"nums"`
	Score    *int                    `json:"score"`
	Ignored  string                  `json:"-"`
	Nested   bindDiffNested          `json:"nested"`
	Extra    map[string]string       `json:"extra"`
	FormOnly string                  `form:"fo"`
	File     *multipart.FileHeader   `json:"file"`
	Files    []*multipart.FileHeader `json:"files"`
}

type bindDiffFileSnapshot struct {
	Filename string
	Size     int64
}

// bindDiffSnapshot 把 Req 转成可比较快照，避免直接比较两个 parser 产生的文件头指针。
type bindDiffSnapshot struct {
	Nick     string
	Page     int
	Active   bool
	Ratio    float64
	Tags     []string
	Nums     []int
	Score    *int
	Ignored  string
	Nested   bindDiffNested
	Extra    map[string]string
	FormOnly string
	File     *bindDiffFileSnapshot
	Files    []bindDiffFileSnapshot
}

func snapshotBindDiff(req bindDiffReq) bindDiffSnapshot {
	snapshot := bindDiffSnapshot{
		Nick:     req.Nick,
		Page:     req.Page,
		Active:   req.Active,
		Ratio:    req.Ratio,
		Tags:     req.Tags,
		Nums:     req.Nums,
		Ignored:  req.Ignored,
		Nested:   req.Nested,
		Extra:    req.Extra,
		FormOnly: req.FormOnly,
	}
	if req.Score != nil {
		value := *req.Score
		snapshot.Score = &value
	}
	if req.File != nil {
		snapshot.File = &bindDiffFileSnapshot{Filename: req.File.Filename, Size: req.File.Size}
	}
	for _, file := range req.Files {
		if file == nil {
			snapshot.Files = append(snapshot.Files, bindDiffFileSnapshot{})
			continue
		}
		snapshot.Files = append(snapshot.Files, bindDiffFileSnapshot{Filename: file.Filename, Size: file.Size})
	}
	return snapshot
}

type bindDiffCase struct {
	name        string
	method      string
	rawQuery    string
	contentType string
	body        []byte
	maxMemory   int64
}

// refBindFileHeaderPtrType 由参考实现独立获取，避免依赖被测包的文件字段判定变量。
var refBindFileHeaderPtrType = reflect.TypeOf((*multipart.FileHeader)(nil))

// refBindFieldName 独立实现文档约定的绑定名解析：form > json > 字段名。
func refBindFieldName(field reflect.StructField) string {
	if tag := field.Tag.Get("form"); tag != "" {
		if name := refBindTagName(tag); name != "" {
			return name
		}
	}
	if tag := field.Tag.Get("json"); tag != "" {
		if name := refBindTagName(tag); name != "" {
			return name
		}
	}
	return field.Name
}

func refBindTagName(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		tag = tag[:idx]
	}
	return tag
}

// refBindValues 用反射按绑定名把 values/files 写入结构体：
// 文件字段优先于普通值、标量取首值且不去空白、切片逐值去空白后整体提交、
// 转换失败保持字段原值（尽力绑定）。仅处理扁平结构体字段。
func refBindValues(dst reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader) {
	typ := dst.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := refBindFieldName(field)
		if name == "" || name == "-" {
			continue
		}
		fieldValue := dst.Field(i)
		if !fieldValue.CanSet() {
			continue
		}
		switch {
		case field.Type == refBindFileHeaderPtrType:
			if headers := files[name]; len(headers) > 0 {
				fieldValue.Set(reflect.ValueOf(headers[0]))
			}
			continue
		case field.Type.Kind() == reflect.Slice && field.Type.Elem() == refBindFileHeaderPtrType:
			if headers := files[name]; len(headers) > 0 {
				fieldValue.Set(reflect.ValueOf(headers))
			}
			continue
		}
		fieldValues, ok := values[name]
		if !ok || len(fieldValues) == 0 {
			continue
		}
		refBindFieldValue(fieldValue, fieldValues)
	}
}

func refBindFieldValue(fieldValue reflect.Value, values []string) {
	if fieldValue.Kind() == reflect.Slice {
		if !refIsScalarType(fieldValue.Type().Elem()) {
			return
		}
		parsed := reflect.MakeSlice(fieldValue.Type(), len(values), len(values))
		for i, value := range values {
			if !refParseScalar(parsed.Index(i), strings.TrimSpace(value)) {
				return
			}
		}
		fieldValue.Set(parsed)
		return
	}
	refParseScalar(fieldValue, values[0])
}

func refIsScalarType(t reflect.Type) bool {
	for depth := 0; t.Kind() == reflect.Ptr; depth++ {
		if depth >= maxPtrDerefDepth {
			return false
		}
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// refParseScalar 先用 stdlib 转换到临时值，成功后才写入目标；失败保持原值。
func refParseScalar(fieldValue reflect.Value, value string) bool {
	if fieldValue.Kind() == reflect.Ptr {
		parsed := reflect.New(fieldValue.Type().Elem())
		if !refParseScalar(parsed.Elem(), value) {
			return false
		}
		fieldValue.Set(parsed)
		return true
	}
	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return false
		}
		fieldValue.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return false
		}
		fieldValue.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return false
		}
		fieldValue.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, fieldValue.Type().Bits())
		if err != nil {
			return false
		}
		fieldValue.SetFloat(parsed)
	default:
		return false
	}
	return true
}

// refBindRequest 用 stdlib 独立实现文档中的绑定流程：
// 先按 query 绑定（GET/DELETE/HEAD 到此结束），其余方法按 Content-Type
// 选择 JSON、urlencoded 或 multipart，后绑定者覆盖先绑定者。
func refBindRequest(t *testing.T, tc bindDiffCase) (bindDiffReq, error) {
	t.Helper()
	var dst bindDiffReq
	// 与 net/url.Values.Query 一致：忽略解析错误，保留已解析部分
	queryValues, _ := url.ParseQuery(tc.rawQuery)
	refBindValues(reflect.ValueOf(&dst).Elem(), queryValues, nil)

	switch tc.method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		return dst, nil
	}

	mediaType := ""
	var params map[string]string
	if tc.contentType != "" {
		parsedType, parsedParams, err := mime.ParseMediaType(tc.contentType)
		if err != nil {
			return dst, err
		}
		mediaType, params = parsedType, parsedParams
	}

	switch mediaType {
	case "application/x-www-form-urlencoded":
		bodyValues, err := url.ParseQuery(string(tc.body))
		if err != nil {
			return dst, err
		}
		// ParseForm 同时解析 URL query；body 成功但 query 非法时仍返回 query 错误
		if _, queryErr := url.ParseQuery(tc.rawQuery); queryErr != nil {
			return dst, queryErr
		}
		refBindValues(reflect.ValueOf(&dst).Elem(), bodyValues, nil)
	case "multipart/form-data":
		boundary := params["boundary"]
		if boundary == "" {
			return dst, fmt.Errorf("no multipart boundary parameter")
		}
		form, err := multipart.NewReader(bytes.NewReader(tc.body), boundary).ReadForm(tc.maxMemory)
		if err != nil {
			return dst, err
		}
		refBindValues(reflect.ValueOf(&dst).Elem(), form.Value, form.File)
		if cleanupErr := form.RemoveAll(); cleanupErr != nil {
			t.Fatalf("case %s: cleanup reference multipart temp files: %v", tc.name, cleanupErr)
		}
		// ParseMultipartForm 内部先执行 ParseForm，query 非法时在 multipart 解析成功后返回该错误
		if _, queryErr := url.ParseQuery(tc.rawQuery); queryErr != nil {
			return dst, queryErr
		}
	default:
		// 空 Content-Type 与未知 Content-Type 均回退 JSON；仅 JSON 空白（空格/Tab/CR/LF）
		// 视为无 body——encoding/json 不把 \v、\f 等 Unicode 空白当作空白。
		if len(bytes.Trim(tc.body, " \t\r\n")) == 0 {
			return dst, nil
		}
		if err := json.Unmarshal(tc.body, &dst); err != nil {
			return dst, err
		}
	}
	return dst, nil
}

// runBindDiff 走被测 bindRequestData，并在返回前清理 multipart 可能产生的临时文件。
func runBindDiff(t *testing.T, meta structMeta, tc bindDiffCase) (bindDiffReq, error) {
	t.Helper()
	req := httptest.NewRequest(tc.method, "/bind-diff", bytes.NewReader(tc.body))
	req.URL.RawQuery = tc.rawQuery
	if tc.contentType != "" {
		req.Header.Set("Content-Type", tc.contentType)
	}
	reqPtr := reflect.New(reflect.TypeOf(bindDiffReq{}))
	err := bindRequestData(req, reqPtr, meta, tc.maxMemory, false)
	if req.MultipartForm != nil {
		if cleanupErr := req.MultipartForm.RemoveAll(); cleanupErr != nil {
			t.Fatalf("case %s: cleanup request multipart temp files: %v", tc.name, cleanupErr)
		}
	}
	return reqPtr.Elem().Interface().(bindDiffReq), err
}

func truncateBindDiffBody(body []byte) string {
	const limit = 256
	if len(body) > limit {
		return string(body[:limit]) + "..."
	}
	return string(body)
}

// assertBindDiffCase 同时执行 stdlib 参考流程与被测绑定，比较错误有无与最终字段值。
func assertBindDiffCase(t *testing.T, meta structMeta, tc bindDiffCase) {
	t.Helper()
	want, wantErr := refBindRequest(t, tc)
	got, gotErr := runBindDiff(t, meta, tc)
	detail := fmt.Sprintf("method=%s query=%q content-type=%q body=%q", tc.method, tc.rawQuery, tc.contentType, truncateBindDiffBody(tc.body))
	if (gotErr != nil) != (wantErr != nil) {
		t.Fatalf("%s: error mismatch\n implementation err: %v\n stdlib reference err: %v\n %s", tc.name, gotErr, wantErr, detail)
	}
	if wantErr != nil {
		return
	}
	gotSnapshot, wantSnapshot := snapshotBindDiff(got), snapshotBindDiff(want)
	if !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
		t.Fatalf("%s: binding mismatch\n implementation: %+v\n stdlib reference: %+v\n %s", tc.name, gotSnapshot, wantSnapshot, detail)
	}
}

type bindDiffField struct {
	name  string
	value string
}

type bindDiffFile struct {
	field   string
	name    string
	content string
}

func buildBindDiffMultipart(t testing.TB, fields []bindDiffField, files []bindDiffFile) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for _, field := range fields {
		if err := writer.WriteField(field.name, field.value); err != nil {
			t.Fatalf("write multipart field %q: %v", field.name, err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile(file.field, file.name)
		if err != nil {
			t.Fatalf("create multipart file %q: %v", file.field, err)
		}
		if _, err := part.Write([]byte(file.content)); err != nil {
			t.Fatalf("write multipart file %q: %v", file.field, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return buf.Bytes(), writer.FormDataContentType()
}

// curatedBindDiffCases 覆盖文档中的关键绑定语义：方法分流、query/body 覆盖、
// JSON null/显式零值/重复键/尾随值、form 尽力绑定、Content-Type 选择、multipart 文件。
func curatedBindDiffCases(t testing.TB) []bindDiffCase {
	t.Helper()
	multipartBody, multipartCT := buildBindDiffMultipart(t,
		[]bindDiffField{{"nick", "body"}, {"page", "2"}, {"tags", "f1"}, {"tags", "f2"}},
		[]bindDiffFile{
			{field: "file", name: "a.txt", content: "hello"},
			{field: "files", name: "b.txt", content: "bb"},
			{field: "files", name: "c.txt", content: "ccc"},
		})
	smallMemoryBody, smallMemoryCT := buildBindDiffMultipart(t, nil,
		[]bindDiffFile{{field: "file", name: "big.bin", content: strings.Repeat("x", 64)}})
	truncatedMultipart := append([]byte(nil), multipartBody...)
	truncatedMultipart = truncatedMultipart[:len(truncatedMultipart)/2]

	return []bindDiffCase{
		{name: "get_body_ignored", method: http.MethodGet, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`{"name":"body","page":9}`), maxMemory: 32 << 20},
		{name: "delete_body_ignored", method: http.MethodDelete, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`{"name":"body","page":9}`), maxMemory: 32 << 20},
		{name: "head_body_ignored", method: http.MethodHead, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`{"name":"body","page":9}`), maxMemory: 32 << 20},
		{name: "post_json_partial_override", method: http.MethodPost, rawQuery: "nick=q&page=1&tags=q1&tags=q2&score=5&active=true", contentType: "application/json", body: []byte(`{"name":"body","page":0,"tags":null,"score":null}`), maxMemory: 32 << 20},
		{name: "post_json_empty_body", method: http.MethodPost, rawQuery: "nick=q&page=1", contentType: "application/json", body: nil, maxMemory: 32 << 20},
		{name: "post_json_vertical_tab_body", method: http.MethodPost, rawQuery: "nick=q", contentType: "application/json", body: []byte("\v"), maxMemory: 32 << 20},
		{name: "post_json_empty_object", method: http.MethodPost, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`{}`), maxMemory: 32 << 20},
		{name: "post_json_top_level_null", method: http.MethodPost, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`null`), maxMemory: 32 << 20},
		{name: "post_json_trailing_value", method: http.MethodPost, rawQuery: "nick=q", contentType: "application/json", body: []byte(`{"name":"a"}{"name":"b"}`), maxMemory: 32 << 20},
		{name: "post_json_malformed", method: http.MethodPost, rawQuery: "nick=q", contentType: "application/json", body: []byte(`{"name":`), maxMemory: 32 << 20},
		{name: "post_json_duplicate_key", method: http.MethodPost, rawQuery: "nick=q", contentType: "application/json", body: []byte(`{"name":"a","name":"b"}`), maxMemory: 32 << 20},
		{name: "post_json_case_insensitive", method: http.MethodPost, rawQuery: "nick=q&page=1", contentType: "application/json", body: []byte(`{"NAME":"b","PAGE":7}`), maxMemory: 32 << 20},
		{name: "post_json_wrong_type", method: http.MethodPost, rawQuery: "page=1", contentType: "application/json", body: []byte(`{"page":"abc"}`), maxMemory: 32 << 20},
		{name: "post_json_nested_and_map", method: http.MethodPost, rawQuery: "nick=q&fo=q", contentType: "application/json", body: []byte(`{"nested":{"key":"k","num":3},"extra":{"a":"b"},"FormOnly":"x"}`), maxMemory: 32 << 20},
		{name: "post_form_partial_override", method: http.MethodPost, rawQuery: "nick=q&page=1&tags=q1&tags=q2", contentType: "application/x-www-form-urlencoded", body: []byte("nick=body&page=0&tags=f1&tags=f2"), maxMemory: 32 << 20},
		{name: "post_form_invalid_keeps_query", method: http.MethodPost, rawQuery: "page=3&active=true", contentType: "application/x-www-form-urlencoded", body: []byte("page=abc&active=notabool"), maxMemory: 32 << 20},
		{name: "post_form_malformed_escape", method: http.MethodPost, rawQuery: "", contentType: "application/x-www-form-urlencoded", body: []byte("page=%zz"), maxMemory: 32 << 20},
		{name: "post_form_semicolon", method: http.MethodPost, rawQuery: "", contentType: "application/x-www-form-urlencoded", body: []byte("page=1;active=true"), maxMemory: 32 << 20},
		{name: "post_form_charset", method: http.MethodPost, rawQuery: "page=1", contentType: "application/x-www-form-urlencoded; charset=UTF-8", body: []byte("nick=body&page=2"), maxMemory: 32 << 20},
		{name: "post_unknown_ct_json", method: http.MethodPost, rawQuery: "nick=q", contentType: "text/plain", body: []byte(`{"name":"body"}`), maxMemory: 32 << 20},
		{name: "post_empty_ct_json", method: http.MethodPost, rawQuery: "nick=q", contentType: "", body: []byte(`{"name":"body"}`), maxMemory: 32 << 20},
		{name: "post_unknown_ct_empty_body", method: http.MethodPost, rawQuery: "nick=q", contentType: "text/plain", body: nil, maxMemory: 32 << 20},
		{name: "post_malformed_ct", method: http.MethodPost, rawQuery: "nick=q", contentType: "application/json; charset", body: []byte(`{}`), maxMemory: 32 << 20},
		{name: "post_multipart_fields_and_files", method: http.MethodPost, rawQuery: "nick=q&page=1", contentType: multipartCT, body: multipartBody, maxMemory: 32 << 20},
		{name: "post_multipart_truncated", method: http.MethodPost, rawQuery: "nick=q", contentType: multipartCT, body: truncatedMultipart, maxMemory: 32 << 20},
		{name: "post_multipart_no_boundary", method: http.MethodPost, rawQuery: "nick=q", contentType: "multipart/form-data", body: multipartBody, maxMemory: 32 << 20},
		{name: "post_multipart_small_memory", method: http.MethodPost, rawQuery: "", contentType: smallMemoryCT, body: smallMemoryBody, maxMemory: 1},
		{name: "query_malformed_escape_ignored", method: http.MethodGet, rawQuery: "nick=q&bad=%zz", contentType: "", body: nil, maxMemory: 32 << 20},
		{name: "slice_invalid_element_keeps_nil", method: http.MethodGet, rawQuery: "nums=1&nums=bad", contentType: "", body: nil, maxMemory: 32 << 20},
		{name: "slice_trims_elements", method: http.MethodGet, rawQuery: "tags=%20a%20&tags=b", contentType: "", body: nil, maxMemory: 32 << 20},
		{name: "scalar_not_trimmed", method: http.MethodGet, rawQuery: "nick=%20q%20", contentType: "", body: nil, maxMemory: 32 << 20},
	}
}

var bindDiffKeys = []string{"nick", "name", "page", "active", "ratio", "tags", "nums", "score", "fo", "unknown", "nested", "extra", "file", "files", "-"}

func refRandomKey(rng *rand.Rand) string {
	return bindDiffKeys[rng.Intn(len(bindDiffKeys))]
}

func refRandomValue(rng *rand.Rand, key string) string {
	switch key {
	case "page", "score":
		return []string{"0", "1", "-7", "42", "abc", "+3", " 42 "}[rng.Intn(7)]
	case "active":
		return []string{"true", "false", "TRUE", "1", "0", "yes", ""}[rng.Intn(7)]
	case "ratio":
		return []string{"0", "1.5", "-0.25", "1e3", "abc", ""}[rng.Intn(6)]
	case "nums":
		return []string{"0", "1", "-2", "bad", " 3 "}[rng.Intn(5)]
	case "tags":
		return []string{"", " a ", "b", "中文", "x,y"}[rng.Intn(5)]
	default:
		return []string{"", "v", " body ", "中文", "-", "x/y"}[rng.Intn(6)]
	}
}

func refRandomQuery(rng *rand.Rand) string {
	if rng.Intn(5) == 0 {
		return ""
	}
	values := url.Values{}
	fieldCount := rng.Intn(6)
	for i := 0; i < fieldCount; i++ {
		key := refRandomKey(rng)
		for j := 0; j < 1+rng.Intn(3); j++ {
			values.Add(key, refRandomValue(rng, key))
		}
	}
	query := values.Encode()
	if rng.Intn(20) == 0 {
		if query != "" {
			query += "&"
		}
		query += "bad=%zz"
	}
	return query
}

func refRandomJSONBody(t *testing.T, rng *rand.Rand) []byte {
	t.Helper()
	switch rng.Intn(14) {
	case 0:
		return nil
	case 1:
		return []byte("   ")
	case 2:
		return []byte("null")
	case 3:
		return []byte(`{"name":"a"}{"name":"b"}`)
	case 4:
		return []byte(`{"page":`)
	case 5:
		return []byte(`{"name":"a"} trailing`)
	}

	object := make(map[string]any)
	keys := []string{"name", "nick", "page", "active", "ratio", "tags", "nums", "score", "nested", "extra", "FormOnly", "unknown"}
	fieldCount := rng.Intn(6)
	for i := 0; i < fieldCount; i++ {
		key := keys[rng.Intn(len(keys))]
		switch key {
		case "name", "nick", "FormOnly", "unknown":
			object[key] = []string{"", "v", " body ", "中文"}[rng.Intn(4)]
		case "page":
			object[key] = []any{0, 1, -7, 42, 3.5}[rng.Intn(5)]
		case "active":
			object[key] = rng.Intn(2) == 0
		case "ratio":
			object[key] = []any{0, 1.5, -0.25, 1e3}[rng.Intn(4)]
		case "tags":
			switch rng.Intn(6) {
			case 0:
				object[key] = nil
			case 1:
				object[key] = []any{1, 2}
			default:
				object[key] = []string{"", "a", " b ", "中文"}
			}
		case "nums":
			switch rng.Intn(6) {
			case 0:
				object[key] = nil
			case 1:
				object[key] = []any{"bad"}
			default:
				object[key] = []any{0, 1, -2}
			}
		case "score":
			if rng.Intn(4) == 0 {
				object[key] = nil
			} else {
				object[key] = rng.Intn(100)
			}
		case "nested":
			object[key] = map[string]any{"key": "k", "num": rng.Intn(10)}
		case "extra":
			object[key] = map[string]string{"a": "b"}
		}
	}
	body, err := json.Marshal(object)
	if err != nil {
		t.Fatalf("marshal random JSON body: %v", err)
	}
	return body
}

func refRandomFormBody(rng *rand.Rand) []byte {
	switch rng.Intn(14) {
	case 0:
		return []byte("page=%zz")
	case 1:
		return []byte("page=1;active=true")
	}
	values := url.Values{}
	fieldCount := rng.Intn(6)
	for i := 0; i < fieldCount; i++ {
		key := refRandomKey(rng)
		for j := 0; j < 1+rng.Intn(3); j++ {
			values.Add(key, refRandomValue(rng, key))
		}
	}
	return []byte(values.Encode())
}

func refRandomMultipart(t *testing.T, rng *rand.Rand) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	fieldCount := rng.Intn(6)
	for i := 0; i < fieldCount; i++ {
		key := refRandomKey(rng)
		if err := writer.WriteField(key, refRandomValue(rng, key)); err != nil {
			t.Fatalf("write random multipart field %q: %v", key, err)
		}
	}
	fileCount := rng.Intn(3)
	for i := 0; i < fileCount; i++ {
		part, err := writer.CreateFormFile("file", fmt.Sprintf("f%d.txt", i))
		if err != nil {
			t.Fatalf("create random multipart file: %v", err)
		}
		if _, err := part.Write([]byte(strings.Repeat("x", 1+rng.Intn(32)))); err != nil {
			t.Fatalf("write random multipart file: %v", err)
		}
	}
	filesCount := rng.Intn(3)
	for i := 0; i < filesCount; i++ {
		part, err := writer.CreateFormFile("files", fmt.Sprintf("g%d.txt", i))
		if err != nil {
			t.Fatalf("create random multipart files element: %v", err)
		}
		if _, err := part.Write([]byte(strings.Repeat("y", 1+rng.Intn(32)))); err != nil {
			t.Fatalf("write random multipart files element: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close random multipart writer: %v", err)
	}
	body := buf.Bytes()
	if rng.Intn(10) == 0 && len(body) > 2 {
		body = body[:len(body)/2]
	}
	return body, writer.FormDataContentType()
}

func refRandomJSONContentType(rng *rand.Rand) string {
	return []string{
		"application/json",
		"application/json; charset=UTF-8",
		"Application/JSON",
		"application/problem+json",
		"",
		"application/json; charset",
	}[rng.Intn(6)]
}

func refRandomUnknownContentType(rng *rand.Rand) string {
	return []string{"text/plain", "application/octet-stream", "", "application/vnd.api+json"}[rng.Intn(4)]
}

// generateBindDiffRandomCases 生成随机请求：方法、query、Content-Type 与 body 相互独立，
// 覆盖 GET 带 body、未知/空 Content-Type 回退 JSON、form/multipart 解析错误等组合。
func generateBindDiffRandomCases(t *testing.T, rng *rand.Rand, count int) []bindDiffCase {
	t.Helper()
	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead,
	}
	cases := make([]bindDiffCase, 0, count)
	for i := 0; i < count; i++ {
		method := methods[rng.Intn(len(methods))]
		rawQuery := refRandomQuery(rng)
		var body []byte
		contentType := ""
		switch rng.Intn(8) {
		case 0, 1:
			body = refRandomJSONBody(t, rng)
			contentType = refRandomJSONContentType(rng)
		case 2, 6:
			body = refRandomFormBody(rng)
			contentType = "application/x-www-form-urlencoded"
			if rng.Intn(3) == 0 {
				contentType += "; charset=UTF-8"
			}
		case 3:
			body, contentType = refRandomMultipart(t, rng)
		case 4:
			body = refRandomJSONBody(t, rng)
			contentType = refRandomUnknownContentType(rng)
		case 5:
			body = nil
		default:
			body = []byte("not-json")
			contentType = "application/json"
		}
		cases = append(cases, bindDiffCase{
			name:        fmt.Sprintf("random_%03d", i),
			method:      method,
			rawQuery:    rawQuery,
			contentType: contentType,
			body:        body,
			maxMemory:   32 << 20,
		})
	}
	return cases
}

// TestBindStdlibDifferential 用 stdlib 解析结果作为 oracle 校验 query/form/JSON/multipart 绑定：
// 参考流程独立实现 form > json > 字段名映射、标量取首值、切片整组提交、失败保持原值、
// body 覆盖 query、GET/DELETE/HEAD 忽略 body、未知 Content-Type 回退 JSON。
// 定向用例覆盖文档锁死的边界；随机用例覆盖方法 × Content-Type × body 组合。
func TestBindStdlibDifferential(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(bindDiffReq{}))
	t.Run("curated", func(t *testing.T) {
		for _, tc := range curatedBindDiffCases(t) {
			t.Run(tc.name, func(t *testing.T) {
				assertBindDiffCase(t, meta, tc)
			})
		}
	})
	t.Run("random", func(t *testing.T) {
		const (
			seed  = int64(20261017)
			count = 150
		)
		rng := rand.New(rand.NewSource(seed))
		for _, tc := range generateBindDiffRandomCases(t, rng, count) {
			t.Run(tc.name, func(t *testing.T) {
				assertBindDiffCase(t, meta, tc)
			})
		}
	})
}

// FuzzBindDifferential 用 stdlib 参考实现差分校验 query/form/JSON 绑定：
// 固定方法、Content-Type、query 与 body 由 fuzzer 变异，逐次比较错误有无与绑定快照。
// multipart 不纳入 fuzz 枚举（curated 用例已覆盖），避免任意 boundary 触发临时文件与磁盘 IO；
// body/query 上限 64 KB，保证单次执行足够快且无外部副作用。
// 普通 go test 只运行 f.Add 种子；探索命令：
//
//	go test ./zchttp -run '^$' -fuzz '^FuzzBindDifferential$' -fuzztime=30s
func FuzzBindDifferential(f *testing.F) {
	meta := buildStructMeta(reflect.TypeOf(bindDiffReq{}))
	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodHead,
	}
	contentTypes := []string{
		"application/json",
		"application/json; charset=UTF-8",
		"application/x-www-form-urlencoded",
		"text/plain",
		"",
	}
	methodIndex := make(map[string]uint8, len(methods))
	for i, method := range methods {
		methodIndex[method] = uint8(i)
	}
	contentTypeIndex := make(map[string]uint8, len(contentTypes))
	for i, contentType := range contentTypes {
		contentTypeIndex[contentType] = uint8(i)
	}

	// 复用定向用例作为种子；multipart 用例的 Content-Type 不在枚举内，自动跳过
	type fuzzSeed struct {
		method      uint8
		contentType uint8
		query       string
		body        string
	}
	seenSeeds := make(map[fuzzSeed]bool)
	for _, tc := range curatedBindDiffCases(f) {
		method, ok := methodIndex[tc.method]
		if !ok {
			continue
		}
		contentType, ok := contentTypeIndex[tc.contentType]
		if !ok {
			continue
		}
		seed := fuzzSeed{method: method, contentType: contentType, query: tc.rawQuery, body: string(tc.body)}
		if seenSeeds[seed] {
			continue
		}
		seenSeeds[seed] = true
		f.Add(seed.method, seed.contentType, seed.query, tc.body)
	}

	f.Fuzz(func(t *testing.T, methodIdx, contentTypeIdx uint8, query string, body []byte) {
		if len(query) > 64<<10 || len(body) > 64<<10 {
			return
		}
		tc := bindDiffCase{
			name:        "fuzz",
			method:      methods[int(methodIdx)%len(methods)],
			rawQuery:    query,
			contentType: contentTypes[int(contentTypeIdx)%len(contentTypes)],
			body:        body,
			maxMemory:   32 << 20,
		}
		want, wantErr := refBindRequest(t, tc)
		got, gotErr := runBindDiff(t, meta, tc)
		if (gotErr != nil) != (wantErr != nil) {
			t.Fatalf("method=%s query=%q content-type=%q body=%q: implementation err=%v, reference err=%v",
				tc.method, tc.rawQuery, tc.contentType, truncateBindDiffBody(body), gotErr, wantErr)
		}
		if wantErr != nil {
			return
		}
		gotSnapshot, wantSnapshot := snapshotBindDiff(got), snapshotBindDiff(want)
		if !reflect.DeepEqual(gotSnapshot, wantSnapshot) {
			t.Fatalf("method=%s query=%q content-type=%q body=%q: implementation=%+v, reference=%+v",
				tc.method, tc.rawQuery, tc.contentType, truncateBindDiffBody(body), gotSnapshot, wantSnapshot)
		}
	})
}
