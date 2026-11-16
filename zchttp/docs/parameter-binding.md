# 参数绑定

本文说明 `zchttp` 如何按 method/Content-Type 把请求数据绑定到 `Req`：来源分流、query/body/path 的合并与覆盖、字段类型、时间解析、文件上传与 `KeepRawBody`。实现细节见 [internals.md](internals.md) 的“绑定管线”，标签/类型/限制的完整表见 [reference.md](reference.md)。

适合：需要处理 query、表单、JSON、路径参数、文件上传的业务开发者。

## 一、心智模型

1. **按来源决定是否绑定**：只有存在 Form 链可绑定字段才解析 query；只有选中的 body 来源存在可绑定字段才读取 body；没有可绑定字段的来源完全不解析、不校验。
2. **合并而非二选一**：POST/PUT/PATCH 等先严格解析/绑定 query，再绑定 body；body 中出现的字段覆盖 query，缺失字段保留 query。
3. **path 最后写**：query/body 绑定成功后，路径参数再绑定并覆盖同名值；若 query/body 绑定失败，path 不写入。优先级 path > body > query。
4. **一旦参与就严格**：query/body/path 已知字段转换失败返回 400；JSON 解码失败 400；不采用“跳过失败字段”的尽力绑定。
5. **Form 链与 JSON 是两套命名**：query/form/multipart/path 用 Form 链名，JSON body 用 `json` 名；详见 [request.md](request.md)。

## 二、GET / DELETE / HEAD

```text
GET /search?keyword=go&page=3
```

- 有 Form 链可绑定字段时，严格解析 `r.URL.RawQuery`（`url.ParseQuery`，语法错误不丢弃）。
- query 语法错误（非法百分号转义、非法分号等）或已知字段转换失败 → 400。
- 未知参数忽略；没有 Form 链字段不解析、不校验。
- `KeepRawBody` 的 Req 无论 method 都按本节规则只绑定 query + 路径参数。

## 三、带 body 的方法

```text
POST /orders/batch?dryRun=true
Content-Type: application/json
{"items":[...]}
```

流程：

1. 先严格解析/绑定 query（有 Form 链字段时）；
2. 再按 Content-Type 绑定 body；
3. body 显式出现的字段覆盖 query；body 缺失字段保留 query；
4. query 非法不会被 body “救回”：query 来源参与时，语法/转换错误在 body 绑定前返回 400。

### Content-Type 分流

| Content-Type | 行为 |
| --- | --- |
| `application/json` | `json.Decoder.Decode`；只接受一个完整 JSON 值，空 body 合法 |
| `application/x-www-form-urlencoded` | 复制 request、清空 query、伪装 POST 解析 body；保留 `ParseForm` 10 MB 上限 |
| `multipart/form-data` | `ParseMultipartForm(MultipartFormMaxMemory)`；绑定表单字段与文件 |
| 其他（有 JSON 字段时） | 回退按 JSON 解析 |

`Content-Type` 用 `mime.ParseMediaType` 解析：主类型小写、支持参数/引号/空白；语法非法 400。

### body 覆盖 query 的粒度

| body 中的情况 | 对同名字段的影响 |
| --- | --- |
| 显式出现（含显式零值） | 覆盖 query 值 |
| `null`，目标为值类型（int/string/bool/struct/数组） | 解码 no-op，保留 query 与注册期 default |
| `null`，目标为指针/切片/map/接口 | 置 nil，覆盖 query；指针有 default 时请求阶段回填；切片/map 的注册期 default 不恢复 |
| 未出现 | 保留 query 值 |

表单同理：body 出现覆盖同名 query，未出现保留。

### body 大小限制

- `MaxBodyBytes`（默认 32 MB）在绑定前用 `http.MaxBytesReader` 包裹 body；绑定期读到超限 → `BindingError` 400。
- 没有 body 可绑定字段、`KeepRawBody` 等不读取 body 的场景不触发绑定错误；读取方会得到 `*http.MaxBytesError`。
- `MaxBodyBytes=0` 表示框架不额外限制；urlencoded 仍受 `ParseForm` 10 MB 上限，JSON 无额外隐式上限，multipart 非文件值仍受 `MultipartFormMaxMemory + 10 MB` 预算约束。
- multipart 文件超出 `MultipartFormMaxMemory` 落临时文件；非文件值无落盘路径，超出预算 400 `multipart: message too large`。

## 四、字段命名

Form 链（query/urlencoded/multipart/path）：

1. `form` 标签：`form:"-"` 无条件跳过；普通名直接用；
2. 普通 `json` 名作为 fallback；
3. 字段名。

`json:"-"`/`json:"-,"` 是 JSON 专用值，Form 链回退字段名。JSON body 完全按 `encoding/json`，`form` 不参与。完整投影表见 [reference.md](reference.md)。

## 五、支持的字段类型

| 类型 | query/form/multipart | path | JSON |
| --- | --- | --- | --- |
| `string`/`bool`/`int*`/`uint*`/`float32/64` | ✅ `setScalar` | ✅ | ✅ |
| 上述标量指针 | ✅ 自动分配 | ✅ | ✅ |
| 上述切片 | ✅ 同名多值 | ❌ 注册期 panic | ✅ |
| `[]byte`/命名 byte slice | ✅ 重复十进制字节 | ❌ 注册期 panic | ✅ 数字数组（也接受 base64） |
| `time.Time` | ✅ 按标签/自动探测 | ✅ 按标签/自动探测 | 仅 RFC3339 |
| `map[K]V` | ❌ 跳过 | ❌ 注册期 panic | ✅ |
| 具名 struct | ❌ 不递归 | ❌ 注册期 panic | ✅ |
| `*multipart.FileHeader` / `[]*multipart.FileHeader` | ✅ multipart 文件 | ❌ 注册期 panic | — |

> `path` 只接受标量、`time.Time` 与指向它们的多级指针；slice、`[]byte`、数组、map、struct、文件字段在注册期 panic，详见 [routing.md](routing.md) 的“路径参数”。

- query/form 参数值**不做 TrimSpace**：标量与切片都保留首尾空格；`default` 值仍按默认值规则 Trim。
- 未知参数与当前来源不可绑定的类型（query/urlencoded 中的 map/struct/文件）按既有规则忽略；JSON 的可绑定类型见上表。
- 切片元素必须是标量/标量指针；`[]Struct`、数组、指针包裹容器在 query/form 不写入；path 上则注册期 panic。
- 单字段转换失败立即 400，不写入半成品。

完整类型 × 来源 × OpenAPI 矩阵见 [reference.md](reference.md)。

## 六、时间解析

`time_format`/`time_location` 只作用于表单链路（query/urlencoded/multipart/path）；JSON 请求体/响应不读，时间必须 RFC3339。

- `time_format`：`unix`/`unixmilli`/`unixmicro`/`unixnano` 或 Go layout；为空自动探测。
- 自动探测：数字按位数推断（13=毫秒、16=微秒、19=纳秒，其余秒）；非数字尝试 RFC3339Nano、RFC3339、常见日期/时间布局。
- `time_location` 默认 `time.Local`；解析失败降级并告警。
- 支持 `time.Time`、多级指针、`[]time.Time`、`[]*time.Time`；`[][]time.Time`、`*[]time.Time`、`map[K]time.Time` 不参与标量绑定。
- 时间切片空字符串元素跳过并保留原位置旧值；旧值为零时间时仍为零时间。

## 七、文件上传

```go
type UploadReq struct {
    Title string                  `json:"title"`
    File  *multipart.FileHeader   `json:"file"`
    Files []*multipart.FileHeader `json:"files"`
}
```

```go
func upload(ctx context.Context, req UploadReq) (UploadRes, error) {
    if req.File == nil {
        return UploadRes{}, nil
    }
    f, err := req.File.Open()
    if err != nil {
        return UploadRes{}, zchttp.NewBindingError(err)
    }
    defer f.Close()
    return UploadRes{Size: req.File.Size}, nil
}
```

- 文件字段类型必须是 `*multipart.FileHeader` 或 `[]*multipart.FileHeader`；只有带 Form 链绑定名才算文件字段。
- 有文件字段的 Req 走 multipart；文件超出内存部分落临时盘。
- 文件字段与 `KeepRawBody` 冲突，注册期 panic。

## 八、KeepRawBody

```go
type WebhookReq struct {
    zchttp.KeepRawBody
    Sig string `json:"sig" nonzero:"true"`
}

func webhook(ctx context.Context, req WebhookReq) (WebhookRes, error) {
    r, _ := zchttp.RequestFromContext(ctx)
    body, err := io.ReadAll(r.Body)
    if err != nil {
        return WebhookRes{}, zchttp.NewBindingError(err)
    }
    // 验签...
    return WebhookRes{OK: true}, nil
}
```

- 只有 Req **顶层值嵌入**识别；`*KeepRawBody`、深层嵌入、具名字段无效。
- 无论 method 只绑定 query + 路径参数，不读取 body；`r.Body` 原样留给中间件/handler。
- query 仍严格校验（存在 Form 链字段时）；默认值、nonzero、Validator、path > query 顺序不变。
- 与带 Form 链绑定名的文件字段同时出现 → 注册期 panic；只有该嵌入、没有其他 JSON 字段的 Req 不读取 body。
- `MaxBodyBytes` 仍生效；handler 读取超限会得到 `*http.MaxBytesError`，可包装为 `BindingError` 返回 400。

## 九、默认值

默认值采用两阶段：

- 注册期：填充模板中“注册期可达”的零值字段（顶层、值类型 struct、固定数组元素）；nil 切片/map 没有元素，字段要等绑定创建元素后由请求阶段补 nil 指针；
- 请求期：只补 nil 指针；slice/map/nested ptr 动态创建，固定数组元素已存在。

嵌套指针 struct 的值类型字段 default 不生效；指针字段可补。切片/map 元素同理。完整支持范围、容器深度矩阵与告警规则见 [reference.md](reference.md) 与 [request.md](request.md)。

## 十、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 来源分流、query/body/path | `binding.go` | `binding_test.go` |
| 字段名、来源标志、重复名 | `meta.go` | `meta_test.go` |
| 默认值两阶段 | `defaults.go` | `defaults_test.go` |
| 请求入口与错误注入 | `httpEngine.go` | `httpEngine_test.go` |
| 时间解析 | `binding.go` | `binding_test.go` |

继续阅读：[reference.md](reference.md)（类型/默认值/限制）、[internals.md](internals.md)（绑定算法）、[parameter-validate.md](parameter-validate.md)（校验）。
