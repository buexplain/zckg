# zcconfig（兼容入口）

本文档已拆分重构。新的入口是 [README.md](README.md)；本文件仅保留旧链接可用。

## 文档地图

| 文档 | 内容 |
|---|---|
| [README.md](README.md) | 总览、30 秒模型、5 分钟上手、概念模型、术语表 |
| [env.md](env.md) | `.env` 装载与 `Env[T]` 读取 |
| [config.md](config.md) | `Register` 合并与 `Config[T]` 路径读取 |
| [cast.md](cast.md) | 类型转换判定树、转换表、数值安全子集 |
| [db-config.md](db-config.md) | `DBConfig` / DSN 组装 |
| [errors.md](errors.md) | 错误模型与排查 |
| [internals.md](internals.md) | 实现原理（数据结构、锁、调用链、取舍） |
| [conventions.md](conventions.md) | 不变量、被否决方案、破坏性变更与迁移 |

## 一句话

两条独立通道（`.env` 文本、业务配置树）共用一个泛型转换引擎：装载/注册只存值，读取时按目标类型解析；key 缺失返回默认值，key 存在但转换失败 panic `*TypeMismatchError`。
