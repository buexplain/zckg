package zcdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestSQLiteInteg_ScanStruct_ErrorSentinels 验证 ScanStruct 对非法 dest 返回的哨兵错误可被 errors.Is 匹配。
func TestSQLiteInteg_ScanStruct_ErrorSentinels(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	ctx := context.Background()

	t.Run("NonPointerDest_ErrNotPointer", func(t *testing.T) {
		rows, err := db.Query(ctx, "SELECT id, name FROM users LIMIT 1")
		if err != nil {
			t.Fatalf("query error: %v", err)
		}
		defer func(rows *sql.Rows) {
			_ = rows.Close()
		}(rows)
		err = ScanStruct(rows, struct {
			Name string `db:"name"`
		}{})
		if err == nil {
			t.Fatal("expected error for non-pointer dest, got nil")
		}
		if !errors.Is(err, ErrNotPointer) {
			t.Errorf("expected errors.Is(err, ErrNotPointer), got: %v", err)
		}
	})

	t.Run("IntPtrDest_ErrScanDest", func(t *testing.T) {
		rows, err := db.Query(ctx, "SELECT id FROM users LIMIT 1")
		if err != nil {
			t.Fatalf("query error: %v", err)
		}
		defer func(rows *sql.Rows) {
			_ = rows.Close()
		}(rows)
		err = ScanStruct(rows, new(int))
		if err == nil {
			t.Fatal("expected error for *int dest, got nil")
		}
		if !errors.Is(err, ErrScanDest) {
			t.Errorf("expected errors.Is(err, ErrScanDest), got: %v", err)
		}
	})

	t.Run("IntSliceDest_ErrInvalidStruct", func(t *testing.T) {
		rows, err := db.Query(ctx, "SELECT id FROM users LIMIT 1")
		if err != nil {
			t.Fatalf("query error: %v", err)
		}
		defer func(rows *sql.Rows) {
			_ = rows.Close()
		}(rows)
		var ints []int
		err = ScanStruct(rows, &ints)
		if err == nil {
			t.Fatal("expected error for *[]int dest, got nil")
		}
		if !errors.Is(err, ErrInvalidStruct) {
			t.Errorf("expected errors.Is(err, ErrInvalidStruct), got: %v", err)
		}
	})
}

// TestSQLiteInteg_NullSafeField_ErrorSentinels 验证 nullSafeField.Scan 对转换失败返回 ErrScanConvert 哨兵错误。
func TestSQLiteInteg_NullSafeField_ErrorSentinels(t *testing.T) {
	t.Run("BytesToInt_ErrScanConvert", func(t *testing.T) {
		var target int
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("abc"))
		if err == nil {
			t.Fatal("expected error for []byte(\"abc\") → int, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("StringToInt_ErrScanConvert", func(t *testing.T) {
		var target int
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan("abc")
		if err == nil {
			t.Fatal("expected error for string(\"abc\") → int, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("Int8Overflow_ErrScanConvert", func(t *testing.T) {
		var target int8
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("300"))
		if err == nil {
			t.Fatal("expected error for \"300\" → int8 overflow, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("InvalidJSONToSlice_ErrScanConvert", func(t *testing.T) {
		var target []int
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("not json"))
		if err == nil {
			t.Fatal("expected error for invalid JSON → []int, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
		var jsonErr *json.SyntaxError
		if !errors.As(err, &jsonErr) {
			t.Errorf("expected error chain to contain *json.SyntaxError, got: %v", err)
		}
	})

	t.Run("TimeToInt_Fallback_ErrScanConvert", func(t *testing.T) {
		var target int
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan(time.Now())
		if err == nil {
			t.Fatal("expected error for time.Time → int fallback, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("BytesToUint_ErrScanConvert", func(t *testing.T) {
		var target uint
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("-1"))
		if err == nil {
			t.Fatal("expected error for []byte(\"-1\") → uint, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("BytesToFloat_ErrScanConvert", func(t *testing.T) {
		var target float64
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("xyz"))
		if err == nil {
			t.Fatal("expected error for []byte(\"xyz\") → float64, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("BytesToBool_ErrScanConvert", func(t *testing.T) {
		var target bool
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("maybe"))
		if err == nil {
			t.Fatal("expected error for []byte(\"maybe\") → bool, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("BytesToMap_ErrScanConvert", func(t *testing.T) {
		var target map[string]any
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("not json"))
		if err == nil {
			t.Fatal("expected error for []byte(\"not json\") → map[string]any, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
		var jsonErr *json.SyntaxError
		if !errors.As(err, &jsonErr) {
			t.Errorf("expected error chain to contain *json.SyntaxError, got: %v", err)
		}
	})

	t.Run("BytesToStruct_ErrScanConvert", func(t *testing.T) {
		var target struct{ Name string }
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan([]byte("not json"))
		if err == nil {
			t.Fatal("expected error for []byte(\"not json\") → struct, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("StringToUint_ErrScanConvert", func(t *testing.T) {
		var target uint
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan("-1")
		if err == nil {
			t.Fatal("expected error for string(\"-1\") → uint, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})

	t.Run("StringToFloat_ErrScanConvert", func(t *testing.T) {
		var target float64
		field := reflect.ValueOf(&target).Elem()
		nsf := &nullSafeField{field: field}
		err := nsf.Scan("xyz")
		if err == nil {
			t.Fatal("expected error for string(\"xyz\") → float64, got nil")
		}
		if !errors.Is(err, ErrScanConvert) {
			t.Errorf("expected errors.Is(err, ErrScanConvert), got: %v", err)
		}
	})
}
