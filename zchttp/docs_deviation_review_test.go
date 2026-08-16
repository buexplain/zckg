package zchttp

// 文档-代码偏离审查（docs/docs-code-deviation-review-report.md）的回归锁死测试：
// 将审查中经 httptest 实测确认的关键行为固化为包内测试，防止后续代码修改
// 使行为再次偏离文档描述。涉及 DEV-01（ReadFrom 标记 written）、
// DEV-02/DEV-03（NewEngine 默认值）、DEV-04（Res 标签参与文档生成）、
// 合并绑定粒度表与容器嵌套深度矩阵。

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func docReviewDoJSON(t *testing.T, h http.Handler, method, target, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var m map[string]any
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("decode response failed: %v, body=%s", err, rec.Body.String())
		}
	}
	return rec, m
}

// DEV-02/DEV-03：NewEngine 默认 MaxBodyBytes 与 MultipartFormMaxMemory 均为 32 MB
//（parameter-binding.md 2.1 节、http-engine-callback.md 第一节/第七步）。
func TestDocReview_NewEngineDefaults(t *testing.T) {
	e := NewEngine()
	// 直接断言文档声称的 32 MB（而非引用常量），常量被改动时本测试能暴露文档偏离
	if e.MaxBodyBytes != 32<<20 {
		t.Errorf("MaxBodyBytes 默认应为 32 MB（parameter-binding.md/http-engine-callback.md）: got %d", e.MaxBodyBytes)
	}
	if e.MultipartFormMaxMemory != 32<<20 {
		t.Errorf("MultipartFormMaxMemory 默认应为 32 MB: got %d", e.MultipartFormMaxMemory)
	}
	if e.OnNotFound == nil || e.OnResponse == nil || e.OnError == nil || e.OnValidationError == nil || e.OnPanic == nil {
		t.Errorf("NewEngine 应装配全部默认回调")
	}
}

// DEV-01：ReadFrom 标记 written（io.Copy(w, reader) 走零拷贝路径后默认 JSON 响应跳过）
//（http-engine-callback.md 第五节 written 判定清单）。
type drReadFromReq struct{}
type drReadFromRes struct{ OK bool }

func TestDocReview_ReadFromMarksWritten(t *testing.T) {
	e := NewEngine()
	e.Router.GET("/dr-readfrom", func(ctx context.Context, req drReadFromReq) (drReadFromRes, error) {
		w, _ := ResponseWriterFromContext(ctx)
		_, _ = io.Copy(w, strings.NewReader("filedata"))
		return drReadFromRes{OK: true}, nil
	})
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dr-readfrom", nil))
	if rec.Body.String() != "filedata" {
		t.Errorf("ReadFrom 应标记 written 使默认 JSON 响应跳过: got %q", rec.Body.String())
	}
}

// parameter-binding.md 一节：POST 等方法合并绑定（先 query 后 body），
// body 覆盖 query 的粒度表（显式零值覆盖 / null 非指针保留 / null 指针置 nil / 未出现保留）。
type drMergeReq struct {
	Page int     `json:"page"`
	Name string  `json:"name"`
	Nick *string `json:"nick"`
}
type drMergeRes struct {
	Page int     `json:"page"`
	Name string  `json:"name"`
	Nick *string `json:"nick"`
}

func TestDocReview_MergeBindingGranularity(t *testing.T) {
	e := NewEngine()
	e.Router.POST("/dr-merge", func(ctx context.Context, req drMergeReq) (drMergeRes, error) {
		return drMergeRes(req), nil
	})
	q := "/dr-merge?page=2&name=qn&nick=qk"

	// 显式出现（含显式零值）→ 覆盖
	_, m := docReviewDoJSON(t, e, http.MethodPost, q, `{"page":0}`)
	data := m["data"].(map[string]any)
	if data["page"].(float64) != 0 || data["name"] != "qn" {
		t.Errorf("显式零值应覆盖 query、未出现字段保留 query: got %v", data)
	}

	// null 对非指针 → no-op 保留 query
	_, m = docReviewDoJSON(t, e, http.MethodPost, q, `{"page":null,"name":null}`)
	data = m["data"].(map[string]any)
	if data["page"].(float64) != 2 || data["name"] != "qn" {
		t.Errorf("null 对非指针应保留 query 值: got %v", data)
	}

	// null 对指针 → 置 nil
	_, m = docReviewDoJSON(t, e, http.MethodPost, q, `{"nick":null}`)
	data = m["data"].(map[string]any)
	if data["nick"] != nil {
		t.Errorf("null 对指针应置 nil: got %v", data["nick"])
	}

	// 未出现 → 保留 query
	_, m = docReviewDoJSON(t, e, http.MethodPost, q, `{}`)
	data = m["data"].(map[string]any)
	if data["page"].(float64) != 2 || data["name"] != "qn" || data["nick"] != "qk" {
		t.Errorf("body 未出现字段应保留 query 值: got %v", data)
	}
}

// request.md 容器嵌套深度矩阵 + DEV-04：
// 单层容器 nonzero/default 生效；多层容器均不生效；
// Res 的 default 展示、Res 的 nonzero 推断 required（参与文档生成）。
type drSingleItem struct {
	Name     string `json:"name" nonzero:"true"`
	IsActive *bool  `json:"isActive" default:"true"`
}
type drMultiItem struct {
	Name     string `json:"name" nonzero:"true"`
	IsActive *bool  `json:"isActive" default:"true"`
}
type drMatrixReq struct {
	Items   []drSingleItem           `json:"items"`
	DeepMap map[string][]drMultiItem `json:"deepMap"`
}

// drMatrixRes 嵌入 Req 以便在响应中回显绑定/填充结果
type drMatrixRes struct {
	drMatrixReq
}
type drResWithTags struct {
	ID   int     `json:"id" nonzero:"true"`
	Addr *string `json:"addr" default:"银河系"`
}

func TestDocReview_ContainerMatrixAndOpenAPIDisplay(t *testing.T) {
	e := NewEngine()
	e.Router.POST("/dr-matrix", func(ctx context.Context, req drMatrixReq) (drMatrixRes, error) {
		return drMatrixRes{req}, nil
	})
	// Res 标签参与文档生成的断言需要另一个路由携带 drResWithTags 作为 Res
	e.Router.GET("/dr-res-tags", func(ctx context.Context, req drReadFromReq) (drResWithTags, error) {
		return drResWithTags{}, nil
	})

	// 单层容器：nonzero 校验生效（报错路径使用绑定名）
	rec, _ := docReviewDoJSON(t, e, http.MethodPost, "/dr-matrix", `{"items":[{"name":""}]}`)
	if rec.Code != 400 {
		t.Fatalf("单层容器 nonzero 应生效: got %d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	if msg, _ := errBody["message"].(string); !strings.Contains(msg, "items.name") || !strings.Contains(msg, "is required") {
		t.Errorf("错误文案应含 items.name 与 is required: got %q", msg)
	}

	// 单层容器指针字段 default 请求阶段补填；多层容器 default/nonzero 均不生效
	rec, m := docReviewDoJSON(t, e, http.MethodPost, "/dr-matrix",
		`{"items":[{"name":"a"}],"deepMap":{"k":[{"name":""}]}}`)
	if rec.Code != 200 {
		t.Fatalf("多层容器 nonzero 不应生效（deepMap 元素 name 为空仍应 200）: got %d body=%s", rec.Code, rec.Body.String())
	}
	data := m["data"].(map[string]any)
	if got := data["items"].([]any)[0].(map[string]any)["isActive"]; got != true {
		t.Errorf("单层容器指针字段 default 应补填 true: got %v", got)
	}
	if got := data["deepMap"].(map[string]any)["k"].([]any)[0].(map[string]any)["isActive"]; got != nil {
		t.Errorf("多层容器指针字段 default 不应填充: got %v", got)
	}

	// OpenAPI：单层容器 struct 的指针 default 展示、多层容器不展示；Res 标签参与文档生成
	doc := GenerateOpenAPI(e.Router, OpenAPIInfo{Title: "dr", Version: "1"})
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)

	single := schemas["drSingleItem"].(map[string]any)["properties"].(map[string]any)
	if single["isActive"].(map[string]any)["default"] != true {
		t.Errorf("drSingleItem.isActive.default 应展示: got %v", single["isActive"])
	}
	multi := schemas["drMultiItem"].(map[string]any)["properties"].(map[string]any)
	if _, has := multi["isActive"].(map[string]any)["default"]; has {
		t.Errorf("drMultiItem.isActive.default 不应展示: got %v", multi["isActive"])
	}
	resSchema := schemas["drResWithTags"].(map[string]any)
	resProps := resSchema["properties"].(map[string]any)
	if d, has := resProps["addr"].(map[string]any)["default"]; !has || d != "银河系" {
		t.Errorf("Res 的 default 应参与文档展示: got %v", resProps["addr"])
	}
	reqList, ok := resSchema["required"].([]any)
	if !ok || len(reqList) != 1 || reqList[0] != "id" {
		t.Errorf("Res 的 nonzero 应推断为 required: got %v", resSchema["required"])
	}
}
