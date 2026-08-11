# M5.22c durable cross-target composition

本目录只保存小型、可审查的实验摘要。两份完整 target artifact 在测试时写入临时的既有 Campaign
内容寻址存储，不复制进仓库或跨目标 ledger。

结果证明同一父 Intent 的两个 target-local planned attempt 能在执行前分别冻结，在执行后分别提交，并从
两个独立 Campaign 目录恢复；外层 ledger 只引用 committed record/artifact digest 并汇总成本。它不合并
PSS，不增加 Coordinator，也没有调用 LLM。

复现：

```bash
go test ./cmd/control-experiment \
  -run '^TestM522bAndM522cPortableIntentExecutesAndRecovers$' \
  -count=1 -timeout 3m
```
