# zchttp 文档与实现一致性审查报告

> 审查基线：提交 `8e98521c4b5bc32837cdb259aa3e84af0ddc2b4e`（2026-10-17）。审查时 `zchttp` 目录无未提交改动（工作区仅 `zcdb` 存在未提交修改，与本报告无关）。
> 修复状态：**已修复（2026-10-17，未提交）**。第三、四节的 `文件:行` 指向修复前基线，仅用于追溯；修复内容与验收结果见第五、六节（对照当前工作区）。

## 一、审查范围与方法

### 1. 文档清单

- `zchttp/docs/routing.md`
- `zchttp/docs/parameter-binding.md`
- `zchttp/docs/parameter-validate.md`
- `zchttp/docs/middleware.md`
- `zchttp/docs/request.md`
- `zchttp/docs/http-engine-callback.md`
- `zchttp/docs/openapi.md`

### 2. 文档与实现、测试的映射

| 文档 | 主要实现 | 主要锁死测试 |
|---|---|---|
| routing.md | `router.go`、`router_trie.go`、`buildEntry.go` | `router_test.go`、`router_trie_test.go`、`buildEntry_test.go` |
| parameter-binding.md | `binding.go`、`defaults.go`、`meta.go`、`httpEngine.go` | `binding_test.go`、`defaults_test.go`、`meta_test.go` |
| parameter-validate.md | `validate.go`、`meta.go`、`errors.go` | `validate_test.go`、`errors_test.go` |
| middleware.md | `middleware.go`、`httpEngine.go`、`context.go` | `middleware_test.go`、`middleware_goroutine_test.go`、`context_test.go` |
| request.md | `meta.go`、`binding.go`、`defaults.go`、`validate.go`、`openapi.go` | `binding_test.go`、`defaults_test.go`、`docs_deviation_review_test.go` |
| http-engine-callback.md | `httpEngine.go`、`responseWriter.go`、`errors.go`、`context.go` | `httpEngine_test.go`、`responseWriter_test.go`、`context_test.go` |
| openapi.md | `openapi.go`、`meta.go`、`defaults.go` | `openapi_test.go`、`docs_deviation_review_test.go` |

### 3. 实测方法

所有结论均在运行时验证过（除注明外）：在临时目录编写探针 `_test.go`，通过 `go test -overlay` 映射进包内（探针可访问未导出符号），每个探针用 `-run '^TestProbeXxx$'` 单独进程执行。探针与 overlay JSON 均在仓库外，审查结束后已删除，未进入工作区。

实测过的行为：

1. `*[]int` / `[]*[]int` 上 `default` 的运行时填充、注册期告警与 OpenAPI 展示；
2. `*[]int` 同时带 `nonzero` 与 `default` 时的运行时校验与 OpenAPI `required`；
3. GET query 中 `[]Struct`、`[N]int`、`*[2]int`、`*[]int` 是否进入 OpenAPI、运行时能否绑定；
4. 带 `json` 名的匿名嵌入在内层 `nonzero` 下的 OpenAPI `required` 与实际请求结果；
5. 带 `json` 名的匿名嵌入在 query / JSON 两条绑定链路上的键名差异；
6. `[][]time.Time`、`*[]time.Time` 的标签不生效时字段实际状态；
7. multipart 非文件字段超过 `MultipartFormMaxMemory` 时是落盘还是报错；
8. 仅设置 `Content-Type`（不写响应）时默认 JSON 是否跳过；
9. 闭包 handler 在 default 误用告警中的位置格式；
10. `Any` 冲突时是否残留部分方法、9 方法是否全部注册；
11. 通配尾段 `/a//b`、`/a/b/`、`/a/` 的捕获值；
12. 多次 `GenerateOpenAPI` 输出是否字节级一致。

### 4. 问题统计与判定标准

问题总数 **9**：高 **0**、中 **4**、低 **5**。

计入标准：文档声明与源码分支、运行时实测、源码注释或另一份文档存在可复现的偏差，且足以误导调用方或造成契约不一致。仅属内部实现细节、不影响公开契约的不计入。库外证据（Go 标准库）以 `GOROOT/` 前缀引用；本机 GOROOT 为 `C:/Users/buexp/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.25.11.windows-amd64`。

## 二、问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修复 | `*[]T`（指针指向切片）上的 `default` 静默失效且无告警，OpenAPI 仍展示默认值；叠加 `nonzero` 时运行时 400 而文档标为可选 |
| DOC-02 | 中 | 已修复 | GET/DELETE/HEAD 的 OpenAPI query 参数包含运行时不可绑定的非扁平字段（`[]Struct`、`[N]T`、`*[]T`），且 `[]Struct` 会被写入零值元素 |
| DOC-03 | 中 | 已修复 | 带 `json` 名的匿名嵌入中内层 `nonzero` 在运行时强制校验，父 schema 却未把该嵌入属性标为 `required` |
| DOC-04 | 中 | 已修复 | `request.md` 声称匿名嵌入对 JSON 也扁平绑定；带 `json` 名的嵌入实际按嵌套对象绑定，扁平键被静默忽略 |
| DOC-05 | 低 | 已修复 | 文档称 multipart 超过内存上限的部分一律落临时文件、multipart 无隐式上限；非文件表单值实际直接报 `multipart: message too large` |
| DOC-06 | 低 | 已修复 | 文档称闭包 handler 在 default 告警中降级为函数名；实测普通闭包仍输出 `文件:行号` |
| DOC-07 | 低 | 已修复 | 文档称 `[][]time.Time` / `*[]time.Time` 标签不生效时“字段保持零值”；实测外层切片会被替换为非 nil 的零元素切片 |
| DOC-08 | 低 | 已修复 | `DefaultResponseHandler` 源码注释称“自定义 Content-Type”会跳过默认响应；仅设置响应头不标记 written，Content-Type 会被覆盖 |
| DOC-09 | 低 | 已修复 | 路由文档称运行期注册“会触发 fatal error”；源码注释表述为数据竞争/未定义行为，二者强度不一致 |

## 三、逐条详情

### DOC-01：指针指向切片的 `default` 静默失效，且 OpenAPI 展示与实际校验相反

- 级别：中
- 类型：实现缺陷（文档契约、运行时填充、OpenAPI 输出三者不一致）
- 文档证据：`zchttp/docs/parameter-binding.md:290` 与 `zchttp/docs/parameter-binding.md:294` 明确 `default` 仅支持标量、标量指针与标量切片，范围之外“注册期仅输出 slog.Warn”“误用字段将永不填充默认值”；`zchttp/docs/request.md:202` 也把“任意非标量的指针/切片/数组”列为不支持并承诺告警；`zchttp/docs/request.md:377` 声称 OpenAPI 对 `default` 的展示规则“与填充保证一致”。
- 实现证据：`zchttp/defaults.go:512-533` 的 `isDefaultSupportedDepth` 对 `Ptr` 无条件递归，使 `*[]int`、`[]*[]int` 被判定为“支持”；`zchttp/defaults.go:193-215` 的注册/请求阶段只按该判定填充；`zchttp/binding.go:226-239` 仅在字段本身是 `Slice` 时展开逗号分隔值，`*[]int` 走 `setScalar`，`zchttp/binding.go:256-268` 的 `Ptr` 分支递归到 `Slice` 后返回 unsupported，最终不写入；`zchttp/openapi.go:1026-1037` 仅凭 `isDefaultSupported` 与可达性输出 `default`。无测试覆盖该组合：`zchttp/defaults_test.go:272-306` 的类型表未包含 `*[]int`。
- 实测：`type Req struct { Tags *[]int \`json:"tags" default:"1,2"\` }`，POST 空请求体返回 200、`tags:null`，注册期告警为空；`components` 中该字段为 `"default":[1,2]`。再加 `nonzero:"true"` 后，POST 空请求体返回 400 `field "v" is required`，而 schema 仍是 `"default":[1,2]` 且无 `required`。`[]*[]int` 带 `default:"1,2"` 时运行时得到长度 2、元素全 nil 的切片，OpenAPI 却展示 `[[1],[2]]`。
- 影响：配置看似生效、文档也宣称生效，实际既不填充也不告警；带 `nonzero` 时客户端按文档省略该字段会被运行期 400 拒绝，形成“文档说可选、运行时必填”的公开契约割裂。
- 推荐方案：首选修正 `isDefaultSupportedDepth`——`Ptr` 只允许递归到标量（拒绝 `Ptr -> Slice/Array/Map`），使其落入既有的 unsupported 告警分支，同时 OpenAPI 不再展示不可生效的 default，与 `parameter-binding.md` 的已声明支持范围一致。若希望真正支持 `*[]T`，则需让 `setFieldValue` 在指针目标为切片时按元素展开并整体写回，并同步扩充文档与 OpenAPI display 规则。两种方案都需补 `*[]int`、`[]*[]int`、`*[]int + nonzero + default` 的回归用例。

### DOC-02：GET/DELETE/HEAD 的 OpenAPI query 参数包含运行时不可绑定的字段

- 级别：中
- 类型：实现缺陷（生成的文档契约与运行时绑定能力不一致）
- 文档证据：`zchttp/docs/openapi.md:214` 声明 GET/DELETE/HEAD 仅扁平字段（标量、切片、指针标量、`time.Time`）参与 query 生成，理由是“query 绑定仅处理扁平字段，展示无法绑定的参数会误导 API 使用者”；`zchttp/docs/parameter-binding.md:173` 只承诺标量切片可绑定，`zchttp/docs/parameter-binding.md:178` 明确嵌套结构体仅 JSON 路径支持，`zchttp/docs/parameter-binding.md:180` 承诺转换失败时“保持绑定前原值”。
- 实现证据：`zchttp/openapi.go:421-432` 仅对指针做一次解引用，然后只跳过 `Map` 与（非 `time.Time` 的）`Struct`；`[]Struct`、`[N]T`、`*[]T`、`*[N]T` 都会进入 query 参数。运行时 `zchttp/binding.go:226-239` 对字段本身不是 `Slice` 的容器走 `setScalar`，`zchttp/binding.go:256-268` 对数组/指针指向的切片返回 unsupported；`[]Struct` 则因外层是 `Slice` 而被替换成“长度等于参数个数、元素全零”的切片。现有测试 `zchttp/openapi_test.go:2713-2735` 只覆盖 ignore/文件/map/结构体跳过，未覆盖容器与数组。
- 实测：`Items []Inner`、`PtrSlice *[]int`、`Arr [2]int`、`PtrArr *[2]int` 均出现在 GET 的 `parameters` 中；请求 `?items=x&ptrSlice=1&arr=1&arr=2&ptrArr=3` 后，`items` 变为长度 1 的全零元素切片，`ptrSlice`/`ptrArr` 仍为 nil，`arr` 保持 `[0,0]`。`[]int`、`[]time.Time` 等真正可绑定的切片正常。
- 影响：生成的 OpenAPI 暴露无法使用的参数，客户端据此生成调用代码后运行期静默丢值；`[]Struct` 还会把模板的 nil 切片改写成零值元素切片，与“失败保持原值”的承诺不符。
- 推荐方案：`buildQueryParams` 改用与绑定能力一致的判定（`isScalarType(f.Type)`，或字段为切片且元素为 `isScalarType`），跳过 `[]Struct`、数组与指针包裹的容器；同时让 `setFieldValue` 在切片元素类型不受支持时不要赋值（可先判定元素类型或让 `setScalar` 返回 supported 标志），避免零值元素写回。补 GET 文档与运行时的对照回归用例（至少覆盖 `[]Struct`、`[N]int`、`*[]int`）。

### DOC-03：带 `json` 名的匿名嵌入内层 `nonzero` 未被父 schema 标为 required

- 级别：中
- 类型：实现缺陷（OpenAPI required 推断与运行时校验不一致）
- 文档证据：`zchttp/docs/openapi.md:72` 声明带 `json` 名的匿名嵌入“作为嵌套对象”，且“请求 schema 的 required 列出受 nonzero 校验的字段的 JSON 名”；`zchttp/docs/parameter-validate.md:66` 说明匿名嵌入字段没有“父字段判零”环节，其内部 nonzero 等价于顶层必填。
- 实现证据：`zchttp/meta.go:171-197` 把匿名嵌入（无论是否带 json 名）的字段扁平展开进 `structMeta`，运行时 `zchttp/validate.go:188-216` 按扁平后的索引逐个校验；而 `zchttp/openapi.go:764-807` 的 `jsonFields` 对带 json 名的嵌入输出父级属性（索引 `[i]`）与独立 `$ref`，`zchttp/openapi.go:686-694` 的 `requiredAt` 以扁平索引（如 `[0,0]`）记录，`zchttp/openapi.go:714-722` 在父级按 jsonFields 索引（如 `[0]`）匹配，二者永远无法命中。现有测试 `zchttp/openapi_test.go:4036-4098` 覆盖了带 json 名嵌入的属性/默认值，但内层字段没有 nonzero，未触及该组合。
- 实测：`type Base struct { Name string \`json:"name" nonzero:"true"\` }; type Req struct { Base \`json:"base"\` }`。生成的 `Req` schema 属性只有 `base` 且无 `required`，`Base` schema 内有 `"required":["name"]`；POST 空对象 `{}` 返回 400 `field "name" is required`。即按父 schema 省略 `base` 是“合法”的，运行期却拒绝。
- 影响：按 OpenAPI 生成的客户端可合法省略该嵌入属性，运行期稳定 400；契约无法表达“该嵌入对象一旦参与就必须满足其内部必填项”的语义。
- 推荐方案：在 `registerStructSchema` 处理带 json 名的嵌入属性时，对该属性对应类型的扁平 nonzero 子树执行一次 `validateNonzero`（以默认模板为初始状态）；不通过则把该属性名加入父级 `required`。同时为“值嵌入 / 指针嵌入 × 内层 nonzero / 仅 default 可满足”组合补回归测试。

### DOC-04：`request.md` 对匿名嵌入的 JSON 扁平化承诺过宽，带 json 名的嵌入实际按嵌套对象绑定

- 级别：中
- 类型：文档错误（跨文档矛盾，且会静默丢数据）
- 文档证据：`zchttp/docs/request.md:29` 声称匿名嵌入字段“对齐 encoding/json 的扁平语义”，`Base` 的导出字段“query / form / JSON / 路径参数均可按字段绑定名直接绑定”。
- 实现证据：`zchttp/meta.go:171-197` 对 query/form/路径参数确实扁平展开；但 JSON 请求体由 `encoding/json` 直接解码，`zchttp/openapi.go:764-807` 的 `jsonFields` 对带 json 名的嵌入按嵌套对象输出，二者都不认父级扁平键。`zchttp/docs/openapi.md:72` 对此有正确描述（“带 json 名的匿名嵌入作为嵌套对象”），故两份文档互相矛盾。
- 实测：`type Base struct { Name string \`json:"name" form:"name"\` }; type Req struct { Base \`json:"base"\` }`。GET `?name=q` 得到 `name:"q"`；POST `{"name":"flat"}` 返回 200 但 `name:""`（静默忽略）；POST `{"base":{"name":"nested"}}` 得到 `name:"nested"`；OpenAPI 的 query 参数名是扁平 `name`，body schema 属性是嵌套 `base`。
- 影响：开发者按 `request.md` 写扁平 JSON 键时请求照样 200，字段却为空；若同时标注 `nonzero`，则表现为难以定位的 400。
- 推荐方案：首选改文档——在 `zchttp/docs/request.md:29` 明确“query / form / 路径参数扁平展开；JSON 遵循 encoding/json：无 json 名的嵌入展开，带 json 名的嵌入作为嵌套对象”，并与 `openapi.md` 交叉引用。若希望 JSON 也扁平绑定，则需在绑定层改为自定义解码（影响面大，不推荐）。

### DOC-05：multipart 非文件字段超内存上限直接报错，不会落临时文件

- 级别：低
- 类型：文档错误
- 文档证据：`zchttp/docs/parameter-binding.md:54` 称 multipart 超过 `MultipartFormMaxMemory` 的部分“由标准库写入临时文件”；`zchttp/docs/parameter-binding.md:66` 再次称该字段“仅控制内存缓冲上限（超出部分写入临时文件）”；`zchttp/docs/parameter-binding.md:64` 在 `MaxBodyBytes=0` 时称“JSON 与 multipart 无此隐式上限”。
- 实现证据：`zchttp/binding.go:89-97` 直接调用 `r.ParseMultipartForm`。标准库 `GOROOT/src/mime/multipart/formdata.go:27-31` 与 `GOROOT/src/mime/multipart/formdata.go:88-105` 显示：内存预算为 `maxMemory + 10 MB`（保留给非文件部分），只有文件部分会落盘；`GOROOT/src/mime/multipart/formdata.go:144-150` 对非文件值超过预算返回 `ErrMessageTooLarge`。
- 实测：`MaxBodyBytes=0`、`MultipartFormMaxMemory=1<<10`，发送单个 11 MB 的非文件字段 `blob`，请求返回 400，消息为 `multipart: message too large`。文件字段超限落盘由 `zchttp/binding_test.go:2421-2474` 覆盖；`zchttp/binding_test.go:2710-2740` 只覆盖 urlencoded 10 MB 与 JSON 场景，未覆盖该分支。
- 影响：把大块文本/Base64 放在普通表单字段而非文件字段时，按文档预期可落盘，实际直接 400；`MaxBodyBytes=0` 也并非“multipart 无上限”（默认配置下非文件部分仍受 32 MB + 10 MB 约束）。
- 推荐方案：修正文档措辞——仅“文件部分”超出 `MultipartFormMaxMemory` 时落盘；非文件部分共享 `MultipartFormMaxMemory + 10 MB` 预算，超出即绑定失败（400）。同时补充该场景的回归用例。

### DOC-06：闭包 handler 的告警位置并非“降级为函数名”

- 级别：低
- 类型：文档错误（诊断细节）
- 文档证据：`zchttp/docs/request.md:281` 称 default 警告的 `handler` 字段输出定义位置 `文件:行号`，“闭包 handler 降级为函数名”。
- 实现证据：`zchttp/defaults.go:49-52` 仅当 `handlerFile == "<autogenerated>"` 时才降级为 `handlerName`；普通闭包由 `runtime.FuncForPC` 返回真实的文件与行号。`zchttp/defaults_test.go:385-392` 锁死的正是 `<autogenerated>` 分支，而非“闭包”分支。
- 实测：以闭包注册 handler 并在匿名嵌入上放置 `default` 标签，警告输出为 `handler=D:/code/buexplain/zckg/zchttp/zz_probe_test.go:71`，不是函数名。
- 影响：排查启动告警时按文档预期寻找函数名会找不到；影响有限，但属于明确的诊断描述错误。
- 推荐方案：把文档改为“普通 handler 与闭包均输出 `文件:行号`；仅当运行时拿不到文件信息（`<autogenerated>`）时降级为函数名”。

### DOC-07：多层时间容器标签不生效时“字段保持零值”的表述与实测不符

- 级别：低
- 类型：文档错误（措辞）
- 文档证据：`zchttp/docs/parameter-binding.md:213` 称 `[][]time.Time`、`*[]time.Time` “绑定链路不支持多层容器与切片指针，标签不生效（注册期不会报错，字段保持零值）”。
- 实现证据：`zchttp/binding.go:226-239` 对字段本身为 `Slice` 的 `[][]time.Time` 仍会创建并写回外层切片，元素转换 unsupported 时只跳过元素；`zchttp/binding.go:256-268` 才是不支持类型的静默分支。`zchttp/meta_test.go:557-612` 只锁死了标签不被解析，未锁死字段最终状态。
- 实测：`type Req struct { Grid [][]time.Time \`json:"grid" time_format:"unix"\` }`，GET `?grid=1600000000` 后 `Grid` 为 `nil=false`、外层长度 1、内层长度 0，并非“零值”（nil）。`*[]time.Time` 字段则保持 nil。
- 影响：调用方若依赖“字段保持 nil/零值”做判断，会观察到非 nil 的空容器；实际数据仍为空。
- 推荐方案：与 DOC-02 一并修复 `setFieldValue`——元素类型不受支持时不要写回切片，使字段真正保持绑定前值；若只改文档，则应改为“外层容器可能被创建为同长度的零元素切片，标签对元素不生效”。

### DOC-08：`DefaultResponseHandler` 注释称“自定义 Content-Type”会跳过默认响应

- 级别：低
- 类型：源码注释错误（与行为、用户文档矛盾）
- 文档证据：`zchttp/docs/http-engine-callback.md:126` 明确“仅调用 Header().Set(...) 不标记 written，因此 CORS 等只设置响应头的中间件不会导致默认 JSON 响应被跳过”。
- 实现证据：`zchttp/httpEngine.go:130-131` 的注释写作“若 handler 已写入响应（如文件下载、自定义 Content-Type 等）则跳过”；实际跳过条件只有 `IsResponseWritten(w)`（`zchttp/httpEngine.go:134-137`），而设置响应头不置位 written（`zchttp/responseWriter.go:126-143`）。
- 实测：handler 仅执行 `w.Header().Set("Content-Type", "text/plain")` 后返回 Res，响应为 200、`Content-Type: application/json`、统一 JSON 包体（Content-Type 被覆盖）。
- 影响：按注释理解，可能误以为设置 Content-Type 就能接管响应输出，实际仍会被默认回调覆盖；属于会误导维护者的注释缺陷。
- 推荐方案：把注释改为“若 handler 已写入响应（如文件下载、调用过 Write/Flush 等）则跳过；仅设置响应头不会跳过，且自定义 Content-Type 会被默认回调覆盖”。不改行为。

### DOC-09：运行期注册导致崩溃的确定性表述与源码注释不一致

- 级别：低
- 类型：文档错误（绝对化措辞）
- 文档证据：`zchttp/docs/routing.md:351` 称运行中调用注册方法“会触发 Go map 并发读写导致进程崩溃（fatal error: concurrent map read and map write）”。
- 实现证据：`zchttp/router.go:11` 的源码注释表述为“注册必须在服务启动前完成，运行期并发注册属未定义行为”。注册会同时修改 `trees` 的 map 与 `routes` 切片，并发读取属于数据竞争；并发 map 读写通常触发 fatal error，但并非语言层面保证每次必现。
- 实测：未做探针。该结论依赖 Go 内存模型与运行时实现，数据竞争结果本身不可确定性复现，强行构造探针只会得到“某次运行是否崩溃”的偶然结果，无法作为稳定判据。
- 影响：把“未定义行为”写成“必然崩溃”，会低估风险（数据竞争下可能出现错误路由/内存不一致而非立即崩溃）。
- 推荐方案：与源码注释对齐，改为“属数据竞争（未定义行为），可能触发 fatal error 或产生不一致路由；必须在启动前完成注册”。

## 四、已核对且未发现偏差的关键行为

以下行为经源码比对与运行/既有测试核对，未发现文档与实现偏差，修复时请勿改动：

1. **`Any` 注册原子性**：先对 9 个方法完整预检并在克隆树上插入，冲突时 panic 且不残留任何方法；正常注册 9 个方法全部命中（探针实测冲突后仅原 GET 存在，`Any` 正常路径 9/9）。
2. **路由匹配优先级**：静态 > 参数 > 通配尾段、跨层静态优先、可选省略 > 通配零段、通配捕获去掉一个前导 `/` 且不折叠内部斜杠；`/a//b` 捕获 `/b`、`/a/b/` 捕获 `b`、`/a/` 零段命中，均与 routing.md 一致。
3. **路径/查询归一化**：补前导 `/`、去尾部 `/`、内部空段注册 panic、请求空段不命中参数；通配尾段可跨空段。
4. **三级覆盖链 path > body > query**：`zchttp/binding_test.go:547` 起已锁死；JSON `null` 对指针/非指针的覆盖粒度与文档表一致（`zchttp/docs_deviation_review_test.go:81`）。
5. **KeepRawBody**：仅顶层值嵌入识别、文件字段冲突注册 panic、三种 body Content-Type 均只绑定 query 且 body 可完整读回；OpenAPI 通用 `*/*` requestBody 与 GET 等价输出。
6. **错误分发**：`*BindingError` / `*ValidationError` → `OnValidationError`（400），其余 → `OnError`（500）；typed-nil 归一化、`errors.Is/As` 穿透与 `ValidationError.Error()` 的三种格式均一致。
7. **written 语义**：`WriteHeader`（除 101 外 1xx 不标记）、`Write`（含 0 字节）、`ReadFrom`、`Flush`、`Hijack` 标记；`Push` 不标记；`Header().Set` 不标记；重复 `WriteHeader` 被包装层拦下；`Unwrap` 可穿透 `http.ResponseController`。`ReadFrom` 由 `zchttp/docs_deviation_review_test.go:54-66` 锁死。
8. **默认 JSON 编码**：`SetEscapeHTML(false)`，`<`、`>`、`&` 原样输出（`zchttp/httpEngine_test.go:786-810`）；编码失败不写 JSON 片段并返回 500 text/plain。
9. **请求体上限**：`NewEngine` 默认 `MaxBodyBytes` 与 `MultipartFormMaxMemory` 均为 32 MB；`MaxBodyBytes=0` 时 urlencoded 仍受 ParseForm 10 MB 限制；超限绑定失败映射 400。
10. **默认值与容器矩阵**：单层容器指针 default 请求阶段补填、多层容器不穿透、固定数组注册期填充、`*Struct` 值字段不生效/指针字段补填、`map` 值写回、深拷贝并发隔离，均与 request.md / parameter-binding.md 的矩阵一致。
11. **nonzero 校验**：传递性跳过标记、固定数组零值仍逐元素、单层容器与指针包裹、错误路径为绑定名（map 附 key）、`Validate()` 仅顶层调用、typed-nil 视为通过。
12. **中间件**：洋葱顺序、`next` 只允许一次（含 >64 层递归回退）、w/r 替换后 Context 访问器与链后回调使用最近一次对象、`r.WithContext` 注入值对下游可见、短路不执行 handler。
13. **Context 访问器**：`BoundReqFromContext` 值类型浅拷贝/指针类型同实例、绑定错误返回、类型不符与缺失统一 `ErrBoundReqNotFound`；`BoundResFromContext` 仅 `next()` 返回后可用。
14. **OpenAPI 结构与确定性**：静态/参数/通配路径转换、path 参数不受 `ignore` 影响、optional/catchAll `required:false`、请求/响应 schema 双角色与 `<Type>Response` 命名、`Response_<Type>` 包装、`ResponseWrapper` 校验 panic、map/指针/时间类型映射、同名字段消解；同一路由多次生成输出字节级一致（探针连续 5 次一致）。

## 五、修复执行记录

修复日期 2026-10-17；基线 `8e98521c4b5bc32837cdb259aa3e84af0ddc2b4e`。按 P1 → P2 逐条修复，仅本地改动，**未提交、未推送**。

| 编号 | 修复内容 | 涉及文件 |
|---|---|---|
| DOC-01 | `isDefaultSupportedDepth` 收紧为“标量 / 标量指针链 / 标量切片”：`Ptr`、`Slice` 只允许解引用到标量，拒绝 `*[]T`、`[][]T`、`[]*[]T`、`[N]T`。这些类型现在按不支持类型告警、运行时不填充、OpenAPI 不展示 default；同时 `nonzero + default` 重新推断为 required，消除“文档可选、运行时 400”。 | `defaults.go`、`docs/parameter-binding.md`、`docs/request.md`、`docs/openapi.md`、`defaults_test.go`、`openapi_test.go` |
| DOC-02 | 新增 `isQueryBindableType`：仅标量（含指针链）与元素为标量的切片可通过 query 生成；`buildQueryParams` 改用它。`setFieldValue` 对不支持的切片元素类型直接跳过、不再写回全零元素。 | `binding.go`、`openapi.go`、`binding_test.go`、`openapi_test.go` |
| DOC-03 | `registerStructSchema` 新增 `requiredEmbedPrefixes`：扁平化路径上存在无法由默认值满足的 nonzero 字段时，把对应的带 json 名匿名嵌入属性加入父级 `required`（支持值嵌入、指针嵌入与多层未命名嵌入）。 | `openapi.go`、`docs/openapi.md`、`openapi_test.go` |
| DOC-04 | `request.md` 明确嵌入语义：query / form / 路径参数扁平展开；JSON 遵循 encoding/json，带 json 名的嵌入为嵌套对象、父级扁平键无效。 | `docs/request.md`、`binding_test.go` |
| DOC-05 | 修正 multipart 描述：仅文件部分可落盘；非文件值共享 `MultipartFormMaxMemory + 10 MB` 预算，超出返回 400；`MaxBodyBytes=0` 时 multipart 仍受该预算约束。 | `docs/parameter-binding.md`、`binding_test.go` |
| DOC-06 | `request.md` 改为：普通 handler 与闭包均输出 `文件:行号`，仅 `<autogenerated>` 降级为函数名。 | `docs/request.md`、`defaults_test.go` |
| DOC-07 | 由 DOC-02 的 `setFieldValue` 修复实现：不支持的容器元素不再写回，字段保持绑定前值；文档措辞同步为“保持绑定前值/零值，不会被写入”。 | `binding.go`、`docs/parameter-binding.md`、`binding_test.go` |
| DOC-08 | `DefaultResponseHandler` 注释改为“写响应才跳过；仅设置响应头不跳过，自定义 Content-Type 会被覆盖”。 | `httpEngine.go`、`docs_deviation_review_test.go` |
| DOC-09 | `routing.md` 弱化为“数据竞争/未定义行为，可能触发 fatal error”，与 `router.go` 注释一致。 | `docs/routing.md` |

新增/扩展的回归用例：

- `TestIsDefaultSupported`（扩展 `*[]int`、`[]*[]int`、`[][]int`、`*[2]int`、`[2]int` 等边界）
- `TestDefault_PointerToSliceUnsupported`
- `TestGenerateOpenAPI_DefaultSkipsPointerToSlice`
- `TestSetFieldValue_UnsupportedContainerKeepsOriginal`
- `TestBindValues_QuerySkipsUnsupportedContainers`（同时锁死 DOC-07 的多层时间容器）
- `TestGenerateOpenAPI_QuerySkipsNonBindableContainers`
- `TestGenerateOpenAPI_JSONNamedEmbedRequired`
- `TestJSONNamedEmbedFlatKeyIgnored`
- `TestBindMultipartValueOverMemoryLimit`
- `TestCheckUnsupportedDefaults_ClosureHandlerLocation`
- `TestDocReview_ContentTypeOnlyDoesNotSkip`

**可失败性验证（overlay 回退修复，仓库未回滚）**：

1. 回退 `isDefaultSupportedDepth` → `TestIsDefaultSupported`、`TestDefault_PointerToSliceUnsupported`、`TestGenerateOpenAPI_DefaultSkipsPointerToSlice` 均失败（如 `isDefaultSupported([]*[]int) = true`）。
2. 回退 `buildQueryParams` 过滤与 `setFieldValue` 守卫 → `TestSetFieldValue_UnsupportedContainerKeepsOriginal`、`TestBindValues_QuerySkipsUnsupportedContainers`、`TestGenerateOpenAPI_QuerySkipsNonBindableContainers` 均失败。
3. 回退 `requiredEmbedPrefixes` 分支 → `TestGenerateOpenAPI_JSONNamedEmbedRequired` 对值嵌入、指针嵌入、多层嵌入三个 schema 均报 `required = <nil>`。

## 六、实际验收结果

| 验收项 | 命令 | 结果 |
|---|---|---|
| 格式 | CRLF 归一化后对 `zchttp/*.go` 逐文件比对 gofmt 输出 | 全部一致 |
| 空白 | `git diff --check -- zchttp` | exit 0 |
| 全仓测试 | `go test ./... -count=1` | zcconfig / zcdb / zchttp / zcmodel / zcquit 全部 ok（zcdb 66.3s） |
| 静态检查 | `go vet ./...` | exit 0 |
| 竞态检测 | `go test -race ./zchttp -count=1` | ok（10.956s） |
| 乱序重复 | `go test ./zchttp -shuffle=on -count=5` | ok（7.863s） |
| 覆盖率 | `go test ./zchttp -cover -count=1` | **99.6%** |
| 定向用例 | 第五节点名的全部用例 | 全部通过 |
| 变异验证 | 三次 overlay 回退 | 三次均按预期变红 |

结论：本次 9 项问题全部修复并通过验收，未发现与本次改动相关的失败。审查/修复期间 `zcdb` / `zcmodel` 目录另有并行改动，未纳入本报告修改范围；`zchttp` 无外部依赖，测试不存在因数据库不可达而 Skip 的情况。
