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

以下示例展示五个模块组合成一个最小可运行的 HTTP + 数据库应用（以 MySQL 为例）：

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/buexplain/zckg/zcconfig"
	"github.com/buexplain/zckg/zcdb"
	"github.com/buexplain/zckg/zchttp"
	"github.com/buexplain/zckg/zcquit"
	_ "github.com/go-sql-driver/mysql" // 注册 mysql 驱动
)

type User struct {
	Id   int64  `db:"id" json:"id"`
	Name string `db:"name" json:"name"`
	Age  int    `db:"age" json:"age"`
}

func main() {
	// 1. 配置：从 .env 读取数据库地址，泛型读取带默认值
	_ = zcconfig.LoadEnv(".env")
	host := zcconfig.Env[string]("DB_HOST", "127.0.0.1")
	port := zcconfig.Env[int]("DB_PORT", 3306)

	// 2. 数据库：创建主从连接池 + DAO（方言决定 SQL 形态）
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "root:root@tcp(" + host + ":" + fmt.Sprint(port) + ")/test?parseTime=true",
	})
	if err != nil {
		panic(err)
	}
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		panic(err)
	}
	zcquit.AddSigHandler(10, func(sig os.Signal) { _ = db.Close() }) // 退出时关闭连接池

	// 3. HTTP：路由 + 参数绑定 + 统一响应
	type ListReq struct {
		MinAge int `form:"min_age"`
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
	engine := zchttp.NewEngine()
	engine.Router = router

	// 4. 启动服务（Run 不含优雅关闭，可改为自行管理 *http.Server）
	go func() {
		if err := engine.Run(&http.Server{Addr: ":8080"}); err != nil && err != http.ErrServerClosed {
			zcquit.Shutdown() // 启动失败主动触发退出
		}
	}()

	// 5. 阻塞等待退出信号（SIGTERM/SIGINT/SIGQUIT），按级别执行清理 handler
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
