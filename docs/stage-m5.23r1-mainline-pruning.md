# M5.23R1：主线收缩与第一批历史 Agent 执行面退休

日期：2026-08-12

状态：完成

## 目标与检查点

M5.23g 的真实 Agent 负结果冻结在已推送 commit `3ec9237`。本阶段不重新调用
模型、不修改冻结 benchmark JSON，不与 M5.24 外部评价同时扩展新框架。

保留根定义为：

```text
Stateless Search
  + restricted frontier-order Agent
  + Campaign durability
  + formal evaluation
  + qualified deterministic baselines
```

Git history、历史阶段文档和冻结 summary 负责退休实验的复现；HEAD 不再为每一代
已放弃的 Agent 方法保留可执行入口。

## 实现

1. 新增 protocol-neutral `AgentTransportFreeze`，将当前 Stateless Agent 和 Campaign 仍使用的
   provider/model/thinking/temperature/call/retry 上限从 B4 ablation 对象中分离。
2. 新增独立 `agent_provider.go`，保留 DeepSeek v4 Flash 的 exact-byte request、response identity、
   token accounting、bounded failure code 与安全 key-file 读取，不包含协议或搜索语义。
3. 保留 `ValidatePreferenceOnlyProposal`，因当前 Campaign Planner 仍机械依赖其硬约束边界；
   它从 feedback 文件迁移到独立 `intent_preference.go`。
4. 退休 M5.18 one-shot、batch feedback、follow-up 和 B4 pair/preflight/freeze/consumer/runner 的
   production composition、CLI strategy 与仅服务该组合的测试。
5. 将 credential permissions/symlink/multi-line 拒绝、pre-credential exact-byte freeze、transport failure、
   unreadable response、token 计费和 transport authority expansion 负例迁移到保留原语的测试。
6. CLI 增加回归，确认六个退休 strategy 不再可达。

## 规模变化

相对 `3ec9237`：

| 指标 | 之前 | 之后 | 变化 |
|---|---:|---:|---:|
| `cmd/control-experiment` + `internal/controlexperiment` Go LOC | 33,832 | 29,393 | -4,439 |
| `cmd/control-experiment/main.go` | 818 | 739 | -79 |

本轮减少的不只是入口分支，而是已放弃 Agent 方法的完整生产闭包。冻结的历史
JSON 和阶段文档未删除。

## 验证

- `go test ./...`：通过；`cmd/control-experiment` 包完整测试约 216 秒。
- `go vet ./...`：通过。
- `go test -race ./internal/controlexperiment`：通过。`cmd/control-experiment` 全包 race 在约定的
  单次 240 秒窗口内未返回可确认终态，未重跑；本轮新增 provider/key/退休入口的
  定向 race 通过。
- `python3 -m unittest discover -s agents -p 'test_*.py'`：命令通过，当前目录发现 0 项。
- `git diff --check`：通过。
- 本阶段未读取 key，未调用 LLM API，未改动 raft 工作区。
- M5.23g summary 和冻结 artifact 未改动。

## 没有证明

- 不证明 Agent 优于 canonical、uniform、DFS 或专家方法。
- 不证明 PSS/义务发现能预测外部缺陷检出。
- 不证明 formal private dataset 已就绪；`formal_ready=false`。
- 不证明 Cross-target、Campaign Planner 或 macro Intent 应保留，只是本轮尚未有足够
  契约信息安全删除。
- 不证明 exact-prefix reconstruction 已成为外部评价的主要瓶颈。

## 下一阶段

M5.23R2 先冻结 M5.24 的 `SearchMethod -> Campaign AttemptProvider` 最小契约和强基线/重复试验
成本字段。然后机械判定 Cross-target 和旧 Campaign Planner 是否仍为正式消费者；若不是，
在第二批 prune-only 中退休。不在契约冻结前抽象 model-call 数据模型或增加 snapshot。
