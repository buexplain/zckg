package zchttp_test

// docs/openapi.md 示例代码的编译级校验（对应“文档与代码一致”中的示例可编译性）。
// 手工收录文档示例，不自动读取 Markdown；文档示例变更须同步本文件。
// 使用外部测试包（zchttp_test）以调用方视角校验 API 写法，不伪造任何测试存根。
// 其中 TestDocExamples_Deprecated 额外执行一遍生成流程，核对文档描述的产物。

import (
	"context"
	"reflect"
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
