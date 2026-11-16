# zcmodel（兼容入口）

本文档已拆分重构。新的入口是 [README.md](README.md)；本文件仅保留旧链接可用。

## 文档地图

| 文档 | 内容 |
|---|---|
| [README.md](README.md) | 总览、30 秒数据流、核心概念、文档地图、阅读路径 |
| [quickstart.md](quickstart.md) | 最小可运行示例、真实产物、zcdb Schema 桥接、显式字段信息 |
| [api.md](api.md) | `Generate`、`Input`/`Column` 等类型、`NameCase`/`Dialect`、错误表 |
| [generated-code.md](generated-code.md) | 产物契约：文件布局、Entity/DO、互转方法、包名、产物安全 |
| [tags-ddl-index.md](tags-ddl-index.md) | tag 转义、`ddl` 片段、索引块、doc 注释净化 |
| [types-and-naming.md](types-and-naming.md) | 三方言类型映射、归一化、命名转换 |
| [regeneration.md](regeneration.md) | 再生成契约：替换/保留/import/块/边界 |
| [architecture.md](architecture.md) | 文件与职责、数据流、设计取舍 |
| [pipeline.md](pipeline.md) | `Generate` 阶段 0—5（六段）、校验顺序、落盘安全 |
| [skeleton-merge.md](skeleton-merge.md) | 骨架保持式合并的数据模型与算法 |
| [testing.md](testing.md) | 测试分层、复现与排障 |
| [glossary.md](glossary.md) | 术语与不变量 |
| [code-map.md](code-map.md) | 文档 ↔ 源码符号 ↔ 测试 |

## 一句话

输入表结构 → 校验并补全列信息 → 组装 Entity/DO/互转方法 → 按「骨架保持式合并」写回目标文件（生成声明原位替换、用户代码与注释原位保留）→ gofmt 自校验 + 原子落盘；纯代码生成，不连接数据库。
