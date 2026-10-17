# zchttp 模糊测试（fuzz）设计与执行

fuzz 是定向单测与外部 oracle 差分的补充：定向用例负责精确期望，差分负责语义一致性，fuzz 负责在更大的输入空间里寻找 `panic`、不变量违背与 oracle 分歧。fuzz 不替代精确断言，也不以语句覆盖率作为主要目标。

## 一、目标清单

| 目标 | 所在文件 | 被测源码 | 输入 | 断言 |
|---|---|---|---|---|
| `FuzzRouterMatch` | `router_trie_test.go` | `router_trie.go`、`normalizePath` | 路由表索引 + 原始路径 | `normalizePath` 契约 + 基数树与线性参考匹配器命中路由/捕获值一致 |
| `FuzzRouterTableShapes` | `router_trie_test.go` | 路由注册 + `router_trie.go` | 生成种子 + 原始路径 | 生成合法路由表不 panic，路由形状 × 路径的命中与捕获值一致 |
| `FuzzBindDifferential` | `binding_test.go` | `binding.go` | 方法索引 + 原始 Content-Type + query + body + `keepRaw` | 与 stdlib 参考绑定比较错误有无与字段快照 |

路由参考实现按决策位置打分取字典序最高分，不与被测基数树共享匹配代码；绑定参考实现独立使用 `net/url`、`encoding/json`、`mime/multipart` 完成字段映射，不调用被测绑定逻辑。

## 二、种子与语料

- 普通 `go test ./...` 会回放 `f.Add` 种子与 `zchttp/testdata/fuzz/<Target>/` 下已收录的失败语料，**不会**主动探索。
- 种子覆盖文档锁死的边界：路由静态/参数/通配优先级、可选省略、回溯、根路径；绑定的方法分流、query/body 覆盖、`KeepRawBody`、JSON null/显式零值/重复键/尾随值、畸形与未知 Content-Type、urlencoded 与 multipart。
- 单次输入上限：路径/query/body 64 KB，Content-Type 1 KB。multipart 的 `maxMemory` 为 32 MB，body 上限低于该阈值，不会落盘；执行器仍会清理 `MultipartForm`。
- fuzz 失败时 Go 会把最小化输入写入 `zchttp/testdata/fuzz/<Target>/`，应将该文件与修复一起提交，并补一条定向回归用例。

## 三、执行命令

一次只能探索一个目标；常规 CI 只跑种子，探索作为独立任务（夜间或本地）执行：

```powershell
go test ./zchttp -run '^$' -fuzz '^FuzzRouterMatch$' -fuzztime=30s
go test ./zchttp -run '^$' -fuzz '^FuzzRouterTableShapes$' -fuzztime=30s
go test ./zchttp -run '^$' -fuzz '^FuzzBindDifferential$' -fuzztime=30s
```

fuzz 报错时先分清是**实现缺陷**还是 **oracle 缺陷**：参考实现同样在被测包内，二者都可能被 fuzz 证伪。

## 四、已知边界

- `FuzzBindDifferential` 只比较错误**有无**，不比较错误类别（Content-Type 解析错误 vs JSON 语法错误）；错误消息由各层自行包装，稳定比较成本高于收益。
- urlencoded body 对除 GET/DELETE/HEAD 外的所有方法生效（含 OPTIONS/TRACE/CONNECT）。`net/http.ParseForm` 只对 POST/PUT/PATCH 读取 body，框架对其余方法显式补读，fuzz 方法域覆盖 `allHTTPMethods` 全部 9 个方法。
- 路由 fuzz 的 `normalizePath` 契约独立断言（前导 `/`、非根无尾 `/`、幂等、追加尾 `/` 等价），避免归一化回归被"两侧共用同一归一化结果"屏蔽。
- 路由表生成器只生成**无注册冲突**的表；注册冲突的 panic 文案由定向测试覆盖，不在 fuzz 输入域内。
- `Any` 注册、OpenAPI 生成等依赖多模块组合的行为不在当前 fuzz 范围，由各自的定向与契约测试覆盖。
