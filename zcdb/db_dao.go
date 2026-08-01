package zcdb

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ctxTxKey 事务在 context 中的存储键
type ctxTxKey struct{}

// txFromCtx 从 context 中提取事务对象，不存在时返回 nil
func txFromCtx(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(ctxTxKey{}).(*sql.Tx)
	return tx
}

// dialectGrammar 根据方言名称推导对应的 Grammar 编译器
func dialectGrammar(dialect string) (Grammar, error) {
	switch dialect {
	case "mysql":
		return &MySQLGrammar{}, nil
	case "postgresql", "postgres", "pgsql":
		return &PostgresGrammar{}, nil
	case "sqlite", "sqlite3":
		return &SQLiteGrammar{}, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownDialect, dialect)
	}
}

// SlowSQLCallback 慢 SQL 回调函数类型。
// 参数依次为：ctx 上下文、elapsed 耗时、sqlStr SQL 语句、args 绑定参数。
type SlowSQLCallback func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any)

// DBDao 数据库访问对象，是面向用户的唯一入口。
type DBDao struct {
	pool    *Pool                                                                       // 连接池（提供 *sql.DB 和读写路由）
	grammar Grammar                                                                     // SQL 编译器（由 dialect 推导）
	onSQL   func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) //SQL 回调函数
}

// NewDBDao 创建数据库访问对象。
// dialect 用于推导 Grammar 编译器：
//
//	"mysql"      → MySQLGrammar
//	"postgresql" → PostgresGrammar
//	"sqlite"     → SQLiteGrammar
//
// slowSQLMillis 控制慢 SQL 检测行为：
//
//	<0 禁用日志输出
//	=0 全量日志输出
//	>0 仅超过阈值（毫秒）时输出
func NewDBDao(pool *Pool, dialect string, onSQL func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any)) (*DBDao, error) {
	grammar, err := dialectGrammar(dialect)
	if err != nil {
		return nil, err
	}
	return &DBDao{
		pool:    pool,
		grammar: grammar,
		onSQL:   onSQL,
	}, nil
}

// Builder 创建 SQL 构造器，链式调用的起点。
func (d *DBDao) Builder() *Builder {
	return NewBuilder(d.grammar, d)
}

func (d *DBDao) Pool() *Pool {
	return d.pool
}

// Exec 执行原始 SQL（含慢 SQL 检测 + 读写路由），返回 sql.Result。
// args 为 SQL 中占位符的绑定值，支持基本类型（int、string、float、bool、time.Time 等）。
func (d *DBDao) Exec(ctx context.Context, sqlStr string, args ...any) (sql.Result, error) {
	start := time.Now()

	var result sql.Result
	var err error

	// 事务检测：有事务走事务连接，否则走写库
	if tx := txFromCtx(ctx); tx != nil {
		result, err = tx.ExecContext(ctx, sqlStr, args...)
	} else {
		db := d.pool.PickWriteDB()
		result, err = db.ExecContext(ctx, sqlStr, args...)
	}

	// 慢 SQL 检测
	if d.onSQL != nil {
		d.onSQL(ctx, time.Since(start), sqlStr, args)
	}

	return result, err
}

// Query 执行原始查询，返回 *sql.Rows 调用方负责 Close。
// args 类型规则同 Exec。
func (d *DBDao) Query(ctx context.Context, sqlStr string, args ...any) (*sql.Rows, error) {
	start := time.Now()

	var rows *sql.Rows
	var err error

	// 事务检测：有事务走事务连接，否则走读库
	if tx := txFromCtx(ctx); tx != nil {
		rows, err = tx.QueryContext(ctx, sqlStr, args...)
	} else {
		db := d.pool.PickReadDB()
		rows, err = db.QueryContext(ctx, sqlStr, args...)
	}

	// 慢 SQL 检测
	if d.onSQL != nil {
		d.onSQL(ctx, time.Since(start), sqlStr, args)
	}

	return rows, err
}

// Transaction 开启事务，通过 context 传播。
// 回调返回 nil → Commit；返回 error → Rollback。
// 回调 panic → defer Rollback 兜底，确保事务始终回滚。
// 嵌套调用时检测到 ctx 中已有事务，直接传播（不再开新事务）。
func (d *DBDao) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	// 嵌套事务检测：ctx 中已有事务则直接传播
	if tx := txFromCtx(ctx); tx != nil {
		return fn(ctx)
	}

	// 开启事务（始终走主库）
	tx, err := d.pool.master.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("zcdb: begin transaction failed: %w", err)
	}

	// 将事务绑定到 ctx
	txCtx := context.WithValue(ctx, ctxTxKey{}, tx)

	// 标记事务是否已提交
	committed := false
	// defer Rollback 兜底：如果回调 panic 或 Commit 失败，确保事务回滚
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// 执行回调
	if err := fn(txCtx); err != nil {
		// 回滚
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("zcdb: rollback failed: %v, original error: %w", rbErr, err)
		}
		committed = true // 已回滚，defer 不再重复执行
		return err
	}

	// 提交
	if err := tx.Commit(); err != nil {
		// Commit 失败后事务已结束，database/sql 会自动回滚，
		// 再调用 Rollback 只会返回 ErrTxDone 这类误导性错误，故直接返回提交错误。
		committed = true // 已提交（或自动回滚），defer 不再重复执行
		return fmt.Errorf("zcdb: commit failed: %w", err)
	}
	committed = true
	return nil
}

// Close 关闭底层连接池。
func (d *DBDao) Close() error {
	if d.pool != nil {
		return d.pool.Close()
	}
	return nil
}
