// 本文件为 Insert/Update 数据提取（reflect.go）的 fuzz 测试：
// 用固定结构体覆盖指针、any、切片、空切片、nil 指针元素与无字段错误路径，
// 参考模型独立推导列名/值/行，并与 ToInsert/ToUpdate 编译结果交叉校验
// （args 与 SQL 占位符数量一致）。不依赖数据库。

package zcdb

import (
	"errors"
	"reflect"
	"testing"
)

type fuzzInsertRow struct {
	Name   *string `db:"name"`
	Age    any     `db:"age"`
	Active bool    `db:"active"`
	Score  int64   `db:"score"`
	Data   []byte  `db:"data"`
	Skip   string  `db:"-"`
}

type fuzzOptionalRow struct {
	Name *string `db:"name"`
	Age  any     `db:"age"`
}

// FuzzInsertUpdateData 校验 extractInsertData / extractUpdateData 的列与值提取：
// fuzzer 选择数据形态（值/指针/结构体切片/指针切片/空切片/nil 元素/无可选字段/nil），
// 参考模型按文档规则推导期望结果，再验证 ToInsert/ToUpdate 的 args 与占位符数量。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzInsertUpdateData$' -fuzztime=30s
func FuzzInsertUpdateData(f *testing.F) {
	type seed struct {
		op      uint8
		kind    uint8
		nameSet bool
		name    string
		ageKind uint8
		ageInt  int64
		ageStr  string
		active  bool
		score   int64
		data    string
	}
	seeds := []seed{
		{0, 0, true, "alice", 1, 25, "", true, 100, "x"},
		{0, 1, false, "", 0, 0, "", false, 0, ""},
		{0, 2, true, "alice", 2, 0, "age", true, 10, "ab"},
		{0, 3, true, "alice", 0, 0, "", true, 10, ""},
		{0, 4, false, "", 0, 0, "", false, 0, ""},
		{0, 5, false, "", 0, 0, "", false, 0, ""},
		{0, 6, false, "", 0, 0, "", false, 0, ""},
		{0, 6, true, "alice", 0, 0, "", false, 0, ""},
		{0, 7, false, "", 0, 0, "", false, 0, ""},
		{1, 0, true, "alice", 1, 25, "", true, 100, "x"},
		{1, 1, false, "", 0, 0, "", false, 0, ""},
		{1, 2, false, "", 0, 0, "", false, 0, ""},
		{1, 3, true, "alice", 0, 0, "", false, 0, ""},
		{1, 4, true, "alice", 0, 0, "", false, 0, ""},
		{1, 5, false, "", 0, 0, "", false, 0, ""},
	}
	for _, s := range seeds {
		f.Add(s.op, s.kind, s.nameSet, s.name, s.ageKind, s.ageInt, s.ageStr, s.active, s.score, []byte(s.data))
	}

	f.Fuzz(func(t *testing.T, op, kind uint8, nameSet bool, name string, ageKind uint8, ageInt int64, ageStr string, active bool, score int64, data []byte) {
		if len(name) > 4<<10 || len(ageStr) > 4<<10 || len(data) > 4<<10 {
			return
		}
		row0 := makeFuzzInsertRow(nameSet, name, ageKind, ageInt, ageStr, active, score, data)
		row1 := fuzzInsertRow{Active: !active, Score: score + 1}
		optional := makeFuzzOptionalRow(nameSet, name, ageKind, ageInt, ageStr)

		if op%2 == 0 {
			input, wantCols, wantRows, wantErr := fuzzInsertInput(kind, row0, row1, optional)
			columns, rows, err := extractInsertData(input, "db")
			assertFuzzExtractResult(t, "insert", kind, wantCols, wantRows, wantErr, columns, rows, err)
			if wantErr != nil {
				if _, _, compileErr := newTestBuilder(NewMySQLGrammar(), nil).Table("t").ToInsert(input); !errors.Is(compileErr, wantErr) {
					t.Fatalf("insert kind=%d: compile error = %v, want %v", kind, compileErr, wantErr)
				}
				return
			}
			sqlStr, args, compileErr := newTestBuilder(NewMySQLGrammar(), nil).Table("t").ToInsert(input)
			assertNoError(t, compileErr)
			if got := codePlaceholderCount(sqlStr, mysqlScanSyntax, "MySQL"); got != len(args) {
				t.Fatalf("insert kind=%d sql=%q: %d placeholder(s), %d arg(s)", kind, sqlStr, got, len(args))
			}
			var flat []any
			for _, row := range wantRows {
				flat = append(flat, row...)
			}
			if !reflect.DeepEqual(flat, args) {
				t.Fatalf("insert kind=%d args=%v, want %v", kind, args, flat)
			}
			return
		}

		input, wantCols, wantValues, wantErr := fuzzUpdateInput(kind, row0, optional)
		columns, values, err := extractUpdateData(input, "db")
		assertFuzzExtractResult(t, "update", kind, wantCols, [][]any{wantValues}, wantErr, columns, [][]any{values}, err)
		if wantErr != nil {
			if _, _, compileErr := newTestBuilder(NewMySQLGrammar(), nil).Table("t").ToUpdate(input); !errors.Is(compileErr, wantErr) {
				t.Fatalf("update kind=%d: compile error = %v, want %v", kind, compileErr, wantErr)
			}
			return
		}
		sqlStr, args, compileErr := newTestBuilder(NewMySQLGrammar(), nil).Table("t").ToUpdate(input)
		assertNoError(t, compileErr)
		if got := codePlaceholderCount(sqlStr, mysqlScanSyntax, "MySQL"); got != len(args) {
			t.Fatalf("update kind=%d sql=%q: %d placeholder(s), %d arg(s)", kind, sqlStr, got, len(args))
		}
		if !reflect.DeepEqual(wantValues, args) {
			t.Fatalf("update kind=%d args=%v, want %v", kind, args, wantValues)
		}
	})
}

func makeFuzzInsertRow(nameSet bool, name string, ageKind uint8, ageInt int64, ageStr string, active bool, score int64, data []byte) fuzzInsertRow {
	row := fuzzInsertRow{Active: active, Score: score, Data: data}
	if nameSet {
		value := name
		row.Name = &value
	}
	switch ageKind % 3 {
	case 1:
		row.Age = ageInt
	case 2:
		row.Age = ageStr
	}
	return row
}

func makeFuzzOptionalRow(nameSet bool, name string, ageKind uint8, ageInt int64, ageStr string) fuzzOptionalRow {
	row := fuzzOptionalRow{}
	if nameSet {
		value := name
		row.Name = &value
	}
	switch ageKind % 3 {
	case 1:
		row.Age = ageInt
	case 2:
		row.Age = ageStr
	}
	return row
}

// fuzzInsertInput 按 kind 生成输入与参考结果：
// 0 值、1 指针、2 值切片（两行）、3 指针切片（两行）、4 空切片、
// 5 nil 指针元素、6 可选字段结构体、其它 nil。
func fuzzInsertInput(kind uint8, row0, row1 fuzzInsertRow, optional fuzzOptionalRow) (any, []string, [][]any, error) {
	switch kind % 8 {
	case 0:
		cols, rows := fuzzInsertExpect([]fuzzInsertRow{row0})
		return row0, cols, rows, nil
	case 1:
		cols, rows := fuzzInsertExpect([]fuzzInsertRow{row0})
		return &row0, cols, rows, nil
	case 2:
		cols, rows := fuzzInsertExpect([]fuzzInsertRow{row0, row1})
		return []fuzzInsertRow{row0, row1}, cols, rows, nil
	case 3:
		cols, rows := fuzzInsertExpect([]fuzzInsertRow{row0, row1})
		return []*fuzzInsertRow{&row0, &row1}, cols, rows, nil
	case 4:
		return []fuzzInsertRow{}, nil, nil, ErrEmptyData
	case 5:
		return []*fuzzInsertRow{nil}, nil, nil, ErrInvalidStruct
	case 6:
		cols, values, err := fuzzOptionalExpect(optional)
		if err != nil {
			return optional, nil, nil, err
		}
		return optional, cols, [][]any{values}, nil
	default:
		return nil, nil, nil, ErrInvalidStruct
	}
}

// fuzzUpdateInput 按 kind 生成输入与参考结果：
// 0 值、1 指针、2 可选值、3 可选指针、4 切片（非法）、其它 nil 指针。
func fuzzUpdateInput(kind uint8, row0 fuzzInsertRow, optional fuzzOptionalRow) (any, []string, []any, error) {
	switch kind % 6 {
	case 0:
		cols, values := fuzzUpdateExpect(row0)
		return row0, cols, values, nil
	case 1:
		cols, values := fuzzUpdateExpect(row0)
		return &row0, cols, values, nil
	case 2:
		cols, values, err := fuzzOptionalExpect(optional)
		return optional, cols, values, err
	case 3:
		cols, values, err := fuzzOptionalExpect(optional)
		return &optional, cols, values, err
	case 4:
		return []fuzzInsertRow{row0}, nil, nil, ErrInvalidStruct
	default:
		return (*fuzzInsertRow)(nil), nil, nil, ErrInvalidStruct
	}
}

func fuzzInsertExpect(rows []fuzzInsertRow) ([]string, [][]any) {
	first := rows[0]
	var columns []string
	if first.Name != nil {
		columns = append(columns, "name")
	}
	if first.Age != nil {
		columns = append(columns, "age")
	}
	columns = append(columns, "active", "score", "data")

	values := make([][]any, 0, len(rows))
	for _, row := range rows {
		var rowValues []any
		if first.Name != nil {
			if row.Name == nil {
				rowValues = append(rowValues, nil)
			} else {
				rowValues = append(rowValues, *row.Name)
			}
		}
		if first.Age != nil {
			rowValues = append(rowValues, row.Age)
		}
		rowValues = append(rowValues, row.Active, row.Score, row.Data)
		values = append(values, rowValues)
	}
	return columns, values
}

func fuzzUpdateExpect(row fuzzInsertRow) ([]string, []any) {
	var columns []string
	var values []any
	if row.Name != nil {
		columns = append(columns, "name")
		values = append(values, *row.Name)
	}
	if row.Age != nil {
		columns = append(columns, "age")
		values = append(values, row.Age)
	}
	return append(columns, "active", "score", "data"), append(values, row.Active, row.Score, row.Data)
}

func fuzzOptionalExpect(row fuzzOptionalRow) ([]string, []any, error) {
	var columns []string
	var values []any
	if row.Name != nil {
		columns = append(columns, "name")
		values = append(values, *row.Name)
	}
	if row.Age != nil {
		columns = append(columns, "age")
		values = append(values, row.Age)
	}
	if len(columns) == 0 {
		return nil, nil, ErrNoFields
	}
	return columns, values, nil
}

func assertFuzzExtractResult(t *testing.T, op string, kind uint8, wantCols []string, wantRows [][]any, wantErr error, gotCols []string, gotRows [][]any, gotErr error) {
	t.Helper()
	if (gotErr != nil) != (wantErr != nil) {
		t.Fatalf("%s kind=%d: err=%v, want %v", op, kind, gotErr, wantErr)
	}
	if wantErr != nil {
		if !errors.Is(gotErr, wantErr) {
			t.Fatalf("%s kind=%d: err=%v, want errors.Is(%v)", op, kind, gotErr, wantErr)
		}
		return
	}
	if !reflect.DeepEqual(wantCols, gotCols) || !reflect.DeepEqual(wantRows, gotRows) {
		t.Fatalf("%s kind=%d: got cols=%v rows=%v, want cols=%v rows=%v", op, kind, gotCols, gotRows, wantCols, wantRows)
	}
}
