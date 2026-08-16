package zchttp

// 本文件是递归审查（docs/recursion-review-plan.md / recursion-review-report.md）
// 修复闭环的回归锁死测试：REC-01~REC-07 修复后，所有对抗用例（U1–U9）必须
// 在进程内正常完成并满足后验断言。若修复回退，本文件中的用例将以
// 栈溢出（测试进程崩溃）或 -timeout 挂起的形式失败。
//
// 对抗类型定义沿用审查阶段实证过的合法 Go 递归类型（计划 3.3 节）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// ---------- 对抗类型定义（均为合法 Go 类型） ----------

// U1 切片自引用
type recS []recS

// U2 map 自引用
type recM map[string]recM

// U3 指针自引用
type recP *recP

// U4 指针互引用
type recA *recB
type recB *recA

// U5 嵌入自身指针
type recE1 struct{ *recE1 }

// U6 嵌入互引用
type recE2 struct{ *recE3 }
type recE3 struct{ *recE2 }

// U8 接口自引用
type recI interface{ M() recI }

// U5/U6 的携带者（匿名嵌入触发 buildStructMeta 展开，修复 REC-01）
type recReqEmbedE1 struct {
	recE1
	Name string `json:"name"`
}

type recReqEmbedE2 struct {
	recE2
	Name string `json:"name"`
}

// U1/U2 作为普通字段（打击 REC-02/03/05 的 Slice/Map 穿透）
type recReqFieldS struct {
	Items recS `json:"items"`
}

type recReqFieldM struct {
	Items recM `json:"items"`
}

// U3/U4 作为普通字段（打击 REC-04 的 typeToSchema Ptr 分支）
type recReqFieldP struct {
	Item recP `json:"item"`
}

type recReqFieldAB struct {
	Item recA `json:"item"`
}

// U8 作为字段（Interface 分支应跳过）
type recReqFieldI struct {
	Item recI `json:"item"`
}

type recPlainRes struct {
	OK bool `json:"ok"`
}

// U9 运行时环数据辅助类型
type recLinked struct {
	Name string     `json:"name"`
	Next *recLinked `json:"next"`
}

type recMapNode struct {
	Name string                `json:"name"`
	M    map[string]recMapNode `json:"m"`
}

type recSliceNode struct {
	Name string         `json:"name"`
	L    []recSliceNode `json:"l"`
}

// 深拷贝共享语义辅助类型：两个指针字段共享同一目标
type recSharedX struct {
	V int `json:"v"`
}

type recSharedReq struct {
	A *recSharedX `json:"a"`
	B *recSharedX `json:"b"`
}

// 对照基准：字段级 struct 自引用（正常形态，修复不得破坏）
type recCatNode struct {
	Name     string        `json:"name"`
	Children []*recCatNode `json:"children"`
}

type recCatReq struct {
	Root recCatNode `json:"root"`
}

// ---------- 辅助函数 ----------

func typeOfRec[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

func valueOfPtr(v any) reflect.Value { return reflect.ValueOf(v) }

// newRecGenerator 构造最小可用的 openAPIGenerator（供 typeToSchema 隔离验证）
func newRecGenerator() *openAPIGenerator {
	return &openAPIGenerator{
		schemas:           map[string]any{},
		typeNames:         map[reflect.Type]string{},
		nameToType:        map[string]reflect.Type{},
		reachedViaValue:   map[reflect.Type]bool{},
		reachedByDefaults: map[reflect.Type]bool{},
	}
}

// ---------- REC-01：buildStructMeta 嵌入环防环 ----------

// TestRecFix_BuildStructMetaSkipsEmbedCycle 锁死：自引用嵌入被跳过而非栈溢出，
// 且跳过不影响同级普通字段的展开（确定性结果）。
func TestRecFix_BuildStructMetaSkipsEmbedCycle(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"U5 嵌入自身指针", typeOfRec[recReqEmbedE1]()},
		{"U6 嵌入互引用", typeOfRec[recReqEmbedE2]()},
		{"U5 裸类型", typeOfRec[recE1]()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta := buildStructMeta(c.typ) // 修复前：栈溢出
			// 重复构建结果确定（缓存安全的前提）
			meta2 := buildStructMeta(c.typ)
			if len(meta.fields) != len(meta2.fields) {
				t.Fatalf("buildStructMeta 结果不确定：%d vs %d 字段", len(meta.fields), len(meta2.fields))
			}
		})
	}

	// U5 携带者：嵌入被跳过，但同级 Name 字段必须保留
	meta := buildStructMeta(typeOfRec[recReqEmbedE1]())
	found := false
	for _, fm := range meta.fields {
		if fm.name == "name" {
			found = true
		}
	}
	if !found {
		t.Errorf("recReqEmbedE1 的 name 字段丢失：嵌入环跳过不得影响同级字段，fields=%+v", meta.fields)
	}
}

// TestRecFix_RegisterAndServeCyclicReq 锁死：病态递归类型可完成注册全链
// （buildEntry/checkUnsupportedDefaults/路由表）并正常处理请求。
func TestRecFix_RegisterAndServeCyclicReq(t *testing.T) {
	router := NewRouter()
	// U5/U6 嵌入环：GET（query 绑定）+ POST（JSON 绑定）
	router.GET("/embed-e1", func(_ context.Context, req recReqEmbedE1) (recPlainRes, error) {
		return recPlainRes{OK: req.Name == "hit"}, nil
	})
	router.POST("/embed-e2", func(_ context.Context, req recReqEmbedE2) (recPlainRes, error) {
		return recPlainRes{OK: req.Name == "hit"}, nil
	})
	// U1/U2 容器自引用字段
	router.POST("/field-s", func(_ context.Context, req recReqFieldS) (recPlainRes, error) {
		return recPlainRes{OK: true}, nil
	})
	router.POST("/field-m", func(_ context.Context, req recReqFieldM) (recPlainRes, error) {
		return recPlainRes{OK: true}, nil
	})
	// U3/U4 指针自引用/互引用字段
	router.POST("/field-p", func(_ context.Context, req recReqFieldP) (recPlainRes, error) {
		return recPlainRes{OK: true}, nil
	})
	router.POST("/field-ab", func(_ context.Context, req recReqFieldAB) (recPlainRes, error) {
		return recPlainRes{OK: true}, nil
	})
	// U8 接口自引用字段
	router.GET("/field-i", func(_ context.Context, req recReqFieldI) (recPlainRes, error) {
		return recPlainRes{OK: true}, nil
	})

	engine := NewEngine()
	engine.Router = router

	// 请求全链（含请求阶段的 cachedStructMeta/绑定/校验）必须正常
	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/embed-e1?name=hit", ""},
		{http.MethodPost, "/embed-e2", `{"name":"hit"}`},
		{http.MethodPost, "/field-s", `{}`},
		{http.MethodPost, "/field-m", `{}`},
		{http.MethodPost, "/field-p", `{}`},
		{http.MethodPost, "/field-ab", `{}`},
		{http.MethodGet, "/field-i", ""},
	}
	for _, c := range cases {
		var req *http.Request
		if c.body != "" {
			req = httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(c.method, c.path, nil)
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s = %d, want 200, body=%s", c.method, c.path, rec.Code, rec.Body.String())
			continue
		}
		// handler 收到的 Req 功能正常（嵌入环跳过不影响可绑定字段）
		if strings.Contains(c.path, "embed") {
			var res recPlainRes
			wrapper := struct {
				Data recPlainRes `json:"data"`
			}{}
			if err := json.Unmarshal(rec.Body.Bytes(), &wrapper); err == nil {
				res = wrapper.Data
			}
			if !res.OK {
				t.Errorf("%s %s：name 字段绑定/回显异常，body=%s", c.method, c.path, rec.Body.String())
			}
		}
	}
}

// ---------- REC-02/03/05：类型树扫描三函数的防环 ----------

// TestRecFix_TypeScanTerminates 锁死：hasRequestPhaseDefaults / hasNonzeroInTree /
// checkUnsupportedDefaults 对自引用容器类型终止（修复前：栈溢出/死循环）。
func TestRecFix_TypeScanTerminates(t *testing.T) {
	for _, typ := range []reflect.Type{typeOfRec[recReqFieldS](), typeOfRec[recReqFieldM]()} {
		if got := hasRequestPhaseDefaults(typ, nil); got {
			t.Errorf("hasRequestPhaseDefaults(%v) = true, want false", typ)
		}
		if got := hasNonzeroInTree(typ, nil); got {
			t.Errorf("hasNonzeroInTree(%v) = true, want false", typ)
		}
		// 直接调用 checkUnsupportedDefaults（修复前 U1 死循环于展开 for）
		checkUnsupportedDefaults(typ, true, true, "POST", "/x", "h", "f.go", 1, map[reflect.Type]bool{})
	}

	// 防环不得破坏正常判定：嵌套 nonzero 仍须被检出
	type recInner struct {
		Code string `json:"code" nonzero:"true"`
	}
	type recOuter struct {
		Inner recInner `json:"inner"`
	}
	if !hasNonzeroInTree(typeOfRec[recOuter](), nil) {
		t.Errorf("hasNonzeroInTree：嵌套 nonzero 漏检（防环修复过度）")
	}
	// struct 自引用经字段（非嵌入）：visiting 拦截且普通字段判定不受影响
	type recSelfField struct {
		Name string        `json:"name" nonzero:"true"`
		Next *recSelfField `json:"next"`
	}
	if !hasNonzeroInTree(typeOfRec[recSelfField](), nil) {
		t.Errorf("hasNonzeroInTree：自引用 struct 字段的 nonzero 漏检")
	}
}

// ---------- REC-04：typeToSchema 深度上限 ----------

// TestRecFix_TypeToSchemaDepthTruncation 锁死：Ptr/Slice/Map 自引用链在深度上限处
// 退化为空 schema（修复前：栈溢出），且正常类型不受影响。
func TestRecFix_TypeToSchemaDepthTruncation(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"U1 Slice 自引用", typeOfRec[recReqFieldS]().Field(0).Type},
		{"U2 Map 自引用", typeOfRec[recReqFieldM]().Field(0).Type},
		{"U3 Ptr 自引用", typeOfRec[recReqFieldP]().Field(0).Type},
		{"U4 Ptr 互引用", typeOfRec[recReqFieldAB]().Field(0).Type},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := newRecGenerator()
			schema := g.typeToSchema(c.typ, emptyField, 0) // 修复前：栈溢出
			if schema == nil {
				t.Fatalf("typeToSchema 返回 nil")
			}
		})
	}

	// 对照：正常标量/切片 schema 不因深度参数退化
	g := newRecGenerator()
	strSchema := g.typeToSchema(reflect.TypeOf(""), emptyField, 0)
	if strSchema["type"] != "string" {
		t.Errorf("string schema = %v, want type=string", strSchema)
	}
	sliceSchema := g.typeToSchema(reflect.TypeOf([]int{}), emptyField, 0)
	if sliceSchema["type"] != "array" {
		t.Errorf("[]int schema = %v, want type=array", sliceSchema)
	}
	items, _ := sliceSchema["items"].(map[string]any)
	if items == nil || items["type"] != "integer" {
		t.Errorf("[]int items = %v, want type=integer", items)
	}
}

// TestRecFix_GenerateOpenAPICyclicTypes 锁死：公开 API 全链
// （注册 + GenerateOpenAPI）对全部对抗类型终止并产出合法文档。
func TestRecFix_GenerateOpenAPICyclicTypes(t *testing.T) {
	router := NewRouter()
	router.POST("/field-s", func(_ context.Context, req recReqFieldS) (recPlainRes, error) { return recPlainRes{}, nil })
	router.POST("/field-m", func(_ context.Context, req recReqFieldM) (recPlainRes, error) { return recPlainRes{}, nil })
	router.POST("/field-p", func(_ context.Context, req recReqFieldP) (recPlainRes, error) { return recPlainRes{}, nil })
	router.POST("/field-ab", func(_ context.Context, req recReqFieldAB) (recPlainRes, error) { return recPlainRes{}, nil })
	router.GET("/embed-e1", func(_ context.Context, req recReqEmbedE1) (recPlainRes, error) { return recPlainRes{}, nil })
	router.POST("/cat", func(_ context.Context, req recCatReq) (recPlainRes, error) { return recPlainRes{}, nil })

	doc := GenerateOpenAPI(router, OpenAPIInfo{Title: "t", Version: "v"}) // 修复前 U3/U4：栈溢出
	paths, _ := doc["paths"].(map[string]any)
	if len(paths) == 0 {
		t.Fatalf("GenerateOpenAPI 未产出 paths")
	}

	// 对照锁死：正常 struct 自引用的 $ref 占位机制不受深度上限影响——
	// recCatNode 的 children.items 必须仍为 $ref 回指自身，而非被截断为空 schema
	components, _ := doc["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	cat, _ := schemas["recCatNode"].(map[string]any)
	if cat == nil {
		t.Fatalf("components/schemas 缺少 recCatNode：%v", schemas)
	}
	props, _ := cat["properties"].(map[string]any)
	children, _ := props["children"].(map[string]any)
	items, _ := children["items"].(map[string]any)
	// children 为 []*recCatNode：items 经 Ptr 分支包装为 nullable + allOf[$ref]
	allOf, _ := items["allOf"].([]any)
	ref := ""
	if len(allOf) > 0 {
		first, _ := allOf[0].(map[string]any)
		ref, _ = first["$ref"].(string)
	}
	if !strings.Contains(ref, "recCatNode") {
		t.Errorf("recCatNode.children.items = %v, want allOf 内 $ref 回指自身（防环过度修复）", items)
	}
}

// ---------- REC-06：deepCopyDefaults 环状数据防环 ----------

// TestRecFix_DeepCopyHandlesCycles 锁死：环状数据深拷贝终止（修复前：栈溢出）。
// 防环策略为深度上限（与既有“每处出现独立新副本”语义兼容）：
// 副本在截断深度处保留浅引用，本测试仅锁死终止性与浅层断开/值保留。
func TestRecFix_DeepCopyHandlesCycles(t *testing.T) {
	// 指针自环：a.Next = &a
	a := &recLinked{Name: "root"}
	a.Next = a
	deepCopyDefaults(valueOfPtr(a).Elem()) // 修复前：栈溢出
	if a.Next == a {
		t.Errorf("深拷贝未断开顶层共享：a.Next 仍指向 a")
	}
	if a.Next.Name != "root" {
		t.Errorf("副本值丢失：a.Next.Name = %q, want root", a.Next.Name)
	}

	// map 自引用：m["x"] 值内含自身 map
	m := recMapNode{Name: "root", M: map[string]recMapNode{}}
	m.M["x"] = m
	deepCopyDefaults(valueOfPtr(&m).Elem()) // 修复前：栈溢出
	if m.M["x"].Name != "root" {
		t.Errorf("map 副本值丢失：%+v", m.M)
	}

	// 切片共享环
	s := recSliceNode{Name: "root"}
	s.L = []recSliceNode{s}
	s.L[0].L = s.L
	deepCopyDefaults(valueOfPtr(&s).Elem())
	if s.L[0].Name != "root" {
		t.Errorf("切片副本值丢失：%+v", s.L)
	}
}

// TestRecFix_DeepCopyKeepsOccurrenceIndependent 锁死：深度上限防环不改变既有语义——
// 共享同一目标的两个指针仍各自独立拷贝（与 TestDeepCopyDefaults_ArrayElemNotShared 一致）。
func TestRecFix_DeepCopyKeepsOccurrenceIndependent(t *testing.T) {
	x := &recSharedX{V: 7}
	req := recSharedReq{A: x, B: x}
	deepCopyDefaults(valueOfPtr(&req).Elem())
	if req.A == x || req.B == x {
		t.Errorf("深拷贝未断开与原数据的共享")
	}
	if req.A == req.B {
		t.Errorf("既有语义被破坏：每处出现应独立新副本（got A==B==%p）", req.A)
	}
	if req.A.V != 7 || req.B.V != 7 {
		t.Errorf("副本值丢失：A.V=%d B.V=%d, want 7", req.A.V, req.B.V)
	}
}

// ---------- U9 实例级防环回归（#7/#19 结论为安全，修复不得破坏） ----------

// TestRecFix_ApplyDefaultsAndValidateCycles 锁死：applyDefaults/validateNonzero
// 对三类环状数据仍终止（实例级防环回归基线）。
func TestRecFix_ApplyDefaultsAndValidateCycles(t *testing.T) {
	v := &recLinked{Name: "root"}
	v.Next = v
	meta := cachedStructMeta(typeOfRec[recLinked]())
	applyDefaults(valueOfPtr(v), meta)
	if err := validateNonzero(valueOfPtr(v), meta); err != nil {
		t.Errorf("validateNonzero = %v, want nil", err)
	}

	m := recMapNode{Name: "root", M: map[string]recMapNode{}}
	m.M["x"] = m
	mMeta := cachedStructMeta(typeOfRec[recMapNode]())
	applyDefaults(valueOfPtr(&m), mMeta)
	if err := validateNonzero(valueOfPtr(&m), mMeta); err != nil {
		t.Errorf("validateNonzero(map 环) = %v, want nil", err)
	}

	s := recSliceNode{Name: "root"}
	s.L = []recSliceNode{s}
	s.L[0].L = s.L
	sMeta := cachedStructMeta(typeOfRec[recSliceNode]())
	applyDefaults(valueOfPtr(&s), sMeta)
	if err := validateNonzero(valueOfPtr(&s), sMeta); err != nil {
		t.Errorf("validateNonzero(切片环) = %v, want nil", err)
	}
}

// ---------- 对照锁死：正常自引用功能不受修复影响 ----------

// TestRecFix_NormalSelfRefEndToEnd 锁死：字段级 struct 自引用（树形结构）
// 注册/绑定/OpenAPI 全链功能正常（防过度修复的端到端基线）。
func TestRecFix_NormalSelfRefEndToEnd(t *testing.T) {
	router := NewRouter()
	router.POST("/cat", func(_ context.Context, req recCatReq) (recPlainRes, error) {
		return recPlainRes{OK: req.Root.Name == "root" && len(req.Root.Children) == 1 &&
			req.Root.Children[0].Name == "child"}, nil
	})
	engine := NewEngine()
	engine.Router = router

	body := `{"root":{"name":"root","children":[{"name":"child"}]}}`
	req := httptest.NewRequest(http.MethodPost, "/cat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /cat = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Errorf("树形自引用绑定结果异常：body=%s", rec.Body.String())
	}
}
