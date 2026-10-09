# zchttp 文档与实现一致性审查报告

审查日期：2026-10-09

## 一、审查范围与方法

本次逐份审查以下 7 份文档，并反向核对 `zchttp` 的全部公开 API 与关键运行时行为：

- `routing.md`
- `parameter-binding.md`
- `parameter-validate.md`
- `request.md`
- `middleware.md`
- `http-engine-callback.md`
- `openapi.md`

证据以当前工作区源码和测试为准，主要对照 `router.go`、`router_trie.go`、`binding.go`、`defaults.go`、`validate.go`、`meta.go`、`middleware.go`、`context.go`、`httpEngine.go`、`responseWriter.go`、`openapi.go` 及对应测试。除静态逐行比对外，还定向实测了分组路径、中间件继承、固定数组默认值、非法指针参数、Content-Type 大小写、尾随 JSON、负时间戳、中间件替换、根路径重复斜杠和零字节写入。

初次审查共确认 **21 个问题**：高优先级 3 个、中优先级 12 个、低优先级 6 个。问题既包括文档与实现直接矛盾，也包括足以误导调用方的关键行为遗漏。单纯未介绍内部优化、但不影响公开契约的内容不计入问题。

**修复状态（2026-10-09）：DOC-01～DOC-21 已全部修复并完成回归验证。** 下文第三节保留的是修复前诊断、旧实现证据与当时的推荐方案，行号也指向修复前基线，仅用于追溯，不代表当前实现仍存在这些缺陷；当前契约以正式文档和源码为准。

## 二、问题汇总

| 编号 | 级别 | 状态 | 最终处理结果 |
| --- | --- | --- | --- |
| DOC-01 | 高 | 已修复 | 标量先解析到临时值，成功后原子提交，失败不分配指针或覆盖原值 |
| DOC-02 | 高 | 已修复 | JSON body 只允许一个完整值，尾随值或垃圾返回绑定错误 |
| DOC-03 | 中 | 已修复 | Content-Type 改用 `mime.ParseMediaType` 解析并按规范匹配主类型 |
| DOC-04 | 中 | 已修复 | 自动时间戳精度忽略负号并使用 `time.Unix*` 标准单位构造 |
| DOC-05 | 中 | 已修复 | 固定数组始终递归校验现存元素，文档与测试拆分数组/切片语义 |
| DOC-06 | 低 | 已修复 | 文档明确匿名嵌入展开及多级 `indices` |
| DOC-07 | 中 | 已修复 | 分组前缀与子路径统一安全拼接，省略前导 `/` 不再产生错路由 |
| DOC-08 | 中 | 已修复 | 子组保留父链引用，路由注册时展开祖先中间件并快照 |
| DOC-09 | 低 | 已修复 | `Any` 先完整预检 9 个方法，全部通过后原子提交 |
| DOC-10 | 低 | 已修复 | 文档补充连续尾斜杠及纯斜杠路径归一为根路径的例外 |
| DOC-11 | 高 | 已修复 | 每次 `next(w, r)` 同步当前对象，Context 访问器与全部链后回调统一 |
| DOC-12 | 中 | 已修复 | 文档按“写操作发生”描述 written，并覆盖零字节与底层错误 |
| DOC-13 | 中 | 已修复 | 请求体收尾前移，所有请求（包括 404）均限量排空并关闭 |
| DOC-14 | 中 | 已修复 | `OnPanic` 外增加最终 recover，二次 panic 记录后兜底 500 |
| DOC-15 | 低 | 已修复 | 不支持的 default 统一为注册期 Warn、运行时不填充 |
| DOC-16 | 中 | 已修复 | map 文档限定为 `encoding/json` 实际支持范围 |
| DOC-17 | 低 | 已修复 | 文档记录切片/数组省略索引、map 带 key 的校验路径格式 |
| DOC-18 | 低 | 已修复 | default/required 标题、正文与锚点统一覆盖 Req 与 Res |
| DOC-19 | 中 | 已修复 | required 依据受支持并被元数据识别的 default 推断 |
| DOC-20 | 中 | 已修复 | `requestBody.required` 通过默认 Req 模板的内建 nonzero 结果推导 |
| DOC-21 | 中 | 已修复 | ResponseWrapper 非法配置 fail-fast，并完整记录字段与占位符约束 |

## 三、修复前详细问题、解释与解决方案

### DOC-01：非法 query/form 指针值会绕过 `nonzero`

**级别：高；类型：实现缺陷 + 文档错误。**

- 文档证据：`parameter-binding.md:174` 声称单字段转换失败时“跳过该字段（保持零值）”；`parameter-binding.md:390` 又称解析失败保留默认值。
- 实现证据：`binding.go:237-245` 在解析前逐层分配指针，随后才在 `binding.go:250-278` 做类型转换；`bindValues` 在 `binding.go:145-146` 丢弃转换错误。
- 校验证据：`validate.go:192-202` 以指针本身的 `IsZero()` 判断 `nonzero`，非 nil 的 `*int(0)` 被视为非零。

例如字段 `Page *int \`json:"page" nonzero:"true"\`` 收到 `?page=bad` 时，当前结果是 `Page != nil && *Page == 0`，绑定不报错，`nonzero` 也通过。这既不是“保持零值 nil”，也会让非法输入进入业务 handler。

**影响：** 必填指针参数可被格式错误的值绕过；布尔、整数、浮点、时间及多层指针都可能受影响。若字段原本带默认指针，则解析失败通常保留默认值，导致同一失败在“有默认值”和“无默认值”时表现不同。

**推荐方案：**

1. `setScalar` 先在临时可设置值上完成全部解引用、解析与赋值，成功后再一次性写回目标字段；失败时目标值保持原样。
2. 保留“query/form 尽力绑定”设计时，非法无默认指针应保持 nil，从而由 `nonzero` 拦截；若希望严格输入，则让 `bindValues` 汇总或立即返回转换错误，并统一包装为 `BindingError`。
3. 增加 `*int`、`**int`、`*bool`、`*time.Time` 的非法值测试，同时覆盖有/无 default、带/不带 nonzero。
4. 文档按最终设计明确“失败保持原值”或“失败返回 400”，不要再使用含糊的“保持零值”。

### DOC-02：JSON 尾随内容未被校验

**级别：高；类型：实现缺陷 + 关键行为遗漏。**

- 文档证据：`parameter-binding.md:34` 将 `application/json` 描述为解析请求体，没有说明只读取第一个 JSON 值。
- 实现证据：`binding.go:83-89` 与 `binding.go:107-114` 均只调用一次 `json.Decoder.Decode`。

定向实测 `{"page":1}{"page":2}`：绑定成功且 `Page == 1`，第二个 JSON 值被忽略；首个 JSON 后的其他非空尾随内容同样可能不进入业务对象。

**影响：** 网关、签名层、审计层与业务层可能对同一请求体作出不同解释；客户端误发拼接 JSON 时也不会得到预期的 400。

**推荐方案：** 首次 Decode 成功后再次 Decode 到空值，只有返回 `io.EOF` 才视为完整合法 JSON；第二次成功或返回其他错误都应作为绑定失败。对显式 `application/json` 与“其他类型回退 JSON”两条分支复用同一 helper，并测试双 JSON、尾随空白、尾随垃圾和空 body。

### DOC-03：Content-Type 匹配大小写敏感

**级别：中；类型：实现兼容性缺陷 + 文档遗漏。**

- 文档证据：`parameter-binding.md:30-50` 只说明去除参数部分，未声明仅接受小写主类型。
- 实现证据：`binding.go:71-78` 手工截取分号并 Trim，随后在 `binding.go:79-96` 对字符串做大小写敏感的精确匹配。

合法的 `Application/X-Www-Form-Urlencoded` 会落入 JSON 回退分支并解析失败；multipart 的混合大小写也不会按表单处理。

**推荐方案：** 使用 `mime.ParseMediaType` 解析，并对主类型做标准化后匹配；解析失败时返回明确的绑定错误或按既定回退策略处理。补大小写、带引号参数、额外空白、非法 media type 测试。若刻意保持现状，文档必须明确只识别表中的小写字面量，但不推荐。

### DOC-04：负自动时间戳的精度判断错误

**级别：中；类型：实现缺陷 + 文档/注释不一致。**

- 文档证据：`parameter-binding.md:208-210` 声明按数字位数判断 10/13/16/19 位精度。
- 实现证据：`binding.go:355-370` 明确接受首位负号；`binding.go:339-352` 却直接用包含负号的 `len(value)` 选择精度。

`-1000000000000` 有 13 个数字，但字符串长度为 14，会按秒而非毫秒处理；随后 `setUnix` 在 `binding.go:328-336` 通过 `n * int64(unit)` 换算纳秒，还可能溢出并生成完全错误的日期。

**推荐方案：** 位数判断前剥离可选符号；时间换算优先使用 `time.Unix`、`time.UnixMilli`、`time.UnixMicro`、`time.Unix(0, n)`，避免统一乘纳秒造成溢出。补四种精度的正数、负数、边界值和溢出测试，并把“符号不计入位数”写入文档。

### DOC-05：固定数组与切片行为并不一致

**级别：中；类型：文档错误 + 实现语义不规则。**

- 文档证据：`request.md:180-187`、`parameter-validate.md:16`、`parameter-binding.md:367` 均声明固定数组与切片行为一致，数组元素值字段的 default 不生效。
- 默认值实现：`defaults.go:245-263` 在注册阶段会遍历已经存在的 `[N]Struct` 每个元素，因此值字段 default 可预填；nil 切片在注册阶段没有元素可遍历。
- 校验实现：`validate.go:199-203` 在进入容器前先跳过零值字段。全零 `[N]Struct` 是零值，会整体跳过；非 nil 的空元素切片不是零值，会进入 `validate.go:216-238` 逐元素校验。

因此数组至少存在两处差异：未传数组可在注册模板中获得元素值 default；全零数组可能跳过元素 `nonzero`，而含一个零元素的切片会校验失败。

**推荐方案：** 先确定期望契约。若要与切片一致，应避免注册阶段预填固定数组值元素，并让数组的递归判定不被外层 `IsZero` 提前截断；若保留当前行为，则拆分数组/切片矩阵，准确写明注册期和校验期差异。无论选择哪种方案，都要增加“字段缺失、空数组、部分元素、尾部零元素、值/指针元素”的对照测试。

### DOC-06：meta 的“顶层/单层 indices”描述与匿名嵌入矛盾

**级别：低；类型：文档内部矛盾。**

- 文档证据：`request.md:27-47` 正确说明匿名嵌入会展开为顶层字段；`parameter-validate.md:7` 却声称 `buildStructMeta` 只计算顶层字段且 `indices` 为单层。
- 实现证据：`meta.go:170-194` 实际递归展开匿名嵌入，并把父索引与子索引拼为多级 `indices`。

**推荐方案：** 将 `parameter-validate.md` 改为“具名嵌套结构体的 meta 按需构建；匿名嵌入在构建当前 meta 时递归展开，indices 可为多级”。`parameter-binding.md:172` 的“query/form 仅处理扁平字段”也应补充“匿名嵌入字段展开后视为扁平字段”。

### DOC-07：RouterGroup 子路径缺少 `/` 时静默注册错误路径

**级别：中；类型：关键约束遗漏。**

- 文档证据：`routing.md:295-320` 说明前缀规范化和自动拼接，但未声明子路径必须以 `/` 开头。
- 实现证据：`router.go:248-307` 直接使用 `g.prefix + path`，只对拼接结果执行后续路径规范化。

`r.Group("/api").GET("users", h)` 当前注册的是 `/apiusers`，而不是调用方通常预期的 `/api/users`；注册过程不会报错。

**推荐方案：** 注册时用统一的路径拼接函数，在 prefix 非空且 path 非空时保证只有一个 `/`；同时覆盖根子路径。若出于兼容性不能修改实现，应在 `routing.md` 明确子路径必须以 `/` 开头，并在注册时对缺失斜杠 panic，避免静默错路由。

### DOC-08：父分组后加中间件不会传播到已创建子分组

**级别：中；类型：文档表述过宽。**

- 文档证据：`routing.md:304-326` 与 `middleware.md:195-199` 表述为父中间件被继承、`Use` 对此后注册路由生效。
- 实现证据：`router.go:236-245` 在创建子分组时立即复制父组中间件；之后父组 `Use` 只修改父组自己的切片。

顺序 `child := parent.Group("/v1"); parent.Use(auth); child.GET(...)` 中，虽然路由在 `Use` 之后注册，子路由仍没有 `auth`。

**推荐方案：** 文档明确两个快照时点：创建子组时快照父组链，注册路由时再快照当前组链；建议写明“先对父组 Use，再创建子组”。若希望符合“此后注册路由”的直觉，可让子组保留父链引用并在路由注册时展开，但需处理并发注册约束和重复继承。

### DOC-09：Any 不是原子注册

**级别：低；类型：关键边界遗漏。**

- 文档证据：`routing.md:15-30` 将 `Any` 描述为一次注册 9 个方法，任一方法冲突时 panic。
- 实现证据：`router.go:214-218` 与 `router.go:302-307` 逐个调用 `register`，每次立即写入树和 routes。

若中途某个 method 冲突，冲突前的方法已经成功注册。调用方 recover 后继续使用 Router，会得到部分注册状态。

**推荐方案：** 注册前对 9 个方法执行完整预检，全部可注册后再提交；或明确规定任何注册 panic 后 Router 状态不可继续使用。优先推荐原子预检，并增加中段 method 冲突后的零残留测试。

### DOC-10：重复斜杠规则遗漏根路径例外

**级别：低；类型：文档绝对化。**

- 文档证据：`routing.md:36-44` 声称多重斜杠不折叠，含空段路径注册时 panic。
- 实现证据：`router.go:322-335` 会把连续尾斜杠全部裁掉；`"//"`、`"///"` 最终归一为 `/`。

内部重复斜杠如 `/a//b` 的描述正确，但“所有多重斜杠”不正确。

**推荐方案：** 改为“内部重复斜杠不折叠；连续尾斜杠会被整体裁剪，因此仅由斜杠组成的路径归一为根路径”。补根路径边界表即可，无需修改实现。

### DOC-11：中间件替换的 Request/Writer 与 Context、回调不一致

**级别：高；类型：文档错误 + 公开行为割裂。**

- 文档证据：`middleware.md:63-83` 称 `next(w, r)` 替换值会传给下游，handler 最终收到最内层版本；`http-engine-callback.md:209` 称各回调收到的 w 都是引擎跟踪包装对象。
- 链实现：`middleware.go:113-122` 的确把替换值传给后续中间件和 final handler。
- Context 实现：`httpEngine.go:263-268` 在执行链前把原始 r/rw 固定到 `requestState`；`context.go:36-51` 始终返回这两个原始对象，从不随 `next` 更新。
- 回调实现：成功时 `httpEngine.go:362-366` 的 `OnResponse` 收到最终替换值；错误时 `httpEngine.go:373-380` 的 `OnError`/`OnValidationError` 又收到原始 r/rw；panic 回调同样使用原始对象。

业务 handler 并不直接接收 w/r，只能经 Context 获取，因此实际上仍看到原对象。若 gzip 中间件把 wrapped writer 传给 `next`，handler 经 `ResponseWriterFromContext` 写入原 writer，而 `OnResponse` 却对 wrapped writer 做 `IsResponseWritten`，可能误判未写入并追加默认 JSON。

**推荐方案：**

1. 明确定义唯一契约。推荐在进入 core 前把最终 r/w 同步到 `requestState`，使 handler Context 与 `OnResponse` 一致；错误和 panic 回调也应明确使用当前链对象还是引擎原对象。
2. 若必须保留原 Context 对象，文档应把替换能力严格限定为“只影响后续中间件和成功回调”，并禁止声称 handler 可通过 Context 得到替换值；同时说明自定义 writer 必须透传 `Written()`，否则默认回调无法判断状态。
3. 增加端到端测试，不只测试 `runChain` 的 final 参数：覆盖 RequestFromContext、ResponseWriterFromContext、OnResponse、OnError、OnPanic 及 handler 直接写响应。

### DOC-12：written 表示写操作已发生，不表示写入成功

**级别：中；类型：文档错误。**

- 文档证据：`http-engine-callback.md:118-120` 称依据是“是否真正写入过响应体或状态码”。
- 实现证据：`responseWriter.go:137-155` 在调用底层 `Write`/`ReadFrom` 前即置 `written=true`；零字节 Write 和底层返回错误也会标记。`Flush`/`Hijack` 同样在调用前标记。

**推荐方案：** 将文档改为“是否发生过最终 WriteHeader、Write、ReadFrom、Flush 或 Hijack 调用，不以成功写入字节为条件；Header.Set、1xx 和 Push 不标记”。当前保守实现有助于避免失败后重复写头，无需为了字面意义改代码，但应补零字节与失败 writer 的说明。

### DOC-13：404 请求不执行引擎请求体排空与关闭

**级别：中；类型：文档错误。**

- 文档证据：`http-engine-callback.md:210` 与 `parameter-binding.md:125` 笼统声称 handler/请求处理结束后引擎会限量排空请求体。
- 实现证据：`httpEngine.go:254-261` 未命中路由后立即返回；请求体排空与 Close 的 defer 直到 `httpEngine.go:270-276` 才注册。

因此该保证只适用于命中路由的请求，不适用于 404。`MaxBodyBytesReader` 虽已在路由匹配前包装，但引擎不会在 404 分支主动排空或关闭。

**推荐方案：** 若这是有意行为，把文档限定为“命中路由后”；若希望统一连接复用和资源释放，则把 body 收尾 defer 前移到路由匹配之前，并用真实 `net/http.Server` 集成测试验证 404 小/大请求体的连接复用，而不只依赖 `httptest.ResponseRecorder`。

### DOC-14：OnPanic 自身 panic 会向外传播

**级别：中；类型：关键约束遗漏。**

- 文档证据：`http-engine-callback.md:89-105`、`middleware.md:164-181` 只说明 panic 交给 `OnPanic`，没有说明回调自身不得 panic。
- 实现证据：`httpEngine.go:248-251` 在 recover defer 内直接调用 `e.OnPanic`，没有第二层 recover。
- 测试证据：`httpEngine_test.go:1123-1159` 明确锁定 `OnPanic` 的 panic 向外传播。

**影响：** 在直接调用 `ServeHTTP` 时 panic 传播给调用方；在标准 `net/http` 服务中通常由服务器层兜底并关闭该请求连接。自定义告警或序列化代码一旦 panic，框架承诺的 500 响应不再成立。

**推荐方案：** 至少在文档醒目标注“OnPanic 必须保证不 panic”。如需框架级隔离，可在调用自定义 OnPanic 外增加最终 recover，并用最小、不可失败的兜底写入；但应避免再次调用同一回调形成递归。

### DOC-15：不支持的 default 是否告警，文档自相矛盾

**级别：低；类型：文档内部矛盾。**

- 文档证据：`parameter-binding.md:269` 正确写明注册期输出 `slog.Warn`，但 `parameter-binding.md:353` 又写成“标签值被静默忽略”。
- 实现与测试证据：`defaults.go:54-85` 和 `defaults_test.go:478-489` 证明会告警但不阻断注册。

**推荐方案：** 将后者统一为“运行时不填充，但注册期输出 Warn；若日志级别过滤 Warn，使用者可能看不到”。同步检查 `request.md:168-170` 的措辞，保持三处一致。

### DOC-16：JSON map 的“任意 key/value 组合”属于过度承诺

**级别：中；类型：文档错误。**

- 文档证据：`parameter-binding.md:176-183` 声称 JSON 可反序列化任意 key/value 组合。
- 实现证据：`binding.go:83-89` 完全委托 `encoding/json`，因此受标准库约束，而不是任意组合。

JSON 对象 key 只能进入 string、整数或实现相应文本解码能力的键类型；channel、func、complex 及很多自定义组合也不可解码。非法组合会在运行时返回绑定错误。

**推荐方案：** 改为“支持 `encoding/json` 可解码的 map 类型”，列出常见支持示例和 key 限制；不要承诺任意组合。增加至少一个不支持键类型的负向测试，确保文档与错误通道一致。

### DOC-17：容器校验错误路径格式未完整记录

**级别：低；类型：关键诊断行为遗漏。**

- 文档证据：`parameter-validate.md:16-23,52` 只说使用字段绑定名路径。
- 实现证据：切片/数组在 `validate.go:225-260` 递归时只追加字段名，不追加索引；map 在 `validate.go:280-301` 会追加字符串化 key。
- 测试证据：`validate_test.go:1304-1307` 锁定 map 路径形如 `root.children.zzz_bad.name`。

当前 `items` 第 3 个元素失败仍只返回 `items.name`，而 map 返回 `children.<key>.name`。

**推荐方案：** 文档先准确列出当前格式。更理想的实现是切片/数组加入索引，如 `items.2.name`，并定义 key 含点号或特殊字符时的转义规则；否则客户端无法可靠定位容器元素。

### DOC-18：OpenAPI 的“仅 Req”标题与实际 Res 行为矛盾

**级别：低；类型：文档内部矛盾。**

- `openapi.md:55-66` 已正确说明 Res 的 default 与 nonzero 也参与文档生成。
- `openapi.md:119-121` 标题和正文又称 default 仅 Req。
- `openapi.md:147-151` required 标题称仅 Req，规则正文却又说 Req 与 Res 均适用。
- 实现证据：`openapi.go:565-602` 对 Req/Res 共用 schema 注册逻辑。

**推荐方案：** 标题改为“default 文档展示规则（Req 与 Res）”和“required 推断规则（Req 与 Res）”；正文单独强调运行时填充/校验仅作用于 Req。同步修正目录锚点链接，避免链接文本继续携带“仅 Req”。

### DOC-19：有 default 标签不一定取消 required

**级别：中；类型：文档错误。**

- 文档证据：`openapi.md:151-155`、`parameter-validate.md:9-12` 都把规则写成 `nonzero:"true"` 且“没有 default 标签”才 required。
- 实现证据：`meta.go:217-228` 只有 default 标签存在且字段类型通过 `isDefaultSupported` 时，`hasDefault` 才为 true；`openapi.go:594-597` 判断的是 `!fm.hasDefault`，不是标签是否存在。

例如 `time.Time \`nonzero:"true" default:"x"\`` 的 default 不受支持，仍会进入 required；按当前文档则会被误判为可选。

**推荐方案：** 全部改为“`nonzero:true` 且不存在**受支持并成功识别的 default**时 required”，并链接 default 支持类型表。增加不支持 default 类型与合法 default 类型的对照测试，Req/Res 各一组。

### DOC-20：requestBody 被固定为 required，与运行时空 body 语义不一致

**级别：中；类型：生成契约偏差 + 文档遗漏。**

- 文档证据：`openapi.md:206-214` 说明哪些方法生成 requestBody，但未说明其 required 规则。
- 实现证据：`openapi.go:449-466` 对所有生成的 requestBody 固定输出 `"required": true`。
- 运行时证据：`binding.go:80-89` 把空 body 的 `io.EOF` 视为成功；若 Req 没有 nonzero 或自定义校验要求，handler 可以正常执行。

因此对全可选 Req，OpenAPI 声称 body 必须存在，运行时却允许省略。

**推荐方案：** 若追求契约准确，可根据 Req 是否存在无默认的 nonzero 字段推导 required；自定义 Validator 无法静态推导时可提供显式操作级配置。若决定始终 required，则必须在文档明确，并考虑让运行时对缺失 body 同步返回 400。补“全可选、含 required、仅 default、KeepRawBody”四类测试。

### DOC-21：ResponseWrapper 约束记录不完整

**级别：中；类型：关键行为遗漏。**

- 文档证据：`openapi.md:213-214` 只说可传自定义包装结构体样例，interface 字段作为 data 占位符。
- 实现证据：`openapi.go:516-527` 对非 struct 静默回退默认包装；`openapi.go:529-545` 跳过未导出字段、无 json 名和 `json:"-"` 字段，并把所有 interface kind 字段都替换为业务 Res schema。

多个 interface 字段会全部变成同一 data schema；没有 json 标签的导出字段不会按字段名生成；传错类型也不会报错。这些规则很容易让生成文档与实际自定义响应结构不一致。

**推荐方案：** 文档列出完整约束，并推荐 wrapper 仅包含一个明确的 interface data 占位字段。更稳妥的实现是在 `GenerateOpenAPI` 前校验：必须为 struct、必须恰有一个 data 占位字段、字段必须有有效 json 名；不满足时返回错误。由于当前 API 只返回 map，可考虑新增返回 error 的生成入口，或至少记录警告而不是静默回退。

## 四、修复执行记录

### P0：非法或歧义输入与请求对象一致性

1. DOC-01：标量绑定改为临时值解析成功后原子提交，非法指针值不再绕过 nonzero。
2. DOC-02：JSON 绑定增加第二次 Decode 完整性检查，只接受一个完整 JSON 值。
3. DOC-11：中间件每次成功调用 `next(w, r)` 后同步请求状态，Context 访问器与成功、错误、校验错误、panic 回调统一使用当前对象。

### P1：跨客户端兼容与核心语义

1. DOC-03、DOC-04：使用标准 media type 解析；负时间戳精度忽略符号并改用 `time.Unix*` 构造。
2. DOC-05：固定数组始终递归校验现存元素，正式文档拆分数组与切片的默认值、校验语义。
3. DOC-07、DOC-08：分组路径统一拼接；子组保留父链，注册路由时展开并快照祖先中间件。
4. DOC-13、DOC-14：请求体收尾覆盖 404；`OnPanic` 外增加最终 recover 和未写响应时的 500 兜底。
5. DOC-19、DOC-20、DOC-21：OpenAPI required 按有效 default 与默认 Req 模板推导；ResponseWrapper 非法配置改为 fail-fast。

### P2：文档契约与诊断细节

DOC-06、DOC-09、DOC-10、DOC-12、DOC-15、DOC-16、DOC-17、DOC-18 已通过实现修正或文档澄清完成闭环，包括匿名嵌入 metadata、`Any` 原子注册、根路径斜杠例外、written 定义、default 告警、JSON map 范围、容器校验路径及 Req/Res OpenAPI 标题。

上述修改均同步到对应源码注释、正式文档和归属测试文件；未新增兼容性垫片，也未改变报告中“已核对且未发现偏差”的既有行为。

## 五、已核对且未发现偏差的关键行为

以下内容已逐项核对，当前文档与实现一致，不应因本报告中的问题而被误改：

- 9 个受支持 HTTP 方法、静态段 > 参数段 > 通配尾段及回溯顺序。
- handler 固定签名及 Req/Res 值/指针形态。
- path > body > query 覆盖顺序；JSON null 对指针/非指针的不同覆盖行为。
- `KeepRawBody` 顶层值嵌入识别、文件字段冲突、MaxBodyBytes 仍生效。
- default 两阶段模型的总体设计，以及多层容器不可达限制。
- nonzero 先于顶层自定义 Validator，普通 error 包装为 ValidationError。
- 洋葱顺序、短路、重复 next 防护、超过 64 层回退及禁止跨 goroutine 调用 next。
- NewEngine 的五个默认回调及两个 32 MB 默认值。
- 默认 200/400/404/500 响应、内部错误脱敏、1xx 与 Push 不标记 written。
- OpenAPI 的 deprecated、ignore、路径参数转换、类型映射、确定性排序及仅生成 200 成功响应的现有说明。

## 六、实际验收结果

2026-10-09 已按“文档声明 → 源码分支 → 可失败测试”逐项复核，并完成以下验收：

| 验收项 | 结果 |
| --- | --- |
| `git diff --check` | 通过 |
| `go test ./zchttp -count=1` | 通过 |
| `go test ./... -count=1` | 通过 |
| `go vet ./...` | 通过 |
| 修改过的 Go 文件经 CRLF 归一化后的 gofmt 对比 | 通过，无格式差异 |
| 文档审查与文档示例编译定向用例 | 通过 |
| `go test -race ./zchttp -count=1` | 通过，无数据竞争 |
| `go test ./zchttp -count=5` | 通过 |
| `go test ./zchttp -shuffle=on -count=5` | 通过 |
| `go test ./zchttp -cover -count=1` | 通过，语句覆盖率 99.4% |

补充执行的 `go test -race ./...` 未发现 data race 报告，但 zcdb 的 MySQL 集成测试因多个包级用例并发复用并互相删除 `zckg_test_integ` 表而失败，表现为表已存在或表不存在；该失败不涉及 zchttp 修改。随后重新执行普通全仓测试通过。

验收结论：DOC-01～DOC-21 均已有对应实现、源码注释、正式文档与回归测试，当前未发现新的文档—实现偏差。
