package zchttp

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBuildStructMeta_DuplicateBindingNamePanic 锁死 PB-05：两个字段解析出同一绑定名时，
// 注册阶段（buildStructMeta）直接 panic，避免 query 写全部、path 只写第一个的不确定行为。
func TestBuildStructMeta_DuplicateBindingNamePanic(t *testing.T) {
	type req struct {
		A string `form:"x" json:"a"`
		B string `form:"x" json:"b"`
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("duplicate binding name should panic")
		}
		if !strings.Contains(fmt.Sprint(recovered), `duplicate form binding name "x"`) {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	_ = buildStructMeta(reflect.TypeOf(req{}))
}

// TestBuildStructMeta_DuplicateJSONNamePanic 锁死 JSON 重复名注册期 panic：
// 使用不同 form 名、相同 json 名，确保 Form 检查通过后 JSON 检查仍拒绝歧义定义。
func TestBuildStructMeta_DuplicateJSONNamePanic(t *testing.T) {
	type dupA struct {
		X string
	}
	type req struct {
		dupA
		Y string `form:"-" json:"X"`
	}
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("duplicate JSON name should panic")
		}
		if !strings.Contains(fmt.Sprint(recovered), `duplicate json name "X"`) {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	_ = buildStructMeta(reflect.TypeOf(req{}))
}

// TestBuildStructMeta_SourceFlags 锁死注册期预计算的来源能力：
// Form 文本、Form 文件、JSON、任意 body 字段四个标志与字段投影规则一致。
func TestBuildStructMeta_SourceFlags(t *testing.T) {
	type normal struct {
		A string
	}
	type formDash struct {
		A string `form:"-" json:"a"`
	}
	type jsonDash struct {
		A string `json:"-"`
	}
	type jsonDashComma struct {
		A string `json:"-,"`
	}
	type mapField struct {
		A map[string]any
	}
	type fileField struct {
		F *multipart.FileHeader
	}
	type bothSkipped struct {
		A string `form:"-" json:"-"`
	}
	type empty struct{}

	cases := []struct {
		name        string
		typ         reflect.Type
		wantForm    bool
		wantFile    bool
		wantJSON    bool
		wantAnyBody bool
	}{
		{"normal", reflect.TypeOf(normal{}), true, false, true, true},
		{"form dash", reflect.TypeOf(formDash{}), false, false, true, true},
		{"json dash", reflect.TypeOf(jsonDash{}), true, false, false, true},
		{"json dash comma", reflect.TypeOf(jsonDashComma{}), true, false, true, true},
		{"map", reflect.TypeOf(mapField{}), false, false, true, true},
		{"file", reflect.TypeOf(fileField{}), false, true, true, true},
		{"both skipped", reflect.TypeOf(bothSkipped{}), false, false, false, false},
		{"empty", reflect.TypeOf(empty{}), false, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := buildStructMeta(tc.typ)
			if meta.hasFormText != tc.wantForm || meta.hasFormFile != tc.wantFile ||
				meta.hasJSON != tc.wantJSON || meta.hasAnyBody != tc.wantAnyBody {
				t.Fatalf("flags = form:%v file:%v json:%v any:%v, want form:%v file:%v json:%v any:%v",
					meta.hasFormText, meta.hasFormFile, meta.hasJSON, meta.hasAnyBody,
					tc.wantForm, tc.wantFile, tc.wantJSON, tc.wantAnyBody)
			}
		})
	}
}

// TestBuildStructMeta_BothSkippedFields 覆盖 form:"-" + json:"-" 的整体排除分支：
// 字段本身与匿名嵌入中的该字段都不进入元数据，且不解析 default/nonzero。
func TestBuildStructMeta_BothSkippedFields(t *testing.T) {
	type embeddedBoth struct {
		Hidden string `form:"-" json:"-" default:"x" nonzero:"true"`
	}
	type req struct {
		embeddedBoth
		Only string `form:"-" json:"-" default:"x" nonzero:"true"`
		Kept string `json:"kept"`
	}
	meta := buildStructMeta(reflect.TypeOf(req{}))
	foundKept := false
	for _, fm := range meta.fields {
		if fm.field.Name == "Only" {
			if !fm.excluded || fm.nonzero || fm.hasDefault {
				t.Fatalf("Only should be fully excluded without tags, got %+v", fm)
			}
			continue
		}
		if fm.excluded {
			t.Fatalf("匿名嵌入中的整体排除字段不应进入可绑定集合: %+v", fm)
		}
		if fm.field.Name == "Kept" {
			foundKept = true
		}
	}
	if !foundKept {
		t.Fatal("正常字段 Kept 应进入可绑定集合")
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

// TestBuildStructMeta_SkipKeepRawBody 验证嵌入的 KeepRawBody 空结构体不产生任何字段元数据，
// 且不影响兄弟字段的名称与反射索引（Name 位于结构体第 2 个字段，索引应为 [1]）。
func TestBuildStructMeta_SkipKeepRawBody(t *testing.T) {
	type reqRaw struct {
		KeepRawBody
		Name string `json:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqRaw{}))

	if len(meta.fields) != 1 {
		t.Fatalf("expected 1 field (KeepRawBody expands to nothing), got %d", len(meta.fields))
	}
	if meta.fields[0].name != "name" {
		t.Fatalf("expected field 'name', got %q", meta.fields[0].name)
	}
	if !reflect.DeepEqual(meta.fields[0].indices, []int{1}) {
		t.Fatalf("expected index [1], got %v", meta.fields[0].indices)
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

// TestBuildStructMeta_DashCommaAndFormDash 锁死 Form/JSON 两套 tag 语义：
// json:"-," 是名为 "-" 的合法 JSON 字段；json:"-" 只跳过 JSON，Form 链回退字段名；
// form:"-" 无条件跳过 Form 链，但 JSON 仍按 json 标签绑定。
func TestBuildStructMeta_DashCommaAndFormDash(t *testing.T) {
	type dashTags struct {
		Dash      string    `json:"-," default:"d" nonzero:"true"`
		TimeForm  time.Time `form:"-" json:"when" time_format:"unix"`
		FormDash  string    `form:"-" json:"v" default:"v" nonzero:"true"`
		Excluded  string    `json:"-" default:"x" nonzero:"true"`
		QueryOnly string    `form:"q" json:"-" default:"q" nonzero:"true"`
	}

	meta := buildStructMeta(reflect.TypeOf(dashTags{}))
	fields := make(map[string]fieldMeta, len(meta.fields))
	for _, fm := range meta.fields {
		fields[fm.field.Name] = fm
	}

	if fm := fields["Dash"]; fm.excluded || fm.name != "Dash" || !fm.jsonIncluded || !fm.hasDefault || !fm.nonzero || !fm.defaultSatisfiesNonzero {
		t.Errorf("json:\"-,\" should keep form field name and tags, got %+v", fm)
	}
	if fm := fields["FormDash"]; fm.excluded || fm.name != "" || !fm.jsonIncluded || !fm.hasDefault || !fm.nonzero || !fm.defaultSatisfiesNonzero {
		t.Errorf("form:\"-\" should skip Form chain but keep JSON tags, got %+v", fm)
	}
	if fm := fields["TimeForm"]; fm.excluded || fm.name != "" || !fm.jsonIncluded || fm.timeFormat != "unix" {
		t.Errorf("form:\"-\" time field should keep time_format, got %+v", fm)
	}
	if fm := fields["Excluded"]; fm.excluded || fm.name != "Excluded" || fm.jsonIncluded || !fm.hasDefault || !fm.nonzero {
		t.Errorf("json:\"-\" without form name should keep Form fallback and tags, got %+v", fm)
	}
	if fm := fields["QueryOnly"]; fm.excluded || fm.name != "q" || fm.jsonIncluded || !fm.hasDefault || !fm.nonzero {
		t.Errorf("json:\"-\" with valid form name should remain Form-bindable, got %+v", fm)
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

// ========== derefType 测试 ==========

// TestDerefType_SelfReferentialPtr 验证自引用指针类型不会使 derefType 死循环
func TestDerefType_SelfReferentialPtr(t *testing.T) {
	done := make(chan reflect.Type, 1)
	go func() {
		done <- derefType(reflect.TypeOf((selfPtr)(nil)))
	}()
	select {
	case got := <-done:
		// 达到深度上限后应原样返回（仍为指针类型），而非死循环
		if got.Kind() != reflect.Ptr {
			t.Errorf("derefType(selfPtr) kind = %v, want Ptr", got.Kind())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed: derefType infinite loop on self-referential pointer type (timed out)")
	}
}

// TestIsStructLikeVariants 覆盖结构体、指针、非结构体类型的判定分支
func TestIsStructLikeVariants(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"struct", reflect.TypeOf(helloReq{}), true},
		{"ptr to struct", reflect.TypeOf(&helloReq{}), true},
		{"int", reflect.TypeOf(0), false},
		{"ptr to int", reflect.TypeOf((*int)(nil)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStructLike(tc.typ); got != tc.want {
				t.Fatalf("isStructLike(%v) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

// ========== 匿名嵌入 struct 展开（问题1 回归锁死） ==========

// TestBuildStructMeta_UnexportedValueEmbedFlattened 未导出「值」嵌入仍应被展开：
// 值字段已在内存中、fieldByIndex 不触发 Set，内部导出字段可正常绑定（防误伤）。
func TestBuildStructMeta_UnexportedValueEmbedFlattened(t *testing.T) {
	type unexportedBase struct {
		Name string `json:"name"`
	}
	type req struct {
		unexportedBase
		Note string `json:"note"`
	}

	meta := buildStructMeta(reflect.TypeOf(req{}))

	names := make(map[string]bool)
	for _, fm := range meta.fields {
		names[fm.name] = true
	}
	if !names["name"] {
		t.Fatalf("unexported value embed should be flattened: inner field 'name' missing, fields=%+v", meta.fields)
	}
	if !names["note"] {
		t.Fatalf("sibling field 'note' missing, fields=%+v", meta.fields)
	}
}

// TestBuildStructMeta_UnexportedPtrEmbedSkipped 未导出「指针」嵌入应被跳过展开：
// 注册不 panic、其内部字段不进入可绑定集合（问题1 修复）。
func TestBuildStructMeta_UnexportedPtrEmbedSkipped(t *testing.T) {
	type unexportedBase struct {
		Name string `json:"name"`
	}
	type req struct {
		*unexportedBase
		Note string `json:"note"`
	}

	meta := buildStructMeta(reflect.TypeOf(req{}))

	for _, fm := range meta.fields {
		if fm.name == "name" {
			t.Fatalf("unexported pointer embed must be skipped: 'name' should not be bindable, fields=%+v", meta.fields)
		}
	}
	if len(meta.fields) != 1 || meta.fields[0].name != "note" {
		t.Fatalf("expected only sibling field 'note', got fields=%+v", meta.fields)
	}
}

// TestBuildStructMeta_ExportedPtrEmbedFlattened 导出「指针」嵌入仍应被展开（现状不回归）。
func TestBuildStructMeta_ExportedPtrEmbedFlattened(t *testing.T) {
	type ExportedBase struct {
		Name string `json:"name"`
	}
	type req struct {
		*ExportedBase
		Note string `json:"note"`
	}

	meta := buildStructMeta(reflect.TypeOf(req{}))

	names := make(map[string]bool)
	for _, fm := range meta.fields {
		names[fm.name] = true
	}
	if !names["name"] {
		t.Fatalf("exported pointer embed should be flattened: 'name' missing, fields=%+v", meta.fields)
	}
	if !names["note"] {
		t.Fatalf("sibling field 'note' missing, fields=%+v", meta.fields)
	}
}

// ========== cachedStructMeta 测试（Nit-1: LoadOrStore） ==========

// TestCachedStructMeta_ConcurrentConsistent 验证并发首次构建同一类型时，
// 所有 goroutine 经 LoadOrStore 返回同一份缓存 structMeta（内容一致），
// 且缓存命中后的后续调用与首次构建结果一致
func TestCachedStructMeta_ConcurrentConsistent(t *testing.T) {
	type cacheProbe struct {
		Name    string   `json:"name" default:"x"`
		Count   int      `json:"count" nonzero:"true"`
		Percent *float64 `json:"percent"`
	}
	tp := reflect.TypeOf(cacheProbe{})

	const n = 16
	metas := make([]structMeta, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			metas[idx] = cachedStructMeta(tp)
		}(i)
	}
	wg.Wait()

	for i, m := range metas {
		if !reflect.DeepEqual(m, metas[0]) {
			t.Fatalf("goroutine %d returned inconsistent structMeta", i)
		}
	}
	if again := cachedStructMeta(tp); !reflect.DeepEqual(again, metas[0]) {
		t.Fatal("cache hit returned inconsistent structMeta")
	}
}

type raceMeta struct{ A string }

// TestCachedStructMeta_ConcurrentLoadOrStore 以并发竞争覆盖
// LoadOrStore 发现其他协程已抢先写入的分支（尽力触发：多轮并发重建）。
func TestCachedStructMeta_ConcurrentLoadOrStore(t *testing.T) {
	typ := reflect.TypeOf(raceMeta{})
	for round := 0; round < 30; round++ {
		structMetaCache.Delete(typ)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 64; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				cachedStructMeta(typ)
			}()
		}
		close(start)
		wg.Wait()
	}
	if _, ok := structMetaCache.Load(typ); !ok {
		t.Fatal("并发构建后缓存应存在")
	}
}

// TestBuildStructMetaWithVisiting_AlreadyVisiting 覆盖入口类型已在
// visiting 集合中时返回空 meta 断开环的分支（包内直调）。
func TestBuildStructMetaWithVisiting_AlreadyVisiting(t *testing.T) {
	typ := reflect.TypeOf(raceMeta{})
	m := buildStructMetaWithVisiting(typ, map[reflect.Type]bool{typ: true})
	if len(m.fields) != 0 {
		t.Fatalf("已在访问路径上的类型应返回空 meta，实际: %+v", m)
	}
}

// SelfEmbedReq 自引用嵌入类型：注册期嵌入展开必须断开环并告警，不得栈溢出。
// 类型名必须导出，否则会先被“未导出嵌入指针”分支拦截，触达不到递归环检测。
type SelfEmbedReq struct {
	*SelfEmbedReq
	Name string `json:"name"`
}

type selfEmbedRes struct {
	Name string `json:"name"`
}

// TestRegister_SelfRecursiveEmbedding 覆盖嵌入展开检测到自引用环时
// 跳过嵌入并告警的分支：注册与请求均应正常完成。
func TestRegister_SelfRecursiveEmbedding(t *testing.T) {
	router := NewRouter()
	router.POST("/self", func(_ context.Context, req SelfEmbedReq) (selfEmbedRes, error) {
		return selfEmbedRes{Name: req.Name}, nil
	})
	rec := serveRequest(t, router, http.MethodPost, "/self", `{"name":"n1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "n1") {
		t.Fatalf("自引用嵌入类型应正常绑定，实际: %s", rec.Body.String())
	}
}

// embedIgnored 嵌入结构体含 json:"-" 字段：JSON 跳过，但 Form 链回退字段名。
type embedIgnored struct {
	Secret string `json:"-"`
	Shown  string `json:"shown"`
}

type embedIgnoredReq struct {
	embedIgnored
}

// TestBuildStructMeta_EmbeddedFieldSkipped 覆盖嵌入展开：json:"-" 子字段仍进入
// Form 链（字段名 Secret），JSON 不参与；正常嵌入字段 Shown 同时进入两套来源。
func TestBuildStructMeta_EmbeddedFieldSkipped(t *testing.T) {
	m := buildStructMeta(reflect.TypeOf(embedIgnoredReq{}))
	secretFound := false
	shownFound := false
	for _, fm := range m.fields {
		if fm.name == "" {
			t.Fatalf("meta 不应包含 Form 链跳过的字段: %+v", fm)
		}
		if fm.field.Name == "Secret" {
			secretFound = true
			if fm.name != "Secret" || fm.jsonIncluded {
				t.Fatalf("json:\"-\" 嵌入字段应仅参与 Form 链: %+v", fm)
			}
		}
		if fm.name == "shown" {
			shownFound = true
		}
	}
	if !secretFound {
		t.Fatal("json:\"-\" 的嵌入字段应按字段名进入 Form 链")
	}
	if !shownFound {
		t.Fatal("正常嵌入字段 shown 应进入可绑定集合")
	}
}

// TestBuildStructMeta_TimeTagsOnAllBoundForms 锁死 time_format/time_location 在绑定支持的**全部**时间字段
// 形态上都被解析：值类型、任意层指针、单层切片（值元素/指针元素）。历史缺陷：判定条件曾写死
// f.Type == time.Time，指针与切片字段的标签在元数据层被丢弃，绑定链路退化为自动探测
// （自定义 layout 静默丢值、time_location 静默失效）。
// 同时锁死反向边界：绑定链路本身不支持的形态（多层切片、*[]time.Time、map 值、非时间字段）
// 不解析标签，避免"读了却没生效"。
func TestBuildStructMeta_TimeTagsOnAllBoundForms(t *testing.T) {
	type probe struct {
		Value    time.Time            `json:"value" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
		Ptr      *time.Time           `json:"ptr" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
		PtrPtr   **time.Time          `json:"ptr_ptr" time_format:"unix" time_location:"UTC"`
		Slice    []time.Time          `json:"slice" time_format:"02/01/2006" time_location:"Asia/Shanghai"`
		SlicePtr []*time.Time         `json:"slice_ptr" time_format:"unix" time_location:"UTC"`
		Nested   [][]time.Time        `json:"nested" time_format:"unix" time_location:"UTC"`
		PtrSlice *[]time.Time         `json:"ptr_slice" time_format:"unix" time_location:"UTC"`
		MapTime  map[string]time.Time `json:"map_time" time_format:"unix" time_location:"UTC"`
		NotTime  []int                `json:"not_time" time_format:"unix" time_location:"UTC"`
	}

	meta := buildStructMeta(reflect.TypeOf(probe{}))
	fieldMap := make(map[string]fieldMeta, len(meta.fields))
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	bound := []struct {
		name   string
		format string
		tz     string
	}{
		{"value", "02/01/2006", "Asia/Shanghai"},
		{"ptr", "02/01/2006", "Asia/Shanghai"},
		{"ptr_ptr", "unix", "UTC"},
		{"slice", "02/01/2006", "Asia/Shanghai"},
		{"slice_ptr", "unix", "UTC"},
	}
	for _, want := range bound {
		fm, ok := fieldMap[want.name]
		if !ok {
			t.Fatalf("字段 %s 未进入元数据", want.name)
		}
		if fm.timeFormat != want.format {
			t.Errorf("%s.timeFormat = %q, want %q", want.name, fm.timeFormat, want.format)
		}
		if fm.timeLocation == nil || fm.timeLocation.String() != want.tz {
			t.Errorf("%s.timeLocation = %v, want %s", want.name, fm.timeLocation, want.tz)
		}
	}

	for _, name := range []string{"nested", "ptr_slice", "map_time", "not_time"} {
		fm := fieldMap[name]
		if fm.timeFormat != "" || fm.timeLocation != nil {
			t.Errorf("绑定不支持的形态 %s 不应解析时间标签，实际 format=%q tz=%v", name, fm.timeFormat, fm.timeLocation)
		}
	}
}
