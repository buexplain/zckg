package zchttp_test

// docs/openapi.md、docs/routing.md、docs/parameter-binding.md 示例代码的编译级校验
// （对应“文档与代码一致”中的示例可编译性）。
// 手工收录文档示例，不自动读取 Markdown；文档示例变更须同步本文件。
// 使用外部测试包（zchttp_test）以调用方视角校验 API 写法，不伪造任何测试存根。
// 各 TestDocExamples_* 在编译之外额外运行示例，核对文档描述的产物或行为。

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/buexplain/zckg/zchttp"
)

// deleteUserReq 对应 docs/openapi.md「deprecated 标记 / 操作级」示例。
type deleteUserReq struct {
	zchttp.OpenAPIMeta `tags:"User" summary:"删除用户" deprecated:"true" description:"已废弃，请使用 DELETE /v2/users/{id}"`
	ID                 int64 `json:"id" nonzero:"true"`
}

// updateUserReq 对应 docs/openapi.md「deprecated 标记 / 字段级」示例。
type updateUserReq struct {
	zchttp.OpenAPIMeta `tags:"User" summary:"更新用户"`
	Name               string `json:"name" nonzero:"true"`
	OldNick            string `json:"old_nick" deprecated:"true" description:"已废弃，请使用 name"`
}

// legacyFields 对应 docs/openapi.md「deprecated 标记 / 废弃原因」示例：
// 源码中用 \x60 表示 Markdown 行内代码的反引号，由 reflect.StructTag.Get 还原。
type legacyFields struct {
	OldField string `json:"old_field" deprecated:"true" description:"~~已废弃~~ 请使用 \x60new_field\x60，本字段将在 v3 移除"`
}

type docExampleRes struct {
	OK bool `json:"ok"`
}

// registerDocExamples 按文档示例注册三条路由（DELETE 示例同时覆盖路径参数绑定）。
func registerDocExamples(r *zchttp.Router) {
	r.DELETE("/v2/users/{id}", func(_ context.Context, _ deleteUserReq) (docExampleRes, error) {
		return docExampleRes{}, nil
	})
	r.POST("/v2/users", func(_ context.Context, _ updateUserReq) (docExampleRes, error) {
		return docExampleRes{}, nil
	})
	r.POST("/v2/legacy", func(_ context.Context, _ legacyFields) (docExampleRes, error) {
		return docExampleRes{}, nil
	})
}

// TestDocExamples_Deprecated 编译并运行文档示例，核对文档所描述的产物：
// 操作级示例输出 deprecated: true 且保留 summary/description；
// 字段级示例仅在带标签的字段上输出 deprecated: true；
// 废弃原因示例的 \x60 经 reflect.StructTag.Get 还原为反引号并进入 schema description。
func TestDocExamples_Deprecated(t *testing.T) {
	const wantLegacyDescription = "~~已废弃~~ 请使用 `new_field`，本字段将在 v3 移除"

	r := zchttp.NewRouter()
	registerDocExamples(r)
	doc := zchttp.GenerateOpenAPI(r, zchttp.OpenAPIInfo{Title: "Doc", Version: "1.0.0"})
	paths := doc["paths"].(map[string]any)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	op := paths["/v2/users/{id}"].(map[string]any)["delete"].(map[string]any)
	if v, ok := op["deprecated"]; !ok || v != true {
		t.Errorf("操作级示例应输出 deprecated: true，实际: %v", op)
	}
	if op["summary"] != "删除用户" || op["description"] != "已废弃，请使用 DELETE /v2/users/{id}" {
		t.Errorf("操作级示例的 summary/description 应原样保留，实际: %v", op)
	}

	props := docRequestBodyProps(t, schemas, paths["/v2/users"].(map[string]any)["post"].(map[string]any))
	for _, tc := range []struct {
		field string
		want  bool
	}{{"old_nick", true}, {"name", false}} {
		node := props[tc.field].(map[string]any)
		v, ok := node["deprecated"]
		if ok != tc.want || (tc.want && v != true) {
			t.Errorf("字段级示例 %s: deprecated 存在=%t 值=%v, want 存在=%t", tc.field, ok, v, tc.want)
		}
	}

	legacy := docRequestBodyProps(t, schemas, paths["/v2/legacy"].(map[string]any)["post"].(map[string]any))
	oldField := legacy["old_field"].(map[string]any)
	if oldField["description"] != wantLegacyDescription {
		t.Errorf("废弃原因示例的 description = %q, want %q", oldField["description"], wantLegacyDescription)
	}
	if v, ok := oldField["deprecated"]; !ok || v != true {
		t.Errorf("废弃原因示例的 old_field 应输出 deprecated: true，实际: %v", oldField)
	}
	f, ok := reflect.TypeOf(legacyFields{}).FieldByName("OldField")
	if !ok {
		t.Fatal("legacyFields.OldField 缺失")
	}
	if got := f.Tag.Get("description"); got != wantLegacyDescription {
		t.Errorf("StructTag.Get 未把 \\x60 还原为反引号：%q", got)
	}
}

// docRequestBodyProps 从 operation 的 requestBody 引用链解析出 Req 的 properties，
// 逐段核对 $ref 指向的组件确实存在（避免只按名称猜测 schema）。
func docRequestBodyProps(t *testing.T, schemas, op map[string]any) map[string]any {
	t.Helper()
	const prefix = "#/components/schemas/"
	body, ok := op["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("operation 缺少 requestBody，实际: %v", op)
	}
	jsonContent, ok := body["content"].(map[string]any)["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody 缺少 application/json 内容，实际: %v", body)
	}
	ref, ok := jsonContent["schema"].(map[string]any)
	if !ok {
		t.Fatalf("application/json 缺少 schema，实际: %v", jsonContent)
	}
	refVal, ok := ref["$ref"].(string)
	if !ok || len(refVal) <= len(prefix) || refVal[:len(prefix)] != prefix {
		t.Fatalf("requestBody schema 应为组件引用，实际: %v", ref)
	}
	obj, ok := schemas[refVal[len(prefix):]].(map[string]any)
	if !ok {
		t.Fatalf("引用 %q 缺少目标组件", refVal)
	}
	props, ok := obj["properties"].(map[string]any)
	if !ok {
		t.Fatalf("组件 %q 缺少 properties，实际: %v", refVal, obj)
	}
	return props
}

// ===== routing.md「使用场景：反向代理」示例（逐字收录） =====

type ProxyReq struct {
	zchttp.KeepRawBody
	Rest string `json:"rest"`
}

type ProxyRes struct{}

var upstream, _ = url.Parse("http://dify-api:5001")

// 全局单例：连接池随 Transport 复用，禁止每请求新建
var proxyTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 256
	t.MaxIdleConnsPerHost = 128 // 默认 2，单上游高并发时须调大
	return t
}()

// 复用 32 KB 拷贝缓冲
type bufferPool struct{ p sync.Pool }

func (b *bufferPool) Get() []byte {
	if v, ok := b.p.Get().(*[]byte); ok {
		return *v
	}
	return make([]byte, 32<<10)
}

func (b *bufferPool) Put(buf []byte) { b.p.Put(&buf) }

var proxy = &httputil.ReverseProxy{
	Transport:  proxyTransport,
	BufferPool: &bufferPool{},
	Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(upstream)
		// 转发用原始转义路径去前缀，避免 Rest（已解码）中的 %2F/%3F 语义被改写
		pr.Out.URL.RawPath = strings.TrimPrefix(pr.In.URL.EscapedPath(), "/dify")
		pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/dify")
	},
}

func difyProxy(ctx context.Context, req ProxyReq) (ProxyRes, error) {
	// 校验用 Rest（已解码，%2e%2e 已还原为 ..），拒绝穿越与非规范路径
	if req.Rest != "" && path.Clean("/"+req.Rest) != "/"+req.Rest {
		return ProxyRes{}, zchttp.NewBindingError(errors.New("invalid proxy path"))
	}
	r, _ := zchttp.RequestFromContext(ctx)
	w, _ := zchttp.ResponseWriterFromContext(ctx)
	// 已写入响应：默认回调检测 IsResponseWritten 后不再输出；包装器保留 Flusher，SSE 可用
	proxy.ServeHTTP(w, r)
	return ProxyRes{}, nil
}

// registerProxyDocExample 收录文档中的注册片段（文档里 r、authMiddleware 为上下文变量）。
func registerProxyDocExample(r *zchttp.Router, authMiddleware zchttp.MiddlewareHandler) {
	g := r.Group("/dify", authMiddleware)
	g.Any("/{rest...}", difyProxy)
}

// ===== routing.md「使用场景：静态文件服务」示例（逐字收录） =====

type StaticReq struct {
	File string `json:"file"`
}

type StaticRes struct{}

var assets = os.DirFS("./public") // 或 embed.FS（无 sendfile、无 Last-Modified，需自设缓存头）

func serveStatic(ctx context.Context, req StaticReq) (StaticRes, error) {
	r, _ := zchttp.RequestFromContext(ctx)
	w, _ := zchttp.ResponseWriterFromContext(ctx)
	name := req.File
	if name == "" {
		name = "index.html"
	}
	// name 必须为相对路径（无前导 /）：fs.ValidPath 拒绝 .. 与非规范路径，
	// ServeFileFS 另对 r.URL.Path 中的 .. 拒绝，两道检查叠加
	http.ServeFileFS(w, r, assets, name)
	return StaticRes{}, nil
}

// registerStaticDocExample 收录文档中的注册片段（文档里 r 为上下文变量）。
func registerStaticDocExample(r *zchttp.Router) {
	static := r.Group("/static") // 不挂重型中间件
	static.GET("/{file...}", serveStatic)
	static.HEAD("/{file...}", serveStatic)
}

// ===== parameter-binding.md「2.2 保留原始请求体（KeepRawBody）」示例（逐字收录） =====

type WebhookReq struct {
	// 声明：请求体原样保留，不解析
	zchttp.KeepRawBody
	// 仍从 query 绑定，nonzero 照常校验
	Sig string `json:"sig" nonzero:"true"`
}

type WebhookRes struct {
	OK bool `json:"ok"`
}

var webhookSecret = []byte("secret")

func webhook(ctx context.Context, req WebhookReq) (WebhookRes, error) {
	r, _ := zchttp.RequestFromContext(ctx)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// 含超出 MaxBodyBytes 的 *http.MaxBytesError，包装为 BindingError 返回 400
		return WebhookRes{}, zchttp.NewBindingError(err)
	}
	mac := hmac.New(sha256.New, webhookSecret)
	mac.Write(body)
	if !hmac.Equal([]byte(req.Sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return WebhookRes{}, zchttp.NewBindingError(errors.New("invalid signature"))
	}
	return WebhookRes{OK: true}, nil
}

// registerWebhookDocExample 收录文档中的注册片段（文档里 r 为上下文变量）。
func registerWebhookDocExample(r *zchttp.Router) {
	r.POST("/webhook", webhook)
}

// TestDocExamples_CatchAllProxy 编译并运行 routing.md 反向代理示例（不连接上游）：
// Any 为九个受支持的方法注册 KeepRawBody + 通配尾段且均经过分组中间件；Rest 解码为含 .. 的路径时
// 返回 400；Rewrite 以 EscapedPath 去前缀，%2F 原始转义保留且请求指向 upstream；BufferPool 提供 32 KB 缓冲。
func TestDocExamples_CatchAllProxy(t *testing.T) {
	authCalls := 0
	auth := func(_ context.Context, w http.ResponseWriter, r *http.Request, next zchttp.NextFunc) error {
		authCalls++
		return next(w, r)
	}
	e := zchttp.NewEngine()
	registerProxyDocExample(e.Router, auth)

	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch,
		http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace,
	} {
		t.Run(method, func(t *testing.T) {
			before := authCalls
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(method, "/dify/%2e%2e/etc/passwd", strings.NewReader(`{"a":1}`)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("穿越路径应返回 400，实际 %d，body=%q", rec.Code, rec.Body.String())
			}
			if authCalls != before+1 {
				t.Fatalf("分组中间件应执行一次，实际 %d 次", authCalls-before)
			}
		})
	}

	in := httptest.NewRequest(http.MethodPost, "/dify/v1/a%2Fb?x=1", nil)
	out := in.Clone(context.Background())
	proxy.Rewrite(&httputil.ProxyRequest{In: in, Out: out})
	if out.URL.Scheme != "http" || out.URL.Host != "dify-api:5001" {
		t.Errorf("Rewrite 应指向 upstream，实际 scheme=%q host=%q", out.URL.Scheme, out.URL.Host)
	}
	if got := out.URL.EscapedPath(); got != "/v1/a%2Fb" {
		t.Errorf("转发路径应保留 %%2F 原始转义，实际 %q", got)
	}
	if out.URL.Path != "/v1/a/b" || out.URL.RawQuery != "x=1" {
		t.Errorf("转发 Path/RawQuery = %q/%q, want /v1/a/b / x=1", out.URL.Path, out.URL.RawQuery)
	}
	if buf := proxy.BufferPool.Get(); len(buf) != 32<<10 {
		t.Errorf("BufferPool 缓冲长度 = %d, want %d", len(buf), 32<<10)
	}
}

// TestDocExamples_CatchAllStatic 编译并运行 routing.md 静态文件示例：assets 为 os.DirFS("./public")，
// 按当前目录解析，故用 t.Chdir 切到临时目录（因此不可并行）。锁死：零段命中回退 index.html；
// 多级路径返回文件内容；HEAD 无响应体；%2e%2e 穿越请求被拒绝且不泄漏 public 之外的文件内容。
func TestDocExamples_CatchAllStatic(t *testing.T) {
	root := t.TempDir()
	writeDocFile(t, filepath.Join(root, "public", "index.html"), "home-page")
	writeDocFile(t, filepath.Join(root, "public", "css", "app.css"), "body{}")
	writeDocFile(t, filepath.Join(root, "secret.txt"), "top-secret")
	t.Chdir(root)

	e := zchttp.NewEngine()
	registerStaticDocExample(e.Router)

	cases := []struct {
		name, method, target string
		wantCode             int
		wantBody             string
	}{
		{"零段命中回退index", http.MethodGet, "/static", http.StatusOK, "home-page"},
		{"零段命中带尾斜杠", http.MethodGet, "/static/", http.StatusOK, "home-page"},
		{"多级路径", http.MethodGet, "/static/css/app.css", http.StatusOK, "body{}"},
		{"HEAD无响应体", http.MethodHead, "/static/css/app.css", http.StatusOK, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.target, nil))
			if rec.Code != tc.wantCode || rec.Body.String() != tc.wantBody {
				t.Fatalf("%s %s = %d %q, want %d %q", tc.method, tc.target, rec.Code, rec.Body.String(), tc.wantCode, tc.wantBody)
			}
		})
	}

	t.Run("穿越被拒绝", func(t *testing.T) {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/%2e%2e/secret.txt", nil))
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "top-secret") {
			t.Fatalf("穿越请求应被拒绝且不泄漏内容，实际 %d %q", rec.Code, rec.Body.String())
		}
	})
}

// TestDocExamples_KeepRawBody 编译并运行 parameter-binding.md KeepRawBody 示例：
// 正确签名返回 200 且 data.ok=true（证明 handler 读到未被绑定消费的完整原始请求体）；
// 签名错误、缺少 nonzero 的 sig、请求体超出 MaxBodyBytes（读取失败包装为 BindingError）均返回 400。
func TestDocExamples_KeepRawBody(t *testing.T) {
	const body = `{"event":"paid","amount":100}`
	mac := hmac.New(sha256.New, webhookSecret)
	mac.Write([]byte(body))
	goodSig := hex.EncodeToString(mac.Sum(nil))

	cases := []struct {
		name     string
		maxBody  int64
		target   string
		wantCode int
	}{
		{"正确签名", 0, "/webhook?sig=" + goodSig, http.StatusOK},
		{"签名错误", 0, "/webhook?sig=bad", http.StatusBadRequest},
		{"缺少sig", 0, "/webhook", http.StatusBadRequest},
		{"超出MaxBodyBytes", 8, "/webhook?sig=" + goodSig, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := zchttp.NewEngine()
			if tc.maxBody > 0 {
				e.MaxBodyBytes = tc.maxBody
			}
			registerWebhookDocExample(e.Router)
			req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("状态码 = %d, want %d，body=%q", rec.Code, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode != http.StatusOK {
				return
			}
			var resp struct {
				Data WebhookRes `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("响应解析失败: %v，body=%q", err, rec.Body.String())
			}
			if !resp.Data.OK {
				t.Fatalf("data.ok 应为 true，body=%q", rec.Body.String())
			}
		})
	}
}

// writeDocFile 写入文档示例运行所需的文件（自动创建父目录）。
func writeDocFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", name, err)
	}
}
