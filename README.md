# zckg

`zckg` 是一个 Go 语言应用开发工具库，提供配置管理、数据库访问、HTTP 服务、模型代码生成与优雅退出五个相互独立的模块，可按需组合使用，构建完整的 Web 应用开发链路。

- 模块路径：`github.com/buexplain/zckg`
- Go 版本要求：**1.25.11+**
- 许可证：[Apache-2.0](LICENSE)

## 模块总览

| 模块 | 说明 | 文档 |
|---|---|---|
| [zcconfig](zcconfig) | 配置加载与泛型读取：`.env` 文件通道（支持 OS 环境变量回退）+ 业务配置树通道（`.` 分隔路径递归查找），另提供 `DBConfig` DSN 生成 | [zcconfig.md](zcconfig/docs/zcconfig.md) |
| [zcdb](zcdb) | 基于 `database/sql` 的数据库访问层：Builder 查询构造器（MySQL / PostgreSQL / SQLite 三方言）、主从连接池与读写分离、事务、结构体自动映射、破坏性操作保护 | [zcdb/docs](zcdb/docs/README.md) |
| [zchttp](zchttp) | HTTP 服务框架：精确 + 基数树双级路由、反射式参数绑定与校验、中间件链、可定制回调（响应/错误/panic/404）、OpenAPI 生成 | [zchttp/docs](zchttp/docs) |
| [zcmodel](zcmodel) | 数据库模型代码生成：输入表结构，输出 Entity/DO 结构体与互转方法的 Go 源码，支持 AST 增量再生成（保留用户自定义代码） | [zcmodel.md](zcmodel/docs/zcmodel.md) |
| [zcquit](zcquit) | 优雅退出：全局可取消上下文 + 信号监听（SIGTERM/SIGINT/SIGQUIT）+ 分级清理 handler，支持 `Shutdown()` 主动触发 | [zcquit.md](zcquit/docs/zcquit.md) |

## 安装

```bash
go get github.com/buexplain/zckg
```

## 快速上手

以下示例展示五个模块组合成一个最小可运行的 HTTP + 数据库应用（以 Sqlite 为例）：

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/buexplain/zckg/zcdb"
	"github.com/buexplain/zckg/zchttp"
	"github.com/buexplain/zckg/zcquit"
	_ "modernc.org/sqlite" // 注册 sqlite 驱动（纯 Go 实现，无需 CGO）
)

type User struct {
	Id   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	Age  int    `db:"age" json:"age"`
}

func main() {
	// 1. 数据库：内存 SQLite。":memory:" 的每个连接是独立的内存库，
	// 连接池限制为单连接，保证所有查询共享同一份数据
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName:   "sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
	})
	if err != nil {
		panic(err)
	}
	db, err := zcdb.NewDBDao(pool, "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		slog.Default().Info(sqlStr, "args", args)
	}, "")
	if err != nil {
		panic(err)
	}
	zcquit.AddSigHandler(10, func(sig os.Signal) { _ = db.Close() }) // 退出时关闭连接池

	// 2. 初始化：建表 + 写入示例数据
	if _, err := db.Exec(context.Background(), `
CREATE TABLE users (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    age  INTEGER NOT NULL
)`); err != nil {
		panic(err)
	}
	if _, err := db.Builder().Table("users").Insert(context.Background(), []User{
		{Id: 1, Name: "张三", Age: 16},
		{Id: 2, Name: "alice", Age: 20},
		{Id: 3, Name: "bob", Age: 25},
		{Id: 4, Name: "carol", Age: 30},
	}); err != nil {
		panic(err)
	}

	// 3. HTTP：路由 + 参数绑定 + 统一响应
	type ListReq struct {
		zchttp.OpenAPIMeta `tags:"用户" summary:"用户列表" description:"按最小年龄查询用户列表"`
		MinAge             int `form:"min_age" description:"最小年龄" example:"18"`
	}
	type ListRes struct {
		Users []User `json:"users"`
	}
	router := zchttp.NewRouter()
	router.GET("/users", func(ctx context.Context, req ListReq) (ListRes, error) {
		var res ListRes
		err := db.Builder().Table("users").
			Where("age", ">=", req.MinAge).
			Find(ctx, &res.Users)
		return res, err
	})

	// 4. /doc 接口：业务路由注册完成后生成一次 OpenAPI 3.0 文档（/doc 自身不参与生成）
	doc := zchttp.GenerateOpenAPI(router, zchttp.OpenAPIInfo{
		Title:       "testGo API",
		Description: "示例服务接口文档",
		Version:     "1.0.0",
		Servers:     []zchttp.OpenAPIServer{{URL: "http://127.0.0.1:8080"}},
	})
	openapiJSON, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	type DocReq struct{}
	type DocRes struct{}
	router.GET("/doc", func(ctx context.Context, req DocReq) (DocRes, error) {
		// 直接写原始 JSON（框架检测到响应已写入会跳过默认的 Response 包装）
		w, _ := zchttp.ResponseWriterFromContext(ctx)
		if r, ok := zchttp.RequestFromContext(ctx); ok && r.URL.Query().Has("download") {
			// 带 download 参数时以附件形式返回，便于导出成 openapi.json 文件
			w.Header().Set("Content-Disposition", `attachment; filename="openapi.json"`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write(openapiJSON)
		return DocRes{}, err
	})

	engine := zchttp.NewEngine()
	engine.Router = router

	// 5. 启动服务（Run 不含优雅关闭，可改为自行管理 *http.Server）
	go func() {
		if err := engine.Run(&http.Server{Addr: ":8080"}); err != nil && !errors.Is(err, http.ErrServerClosed) {
			zcquit.Shutdown() // 启动失败主动触发退出
		}
	}()

	// 6. 阻塞等待退出信号（SIGTERM/SIGINT/SIGQUIT），按级别执行清理 handler
	zcquit.Listen()
}
```

> handler 的签名固定为 `func(ctx context.Context, req Req) (Res, error)`，其中 Req/Res 必须是**结构体或结构体指针**，注册时校验不通过会 panic（见 [routing.md](zchttp/docs/routing.md)）；生产代码中建议使用 [zcmodel](zcmodel/docs/zcmodel.md) 生成的 Entity/DO 作为请求与响应类型。

### 各模块独立示例

- **zcconfig**：`.env` 加载与泛型读取 → [zcconfig.md](zcconfig/docs/zcconfig.md)
- **zcdb**：Builder 查询、写入、事务、读写分离 → [zcdb/docs/README.md](zcdb/docs/README.md) 及分主题文档
- **zchttp**：路由注册、参数绑定、中间件、OpenAPI → [zchttp/docs](zchttp/docs)
- **zcmodel**：从表结构生成模型代码 → [zcmodel.md](zcmodel/docs/zcmodel.md)
- **zcquit**：信号监听与分级清理 → [zcquit.md](zcquit/docs/zcquit.md)

## 测试

```bash
# 运行全部测试（集成测试在数据库不可达时自动跳过）
go test ./...
```

- `*_unit_test.go`：纯单元测试，无外部依赖。
- `*_integration_test.go`：集成测试，连接 `127.0.0.1` 上的 MySQL（3306）/ PostgreSQL（5432）/ SQLite（纯 Go 驱动，无需环境）；数据库不可达时自动 `t.Skip`。

本地准备 MySQL / PostgreSQL 测试环境：

```bash
docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
```

## 目录结构

```
zckg/
├── zcconfig/   # 配置模块（含 docs/ 文档）
├── zcdb/       # 数据库访问模块（含 docs/ 文档）
├── zchttp/     # HTTP 框架模块（含 docs/ 文档）
├── zcmodel/    # 模型代码生成模块（含 docs/ 文档）
├── zcquit/     # 优雅退出模块（含 docs/ 文档）
├── AGENTS.md   # 面向 AI 编码代理的仓库工作指南
├── LICENSE     # Apache-2.0
└── go.mod
```

## 许可证

本项目基于 [Apache License 2.0](LICENSE) 开源。
