# zchttp 代码审计计划

- 审计日期：2026-08-28
- 审计维度：代码实现正确性、潜在 bug、文档时效性、注释准确性
- 审计范围：约 3800 行源码（13 个源文件）+ 7 份文档

## 1. 审计范围

| 类型 | 文件 |
|---|---|
| 引擎 | `httpEngine.go`、`defaults.go`、`context.go`、`responseWriter.go` |
| 路由 | `router.go`、`router_trie.go`、`meta.go` |
| 绑定校验 | `binding.go`、`validate.go` |
| 中间件 | `middleware.go` |
| OpenAPI | `openapi.go`、`buildEntry.go` |
| 错误 | `errors.go` |
| 测试 | 17 个测试文件（含 `docs_deviation_review_test.go`） |
| 文档 | `docs/` 下 7 份：http-engine-callback、middleware、openapi、parameter-binding、parameter-validate、request、routing |

## 2. 审计方法（按维度）

### 2.1 代码实现正确性与潜在 bug
1. **引擎层**：`NewEngine()` 构造与默认回调装配（`MaxBodyBytes`/`MultipartFormMaxMemory` 32MB 不变量）；字面量构造的防护；请求体读取/限制；`http.Handler` 契约（超时、取消传播）。
2. **路由层**：单一基数树正确性——静态段与参数段冲突解析、优先级、尾斜杠、大小写、特殊字符、空段；方法匹配与 405/404 语义；路径参数解码与绑定；并发注册/查询安全性。
3. **绑定与校验**：反射绑定的类型覆盖（基础类型、指针、切片、嵌套结构、time.Time 等）；来源优先级（query/path/header/body）；校验失败的错误结构与状态码；恶意/畸形输入（超大值、非法编码）。
4. **中间件**：新签名（传递 `w` 和 `r`）下的包装正确性；中间件链的执行顺序；中间件改写响应/短路的行为；goroutine 泄漏（对照 `middleware_goroutine_test.go`）。
5. **handler 契约**：`func(ctx, Req) (Res, error)` 注册期校验（不通过即 panic，不变量）；Res 为 nil/零值的处理；error 的响应映射。
6. **responseWriter**：状态码/头部的写入时机、重复写、Hijack/Flush 支持。
7. **OpenAPI**：生成规范与路由注册信息的一致性（路径、方法、参数、响应结构）。
8. 运行 `go build ./zchttp/... ; go vet ./zchttp/... ; go test ./zchttp/... -race` 作为证据；跑一次 `bench_perf_test` 确认无异常。

### 2.2 文档时效性
1. 逐份对照 7 份文档与当前实现，特别注意最近变更点：
   - 中间件函数签名已改为传递 `w` 和 `r`（破坏性变更）——文档/示例是否已同步。
   - 路由重构为单一基数树、统一"静态路由"术语——文档术语是否一致。
2. 文档示例代码逐一验证可编译/行为一致（`docs_deviation_review_test.go` 是回归锁，先确认其覆盖面再补充人工核对）。

### 2.3 注释准确性
1. 核对所有导出符号注释与实现；重点：默认值承诺（32MB）、panic 条件、中间件签名说明、路由匹配规则描述。

## 3. 重点风险区

- `HttpEngine` 必须经 `NewEngine()` 构造（不变量）。
- handler 签名校验不通过即 panic（不变量）。
- 中间件签名破坏性变更后的一致性。
- 基数树静态/参数段混合匹配正确性。

## 4. 执行步骤

1. 读取全部 13 个源文件与关键测试。
2. 运行构建/静态检查/`-race` 测试。
3. 按子系统逐维度审计并记录问题（文件:行号、现象、影响、证据）。
4. 逐份文档对照核对。
5. 汇总为 `docs/code-audit-report.md`。

## 5. 问题分级

- P0：路由错误分发、绑定错误导致数据错误、安全缺陷（请求体限制失效等）。
- P1：边界输入/并发下的缺陷。
- P2：文档/注释与实现不一致。
- P3：可读性建议。

## 6. 交付物

`zchttp/docs/code-audit-report.md`：含审计结论、问题清单（级别/位置/说明/解决方案）、验证证据。
