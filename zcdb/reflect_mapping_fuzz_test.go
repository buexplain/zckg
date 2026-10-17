// 本文件为结构体字段映射的 fuzz 测试：
// 固定 fixture 覆盖扁平字段、值/指针嵌入、同层重名、非匿名嵌套、自引用嵌入、
// 菱形嵌入、标签与未导出字段；fuzzer 选择 fixture、嵌入指针 nil、any/指针字段与
// 指针输入形态，交叉校验 parseStruct / getScanFieldInfo / extractInsertData / extractUpdateData。

package zcdb

import (
	"errors"
	"reflect"
	"testing"
)

type FuzzMapBase struct {
	ID        int    `db:"id"`
	CreatedAt string `db:"created_at"`
}

type fuzzMapFlat struct {
	ID     int    `db:"id"`
	Name   string // name
	Skip   string `db:"-"`
	hidden string
}

type fuzzMapValueEmbed struct {
	FuzzMapBase
	Name string `db:"name"`
	ID   string `db:"id"` // 外层 ID 遮蔽嵌入 ID
}

type fuzzMapPtrEmbed struct {
	*FuzzMapBase
	Name string `db:"name"`
}

type fuzzMapDup struct {
	A string `db:"x"`
	B string `db:"x"` // 同深度重复，保留 A
	C string `db:"y"`
}

type fuzzMapNested struct {
	Child FuzzMapBase // 具名嵌套，不展开
	Name  string      `db:"name"`
}

type FuzzMapSelf struct {
	*FuzzMapSelf
	Name string `db:"name"`
}

type FuzzMapLeafA struct {
	X string `db:"x"`
}
type FuzzMapLeafB struct{ FuzzMapLeafA }
type FuzzMapLeafC struct{ FuzzMapLeafA }
type fuzzMapDiamond struct {
	FuzzMapLeafB
	FuzzMapLeafC
	Name string `db:"name"`
}

type fuzzMapTagged struct {
	Field  string `db:"custom"`
	NoTag  string
	Skip   string `db:"-"`
	Hidden int    `json:"json_name"` // db 标签缺失 → hidden
}

type fuzzMapAny struct {
	A any     `db:"a"`
	B *string `db:"b"`
}

// FuzzStructFieldMapping 校验字段映射的“外层优先、浅层遮蔽、同层保留先声明、
// 嵌入 BFS、标签/蛇形命名、未导出与 "-" 跳过”规则，并确认 parse/scan/insert/update
// 四处消费同一份映射。fixture 0..8；flags 控制嵌入指针 nil、any/指针字段与指针输入。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzStructFieldMapping$' -fuzztime=30s
func FuzzStructFieldMapping(f *testing.F) {
	seeds := []struct {
		fixture uint8
		flags   uint8
		a, b, c string
		n       int64
	}{
		{0, 0, "a", "b", "c", 7},
		{1, 0, "a", "b", "c", 7},
		{2, 0, "a", "b", "c", 7},
		{2, 1, "a", "b", "c", 7},
		{3, 0, "a", "b", "c", 7},
		{4, 0, "a", "b", "c", 7},
		{5, 0, "a", "b", "c", 7},
		{6, 0, "a", "b", "c", 7},
		{7, 0, "a", "b", "c", 7},
		{8, 0, "a", "b", "c", 7},
		{8, 6, "a", "b", "c", 7},
		{8, 5, "a", "b", "c", 7},
		{8, 15, "a", "b", "c", 7},
	}
	for _, seed := range seeds {
		f.Add(seed.fixture, seed.flags, seed.a, seed.b, seed.c, seed.n)
	}

	f.Fuzz(func(t *testing.T, fixture, flags uint8, a, b, c string, n int64) {
		if len(a) > 4<<10 || len(b) > 4<<10 || len(c) > 4<<10 {
			return
		}
		fixture %= 9
		typ, fields := fuzzMappingFields(fixture)
		value := fuzzMappingValue(fixture, flags, a, b, c, n)

		parsed := parseStruct(typ, "")
		if parsed == nil || !reflect.DeepEqual(parsed.Fields, fields) {
			t.Fatalf("fixture=%d parseStruct=%+v, want %+v", fixture, parsed, fields)
		}
		scanInfo := getScanFieldInfo(typ, "")
		if len(scanInfo.columnIndex) != len(fields) {
			t.Fatalf("fixture=%d scan columns=%v, want %+v", fixture, scanInfo.columnIndex, fields)
		}
		for _, field := range fields {
			if got, ok := scanInfo.columnIndex[field.Column]; !ok || !reflect.DeepEqual(got, field.Index) {
				t.Fatalf("fixture=%d scan column %q index=%v ok=%v, want %v", fixture, field.Column, got, ok, field.Index)
			}
		}

		input := value
		if flags&8 != 0 {
			ptr := reflect.New(reflect.TypeOf(value))
			ptr.Elem().Set(reflect.ValueOf(value))
			input = ptr.Interface()
		}
		wantCols, wantVals := fuzzExpectedExtracted(input, fields)

		cols, vals, err := extractUpdateData(input, "")
		assertFuzzMappingExtract(t, "update", fixture, wantCols, [][]any{wantVals}, err, cols, [][]any{vals})
		cols, rows, err := extractInsertData(input, "")
		assertFuzzMappingExtract(t, "insert", fixture, wantCols, [][]any{wantVals}, err, cols, rows)
	})
}

func fuzzMappingFields(fixture uint8) (reflect.Type, []structField) {
	switch fixture {
	case 0:
		return reflect.TypeOf(fuzzMapFlat{}), []structField{
			{Column: "id", Index: []int{0}},
			{Column: "name", Index: []int{1}},
		}
	case 1:
		return reflect.TypeOf(fuzzMapValueEmbed{}), []structField{
			{Column: "name", Index: []int{1}},
			{Column: "id", Index: []int{2}},
			{Column: "created_at", Index: []int{0, 1}},
		}
	case 2:
		return reflect.TypeOf(fuzzMapPtrEmbed{}), []structField{
			{Column: "name", Index: []int{1}},
			{Column: "id", Index: []int{0, 0}},
			{Column: "created_at", Index: []int{0, 1}},
		}
	case 3:
		return reflect.TypeOf(fuzzMapDup{}), []structField{
			{Column: "x", Index: []int{0}},
			{Column: "y", Index: []int{2}},
		}
	case 4:
		return reflect.TypeOf(fuzzMapNested{}), []structField{
			{Column: "child", Index: []int{0}},
			{Column: "name", Index: []int{1}},
		}
	case 5:
		return reflect.TypeOf(FuzzMapSelf{}), []structField{
			{Column: "name", Index: []int{1}},
		}
	case 6:
		return reflect.TypeOf(fuzzMapDiamond{}), []structField{
			{Column: "name", Index: []int{2}},
			{Column: "x", Index: []int{0, 0, 0}},
		}
	case 7:
		return reflect.TypeOf(fuzzMapTagged{}), []structField{
			{Column: "custom", Index: []int{0}},
			{Column: "no_tag", Index: []int{1}},
			{Column: "hidden", Index: []int{3}},
		}
	default:
		return reflect.TypeOf(fuzzMapAny{}), []structField{
			{Column: "a", Index: []int{0}},
			{Column: "b", Index: []int{1}},
		}
	}
}

func fuzzMappingValue(fixture, flags uint8, a, b, c string, n int64) any {
	nilEmbed := flags&1 != 0
	anySet := flags&2 != 0
	ptrSet := flags&4 != 0
	switch fixture {
	case 0:
		return fuzzMapFlat{ID: int(n), Name: a, Skip: c}
	case 1:
		return fuzzMapValueEmbed{
			FuzzMapBase: FuzzMapBase{ID: int(n), CreatedAt: a},
			Name:        b,
			ID:          c,
		}
	case 2:
		value := fuzzMapPtrEmbed{Name: a}
		if !nilEmbed {
			value.FuzzMapBase = &FuzzMapBase{ID: int(n), CreatedAt: b}
		}
		return value
	case 3:
		return fuzzMapDup{A: a, B: b, C: c}
	case 4:
		return fuzzMapNested{
			Child: FuzzMapBase{ID: int(n), CreatedAt: a},
			Name:  b,
		}
	case 5:
		return FuzzMapSelf{Name: a}
	case 6:
		return fuzzMapDiamond{Name: a}
	case 7:
		return fuzzMapTagged{Field: a, NoTag: b, Skip: c, Hidden: int(n)}
	default:
		value := fuzzMapAny{}
		if anySet {
			value.A = a
		}
		if ptrSet {
			ptr := b
			value.B = &ptr
		}
		return value
	}
}

func fuzzExpectedExtracted(input any, fields []structField) ([]string, []any) {
	value := reflect.ValueOf(input)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	var columns []string
	var values []any
	for _, field := range fields {
		fv, ok := fuzzFieldByIndex(value, field.Index)
		if !ok {
			continue
		}
		switch fv.Kind() {
		case reflect.Interface, reflect.Ptr:
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}
		columns = append(columns, field.Column)
		values = append(values, fv.Interface())
	}
	return columns, values
}

// fuzzFieldByIndex 独立沿索引路径取值，不使用被测的 fieldByIndexSafe，
// 避免该 helper 自身的缺陷同时污染期望值与实际值。
func fuzzFieldByIndex(v reflect.Value, index []int) (reflect.Value, bool) {
	for _, i := range index {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

func assertFuzzMappingExtract(t *testing.T, op string, fixture uint8, wantCols []string, wantRows [][]any, gotErr error, gotCols []string, gotRows [][]any) {
	t.Helper()
	if len(wantCols) == 0 {
		if !errors.Is(gotErr, ErrNoFields) {
			t.Fatalf("fixture=%d %s err=%v, want ErrNoFields", fixture, op, gotErr)
		}
		return
	}
	if gotErr != nil {
		t.Fatalf("fixture=%d %s unexpected err: %v", fixture, op, gotErr)
	}
	if !reflect.DeepEqual(gotCols, wantCols) || !reflect.DeepEqual(gotRows, wantRows) {
		t.Fatalf("fixture=%d %s got cols=%v rows=%v, want cols=%v rows=%v", fixture, op, gotCols, gotRows, wantCols, wantRows)
	}
}
