# M5.9a legacy execution consumer freeze

`production-edges.txt` 是当前生产 Go 包对以下 legacy execution 范围的全部直接 import 边：

- `internal/adapter`
- `internal/host`
- `internal/engine`
- `internal/explore`
- `internal/scenario`
- `drivers/etcdraft`

它不把 `internal/core`、`internal/driver`、Oracle、Coverage、PSS 或 Family knowledge 整体标为可删除；
这些包混合承载历史 schema 或仍需移植的可信研究逻辑，必须另行拆分。

复核命令：

```bash
make audit-legacy-consumers
```

Make 目标从 `go list` 的生产 imports 重新生成排序后的边集合并与本文件逐行比较。新增 legacy
consumer 会失败；迁移删除一条边也会失败，要求开发者同步收缩冻结集合和阶段记录。测试 imports
和只读 JSON/Markdown 工件不计为生产边，但仍由全仓测试和文档链接检查保护。
