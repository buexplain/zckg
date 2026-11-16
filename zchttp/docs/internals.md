# zchttp 实现原理

本文件是 [architecture.md](architecture.md) 的深潜版：按子系统说明数据结构、关键算法、分支与设计取舍。标签/类型/错误的完整契约见 [reference.md](reference.md)。

文中“当前实现”表示内部细节，可能随优化变化；公开行为以 [reference.md](reference.md) 为准。

## 一、元数据与反射

### 1. `structMeta` / `fieldMeta`

`buildStructMeta` 在注册期把 Req 结构体反射成可复用的元信息：

```
structMeta
 ├─ fields []fieldMeta
 ├─ implementsValidator bool
 ├─ hasFormText bool      // 至少一个 Form 链可绑定的标量/标量切片
 ├─ hasFormFile bool      // 至少一个 Form 链文件字段
 ├─ hasJSON bool          // 至少一个 JSON 可绑定字段
 └─ hasAnyBody bool       // 前 3 者任一
```

`fieldMeta` 缓存：Form 链绑定名、字段索引路径、`jsonIncluded`、`excluded`、`nonzero`、`hasDefault`、`defaultVal`、`defaultSatisfiesNonzero`、切片/文件标记、`time_format`/`time_location`。请求期绑定、校验、默认值、OpenAPI 都直接读它，不再解析标签。

### 2. 来源投影

- `resolveFormName`：`form` 普通名 → 普通 `json` 名 → 字段名；`form:"-"` 返回空；`json:"-"`/`json:"-,"` 的 `-` 不污染 Form 链，回退字段名。
- `jsonIncluded`：精确 `json:"-"` 为 false；`json:"-,"` 为 true，键名 `-`。
- `excluded`：`name == "" && !jsonIncluded`，两个来源都不参与，整体跳过后续标签解析。
- 文件字段只在 Form 链绑定名非空时判定；`form:"-"` 的文件字段不算 multipart 文件字段。

### 3. 匿名嵌入展开

- 导出 struct / 指向 struct 的指针、未导出值 struct：递归展开为扁平字段，`indices` 变为多级路径。
- 未导出匿名指针嵌入：跳过展开并 `slog.Warn`（`fieldByIndex` 无法对未导出字段 `Set`）。
- 自引用嵌入：`visiting` 检测环，命中时跳过并告警。
- 展开后的字段沿用子字段的绑定名；重复名检查在展开后的 `meta.fields` 上做。
- `checkDuplicateJSONNames` 按 `encoding/json` 的可见字段递归展开匿名嵌入；`json:"-"` 跳过，`json:"-,"` 名为 `-`；重复即 panic。

### 4. meta 构建时机

`buildEntry` 调用 `buildStructMeta(reqElemType)` 构建 Req 顶层 meta。嵌套**具名** struct 的 meta 由 `cachedStructMeta` 按需构建：

- 注册期 `applyDefaults` 可达的值类型 struct、固定数组元素、切片/map 元素（含指针元素指向的 struct）会在注册阶段构建；
- 直接 `*Struct` 字段（模板 nil）等延迟到请求期递归校验/默认值填充，或 OpenAPI 生成首次使用；
- 构建结果进 `sync.Map`，之后跨请求共享。

### 5. `fieldByIndex` 与模板

`fieldByIndex` 沿 `indices` 取字段；遇到 nil 指针会自动 `reflect.New` 物化。这样：

- 匿名嵌入指针在注册期被物化，内部 default/nonzero 等价于顶层；
- 嵌套访问不会因 nil 指针 panic；
- `defaultReq` 是注册期 `applyDefaults` 后的模板，请求时 `reflect.New` + `Set` 浅拷贝复用。

## 二、路由基数树

### 1. 结构

`routeNode` 同一节点可同时持有：

- `static map[string]*routeNode`：静态段；
- `param *routeNode`：参数段（`{name}` / `{name?}`）；
- `catchAll *routeNode`：通配尾段（`{name...}`）；
- `entries[0]`：该节点终点路由；`entries[1]`：可选参数省略分支；
- `regEntry`：该节点代表的路由（用于冲突提示）。

### 2. 插入

`insertRoute` 逐段下探：

- 静态段：新建/复用 `static[literal]`；
- 通配段：写入 `catchAll`，同名冲突 panic，终点写 `entries[0]`；
- 参数段：同名、可选性必须一致，否则 panic；可选参数同时写 `param.entries[1]`（省略分支），并继续下探以支持后续段命中 `entries[0]`；
- 终点重复注册 panic；冲突消息含双方 handler 文件/行号。

`Any` 先对所有 9 个方法 `prepareRegistration`，再克隆树逐方法预检，全部通过才提交；任一步 panic 都不会残留部分方法。

### 3. 匹配

`matchPath` 逐段子串扫描，不预切分整个路径：

1. 路径为空：终点 `entries[0]` > 可选省略 `param.entries[1]` > 通配零段 `catchAll.entries[0]`；
2. 静态段优先，失败回溯；
3. 参数段不匹配空段，捕获当前段；
4. 静态/参数都失败时，通配槽捕获进入本层时的剩余路径（去掉前导 `/`，可含多段）。

优先级因此是 **静态 > 参数 > 通配**；`/a/{x}` 与 `/a/b/{rest...}` 并存时，`/a/b` 沿静态 `b` 命中通配零段，而不是回退到 `{x}`。

### 4. 归一化

`normalizePath`：补前导 `/`；非根路径去掉所有末尾 `/`；空串归一到 `/`。注册与匹配都调用，所以 `/hello` 与 `/hello/` 等价。内部多重斜杠不折叠：注册 `//a` panic；请求 `/a//b` 不命中静态/参数路由，但可被通配捕获。

## 三、绑定管线

### 1. 入口

```
bindRequestData(r, reqPtr, meta, multipartMaxMemory, keepRawBody)
  ├─ meta.hasFormText → parseStrictQuery + bindQueryValues
  ├─ keepRawBody → return
  ├─ GET/DELETE/HEAD → return
  ├─ !meta.hasAnyBody → return
  └─ bindBody
```

- `parseStrictQuery` 用 `url.ParseQuery(r.URL.RawQuery)`，保留语法错误（`r.URL.Query()` 会丢弃）。
- 只有存在 Form 链可绑定字段才解析 query；没有 body 可绑定字段不读 body。
- 绑定错误不提前返回，`ServeHTTP` 存 `bindingErr`，core 层统一 400。

### 2. Content-Type 分流

`bindBody` 先 `mime.ParseMediaType`（主类型小写）：

- `application/json`：有 `hasJSON` 才解码；空 body 视为 EOF 合法。
- `application/x-www-form-urlencoded`：有 `hasFormText` 才解析；复制 request、清空 query、伪装 POST，只读 body；保留 `ParseForm` 的 10 MB 上限。
- `multipart/form-data`：有 `hasFormText` 或 `hasFormFile` 才 `ParseMultipartForm`；复制 request 清空 query；把 `MultipartForm`/`Form`/`PostForm` 写回原 request。
- 其他：有 `hasJSON` 时按 JSON 解码（空 body 合法）。

`decodeJSONBody` 只接受一个完整 JSON 值：首个值后允许空白；第二个 JSON 值或非空尾随内容报错。

### 3. 绑定值与转换

`bindValues` 遍历 `meta.fields`：

- 文件字段：从 files map 取，设置 `*FileHeader`/`[]*FileHeader`，随后跳过 values；
- 普通字段：取同名 values，`setFieldValue` 写字段；
- 单字段转换失败立即返回错误（带来源和参数名），不写半成品。

`setFieldValue`：

- 切片：元素必须是 `isScalarType`；`time` 切片的空字符串元素保留原位置旧值；其余逐元素 `setScalar`；全部成功后才 `Set`。
- 标量：`setScalar` 在临时值中解析，成功才提交；失败保持原值。

`parseScalar` 支持 `string/bool/int*/uint*/float*/time.Time` 及其指针（最多 32 层）；其他类型返回 unsupported，由调用方跳过。`setTime` 处理 `unix*`/layout/自动探测；`time_location` 默认 local。

### 4. 路径参数

`bindPathParams` 按注册顺序取 `pathParamBinding.indices`，用同一个 `setScalar` 写字段：

- 覆盖同名 query/body；
- 尾部可选参数省略/通配零段命中时捕获值不足，`break`，保留模板 default/零值；
- 单个参数转换失败立即 400（`BindingError`）。

### 5. 来源参与后的严格性

query、urlencoded/multipart body、路径参数一旦参与绑定，已知字段转换失败即 400；未知参数和该来源不可绑定类型忽略。JSON 解码失败同样 400。没有可绑定字段的来源完全不解析/不校验。

## 四、默认值

### 1. 解析

`isDefaultSupported` 只接受标量、标量任意层指针、元素为标量/标量指针的切片；`parseDefaultValue` 用与运行时相同的 Trim/逗号切分/`setFieldValue` 规则解析，并拒绝 `NaN/Inf`。解析失败 → `hasDefault=false`，注册期 `checkUnsupportedDefaults` 告警。

### 2. 两阶段

`applyDefaults(reqPtr, meta, requestPhase)`：

- 注册阶段：`hasDefault && CanSet && IsZero` → 填充；递归进入值 struct、固定数组、有元素的切片/map、非 nil 指针。
- 请求阶段：`hasDefault && CanSet && Kind==Ptr && IsNil` → 填充；值类型一律跳过。

容器递归规则：

- 值 struct：注册期可递归；
- 固定数组：元素始终存在，注册期即可预填值/指针 default；
- nil 切片/map：无元素可填，绑定创建元素后请求阶段补 nil 指针；
- nil 指针/容器指针：注册期跳过，绑定创建后请求阶段补；
- 多层容器（`[][]Struct`、`map[K][]Struct` 等）：不穿透内部元素。

### 3. 深拷贝与可达性

`hasRefFields` 扫描模板中非 nil 指针/切片/map（含数组元素、接口动态值），决定 `needsDeepCopy`。请求时浅拷贝后 `deepCopyDefaults` 递归断开共享引用（深度上限 32，防环）。

`hasRequestPhaseDefaults` 扫描类型树是否存在带 default 的指针字段，决定请求阶段是否需要执行 `applyDefaults`。`checkUnsupportedDefaults` 递归类型树，对不支持类型、非法值、请求期不可达字段输出 `slog.Warn`。

## 五、校验

### 1. 入口

```
validateRequest(reqPtr, meta, needsNonzero)
  ├─ needsNonzero → validateNonzero
  └─ validateCustom（Validator）
```

`needsNonzeroValidation` 由 `hasNonzeroInTree` 在注册期传递性扫描；全树无 nonzero 时跳过遍历。

### 2. `validateNonzeroWalk`

递归遍历 `meta.fields`：

- `nonzero` 且 `IsZero` → `ValidationError{Field: prefix+name, Message:"is required"}`，不递归；
- `nonzero` 非零 → 继续递归；
- 非 nonzero 且零值 → 跳过；固定数组例外，仍逐元素递归；
- 非零 struct/指针/切片/数组/map → 递归。

工具处理：

- 切片/数组：元素为 struct 或 `*struct` 时逐元素；不附加索引，错误路径仍是 `items.name`；nil 指针元素跳过。
- map：value 为 struct/*struct 时逐 value；错误路径追加 `fmt.Sprint(key)`；值拷贝后校验，再写回 map。
- `visited`：`visitKey{ptr,typ}` 防环；map 桶指针与指针 value 也登记；临时副本不登记自身地址，避免误判漏检。
- 固定数组即使整体为零也递归，因为 N 个元素始终存在。

### 3. `Validate()`

`validateCustom` 只对顶层 Req 调用：

- `meta.implementsValidator` 为 false 快返回；
- typed-nil 用 `isNilError` 视为通过；
- 返回 `*ValidationError` 直接透传；
- 其他 error 包装为 `&ValidationError{Message: err.Error(), Err: err}`，保留错误链。

`hasNonzeroInTree` 扫描全类型树（穿透嵌套 struct/指针/切片/数组/map），多层容器也保守下探，避免漏检。

## 六、中间件链

### 1. 执行器

`runChain` 从池取 `chainRunner`，把 `middlewares[0]` 放最外层，`finalHandler` 放最内层。`finalHandler` 是 core：先校验，再调用 handler。

- 层数 ≤ 64：位图实现，所有层共享一个 `nextFunc`，按 `current` 层号置位；
- 层数 > 64：`runChainRecursive`，每层一个闭包，语义一致。

### 2. `next`

`advance(w,r)`：

1. 按当前层号检查 `calledBits`；已调用 → `ErrNextCalledMultipleTimes`；
2. 置位；
3. 若 `r` 换了，`c.ctx = nextContext(c.ctx, r)`（用 `r.Context()`，缺 requestState 时补挂）；
4. 更新 `c.w/c.r` 与 `requestState.w/req`；
5. 执行下一层。

外层中间件的 `ctx` 参数不会被下游替换改变；它通过 `BoundResFromContext(ctx)` 读的是同一个 `requestState`，所以后置阶段能看到 handler 的 Res。

### 3. 洋葱模型

```
A 前置 → B 前置 → core（校验 + handler） → B 后置 → A 后置
```

校验发生在 core 内，因此各层前置逻辑已执行、后置逻辑尚未执行。路由级中间件是最内层中间件，紧贴 core，不是直接紧贴裸 handler。

## 七、响应与错误

### 1. `responseWriter`

`responseWriter` 包装底层 writer，按底层能力组合出 Flusher/Hijacker/Pusher 变体；所有变体嵌入同一个基础包装器，池化复用。

写操作：

- `WriteHeader`：除 101 外的 1xx 直接透传、不置 written、不参与幂等；101 与最终状态码置 written，重复最终调用直接返回；
- `Write`：先置 written 再透传（0 字节也置位）；
- `ReadFrom`：先置 written；底层支持 `io.ReaderFrom` 走零拷贝，否则 `io.Copy`；
- `Flush`/`Hijack`：先置 written 再透传；
- `Push`：只透传，不置 written（独立流）；
- `Unwrap`：返回底层 writer，供 `http.NewResponseController` 使用；直接写底层会绕过跟踪。

### 2. 回调与错误分发

`ServeHTTP` 最外层 `runChain` 返回 error 后：

```
errors.As(*ValidationError) || errors.As(*BindingError) → OnValidationError
否则 → OnError
```

`OnResponse` 在 core 内、handler 成功后调用；`OnNotFound` 在路由未命中时调用；`OnPanic` 由 `defer recover` 调用，回调再 panic 由 `invokePanicHandler` 第二层 recover 记录并兜底 500。

### 3. 默认回调

- `DefaultResponseHandler`：未写响应才编码 JSON；编码失败（含 func/chan）→ 500 text/plain；写出失败只记日志。
- `DefaultErrorHandler`：未写响应才输出 500；详情只写日志。
- `DefaultValidationErrorHandler`：未写响应才输出 400，回显 `err.Error()`。
- `DefaultNotFoundHandler`：未写响应才输出 404。
- `DefaultPanicHandler`：记录 panic + stack；未写响应才输出 500。
- 所有默认回调都按 `WantHtml` 选择 HTML/JSON；日志带 `zckg=zchttp`。

JSON 编码使用池化 `json.Encoder`，关闭 HTML 转义；先编码到缓冲，成功后再写响应。

## 八、OpenAPI 生成器

### 1. 三遍遍历

`GenerateOpenAPI(router, info)`：

1. `collectTypeUsages`：从每个 Req 类型出发，标记 `reachedViaValue`（值嵌套可达）；
2. `collectDefaultsReachability`：标记 `reachedByDefaults`（`applyDefaults` 可达）；
3. 按排序后的 method/path 构建 paths/operations/schemas。

两遍可达性都只遍历 Req；只出现在响应中的类型不会获得 default 展示标记。路由与字段排序保证输出确定性。

### 2. schema 角色与 key

`schemaKey{t, form, role}` 区分：

- 请求变体：裸名 `<Type>`；
- 响应变体：`<Type>Response`；
- 同类型同角色但 form 不同（multipart 顶层 vs JSON 结构）：`uniqueName` 追加序号（`Type2` 等）。

`registerStructSchema` 先占位再填充，支持递归 struct 循环引用。

### 3. 字段命名

- query/path/multipart 顶层：`meta.fields` 的 Form 链名；multipart 只输出 `isQueryBindableType` 或文件字段，命名 struct/map/数组/多层容器跳过。
- JSON 请求体、响应、嵌套 struct：`jsonFields`，按 `encoding/json` 规则（标签名、匿名嵌入展开、同深度冲突丢弃/标签优先）。

### 4. `typeToSchema`

按类型递归：

- 指针：被指向类型是 `$ref` → `{nullable, allOf:[$ref]}`；否则在被指向 schema 上加 `nullable`；
- `time.Time`：表单形态（`roleRequest && !jsonWire`）按 `time_format`，JSON/响应恒 date-time；
- `[]byte`：请求侧 array（0..255，nullable），响应侧 string/byte；
- 切片/数组：array + items；普通切片 nullable，固定数组不；
- struct：`registerRequestSchema`/`registerResponseSchema` 后 `decorate`；
- map：object + additionalProperties + nullable；非 string key 写 description；
- 文件：string/binary 或 array of binary。

`decorate` 读 `default`/`example`/`description`/`deprecated`；default 还受 `reachedByDefaults`（指针）与 `reachedViaValue`（值类型）控制，`suppressDefault` 用于必选 path 参数。

### 5. 参数与 required

- `buildQueryParams`：GET/DELETE/HEAD，跳过 `name==""`/`"-"`、path 字段、`ignore`、文件、非 `isQueryBindableType`。
- `buildPathParams`：所有方法声明 path，`required:true`；可选/通配语义写 description；必选 path 抑制 default。
- `requestBodyRequired`：以 `defaultReq` 为初始状态，排除 path 字段后跑 `validateNonzero`；失败 → body required。自定义 `Validate` 不参与。
- `KeepRawBody`：其余方法把非路径字段作 query，requestBody 固定 `{"content":{"*/*":{"schema":{}}}}`。
- 响应：统一包装 `Response_<Type>`，data 指向 `<Type>Response`；`OpenAPIInfo.ResponseWrapper` 可自定义包装并校验占位符。

### 6. 展示规则

- 请求 schema 才展示 default；响应不展示。
- 请求 schema 的 required 由 `nonzero` + 有效非零 default 推断；响应不输出 required。
- `ignore` 影响 schema/query，不影响 path 声明。
- 值 struct 的 deprecated 作为 `$ref` 兄弟键输出，但 OpenAPI 3.0 工具忽略；指针 struct 用 allOf 外层承载。
- 多层容器中的 struct 不可达时不展示 default；同一类型在单层/值嵌套路径可达时按类型并集展示。
