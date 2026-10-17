// 本文件为确定性故障注入测试：
// 用测试专有 database/sql driver 包装 SQLite，注入 Prepare/Query/Exec/Begin/Commit
// 与 rows.Next/Scan 失败，验证 Cursor、CursorBy、ScanStruct、Exec/Update、Transaction
// 的错误传播与“不静默截断数据”。不使用随机 fuzz，避免故障点不可定位。

package zcdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"

	"modernc.org/sqlite"
)

var errInjectedFault = errors.New("zcdb test: injected fault")

type faultConfig struct {
	mu            sync.Mutex
	failPrepare   bool
	failQuery     bool
	failExec      bool
	failBegin     bool
	failCommit    bool
	failScan      bool
	failNextAfter int // <0 关闭；否则 rowsSeen >= 该值时 Next 返回错误
}

var testFaultConfig = &faultConfig{failNextAfter: -1}

func (c *faultConfig) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failPrepare = false
	c.failQuery = false
	c.failExec = false
	c.failBegin = false
	c.failCommit = false
	c.failScan = false
	c.failNextAfter = -1
}

func (c *faultConfig) configure(fn func(*faultConfig)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c)
}

type faultSnapshot struct {
	failPrepare   bool
	failQuery     bool
	failExec      bool
	failBegin     bool
	failCommit    bool
	failScan      bool
	failNextAfter int
}

func (c *faultConfig) snapshot() faultSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return faultSnapshot{
		failPrepare: c.failPrepare, failQuery: c.failQuery, failExec: c.failExec,
		failBegin: c.failBegin, failCommit: c.failCommit, failScan: c.failScan,
		failNextAfter: c.failNextAfter,
	}
}

type faultDriver struct{ inner driver.Driver }

func (d *faultDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.inner.Open(name)
	if err != nil {
		return nil, err
	}
	return &faultConn{conn: conn}, nil
}

type faultConn struct{ conn driver.Conn }

func (c *faultConn) Prepare(query string) (driver.Stmt, error) {
	if testFaultConfig.snapshot().failPrepare {
		return nil, errInjectedFault
	}
	stmt, err := c.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return &faultStmt{stmt: stmt}, nil
}

func (c *faultConn) Close() error { return c.conn.Close() }

func (c *faultConn) Begin() (driver.Tx, error) {
	return c.begin(context.Background(), driver.TxOptions{})
}

func (c *faultConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.begin(ctx, opts)
}

func (c *faultConn) begin(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if testFaultConfig.snapshot().failBegin {
		return nil, errInjectedFault
	}
	var tx driver.Tx
	var err error
	if beginTx, ok := c.conn.(driver.ConnBeginTx); ok {
		tx, err = beginTx.BeginTx(ctx, opts)
	} else {
		tx, err = c.conn.Begin()
	}
	if err != nil {
		return nil, err
	}
	return &faultTx{tx: tx}, nil
}

func (c *faultConn) Ping(ctx context.Context) error {
	if pinger, ok := c.conn.(driver.Pinger); ok {
		return pinger.Ping(ctx)
	}
	return nil
}

type faultStmt struct{ stmt driver.Stmt }

func (s *faultStmt) Close() error  { return s.stmt.Close() }
func (s *faultStmt) NumInput() int { return s.stmt.NumInput() }

func (s *faultStmt) Exec(args []driver.Value) (driver.Result, error) {
	if testFaultConfig.snapshot().failExec {
		return nil, errInjectedFault
	}
	return s.stmt.Exec(args)
}

func (s *faultStmt) Query(args []driver.Value) (driver.Rows, error) {
	snapshot := testFaultConfig.snapshot()
	if snapshot.failQuery {
		return nil, errInjectedFault
	}
	rows, err := s.stmt.Query(args)
	if err != nil {
		return nil, err
	}
	return &faultRows{rows: rows, config: snapshot}, nil
}

type faultRows struct {
	rows     driver.Rows
	config   faultSnapshot
	rowsSeen int
}

func (r *faultRows) Columns() []string { return r.rows.Columns() }
func (r *faultRows) Close() error      { return r.rows.Close() }

func (r *faultRows) Next(dest []driver.Value) error {
	if r.config.failNextAfter >= 0 && r.rowsSeen >= r.config.failNextAfter {
		return errInjectedFault
	}
	if err := r.rows.Next(dest); err != nil {
		return err
	}
	r.rowsSeen++
	if r.config.failScan {
		for i := range dest {
			dest[i] = "not-a-number"
		}
	}
	return nil
}

type faultTx struct{ tx driver.Tx }

func (t *faultTx) Commit() error {
	if testFaultConfig.snapshot().failCommit {
		return errInjectedFault
	}
	return t.tx.Commit()
}

func (t *faultTx) Rollback() error { return t.tx.Rollback() }

var registerFaultDriverOnce sync.Once

// openFaultDAO 注册/打开测试故障驱动并建表；每个测试独立 DAO，避免 prepared statement 缓存
// 绕过 Prepare 故障注入。
func openFaultDAO(t *testing.T) *DBDao {
	t.Helper()
	registerFaultDriverOnce.Do(func() {
		sql.Register("zcdb_fault_sqlite", &faultDriver{inner: &sqlite.Driver{}})
	})
	testFaultConfig.reset()
	pool, err := NewPool(PoolConfig{
		DriverName:   "zcdb_fault_sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
	})
	if err != nil {
		t.Fatalf("open fault pool: %v", err)
	}
	dao, err := NewDBDao(pool, "sqlite", nil, "", "")
	if err != nil {
		t.Fatalf("create fault dao: %v", err)
	}
	t.Cleanup(func() {
		testFaultConfig.reset()
		_ = dao.Close()
	})
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE fault_t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create fault table: %v", err)
	}
	for id := 1; id <= 3; id++ {
		if _, err := dao.Exec(ctx, `INSERT INTO fault_t (id, name) VALUES (?, ?)`, id, "row"); err != nil {
			t.Fatalf("insert fault row: %v", err)
		}
	}
	return dao
}

func TestFaultInjection_QueryExecPrepareBegin(t *testing.T) {
	ctx := context.Background()

	t.Run("prepare", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failPrepare = true })
		if _, err := dao.Query(ctx, `SELECT id FROM fault_t`); !errors.Is(err, errInjectedFault) {
			t.Fatalf("query err=%v, want injected fault", err)
		}
	})

	t.Run("query", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failQuery = true })
		if _, err := dao.Query(ctx, `SELECT id FROM fault_t`); !errors.Is(err, errInjectedFault) {
			t.Fatalf("query err=%v, want injected fault", err)
		}
	})

	t.Run("exec", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failExec = true })
		_, err := dao.Builder().Table("fault_t").Where("id", "=", 1).Update(ctx, struct {
			Name string `db:"name"`
		}{Name: "x"})
		if !errors.Is(err, errInjectedFault) {
			t.Fatalf("update err=%v, want injected fault", err)
		}
	})

	t.Run("begin", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failBegin = true })
		called := false
		err := dao.Transaction(ctx, func(context.Context) error {
			called = true
			return nil
		})
		if !errors.Is(err, errInjectedFault) || called {
			t.Fatalf("transaction err=%v called=%v, want injected fault and callback not called", err, called)
		}
	})

	t.Run("commit", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failCommit = true })
		err := dao.Transaction(ctx, func(context.Context) error { return nil })
		if !errors.Is(err, errInjectedFault) {
			t.Fatalf("commit err=%v, want injected fault", err)
		}
	})
}

func TestFaultInjection_RowsNext(t *testing.T) {
	ctx := context.Background()

	t.Run("cursor", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failNextAfter = 1 })
		var row struct {
			ID int `db:"id"`
		}
		count := 0
		var gotErr error
		for err := range dao.Builder().Table("fault_t").Select("id").OrderBy("id", "ASC").Cursor(ctx, &row) {
			if err != nil {
				gotErr = err
				break
			}
			count++
		}
		if !errors.Is(gotErr, errInjectedFault) || count != 1 {
			t.Fatalf("cursor err=%v count=%d, want injected fault after 1 row", gotErr, count)
		}
	})

	t.Run("cursor_by", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failNextAfter = 1 })
		var row struct {
			ID int `db:"id"`
		}
		count := 0
		var gotErr error
		for err := range dao.Builder().Table("fault_t").Select("id").CursorBy(ctx, &row, 2, "id") {
			if err != nil {
				gotErr = err
				break
			}
			count++
		}
		if !errors.Is(gotErr, errInjectedFault) || count != 1 {
			t.Fatalf("cursorBy err=%v count=%d, want injected fault after 1 row", gotErr, count)
		}
	})

	t.Run("scan_all_rows", func(t *testing.T) {
		dao := openFaultDAO(t)
		testFaultConfig.configure(func(c *faultConfig) { c.failNextAfter = 1 })
		rows, err := dao.Query(ctx, `SELECT id FROM fault_t ORDER BY id`)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		var dest []struct {
			ID int `db:"id"`
		}
		if err := ScanStructClose(rows, &dest); !errors.Is(err, errInjectedFault) {
			t.Fatalf("scan all rows err=%v, want injected fault (rows=%d)", err, len(dest))
		}
	})
}

func TestFaultInjection_ScanConversion(t *testing.T) {
	dao := openFaultDAO(t)
	testFaultConfig.configure(func(c *faultConfig) { c.failScan = true })
	rows, err := dao.Query(context.Background(), `SELECT id FROM fault_t ORDER BY id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	var dest struct {
		ID int `db:"id"`
	}
	if err := ScanStructClose(rows, &dest); !errors.Is(err, ErrScanConvert) {
		t.Fatalf("scan err=%v, want ErrScanConvert", err)
	}
}
