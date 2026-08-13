# M5.23R4a：旧无模型 Campaign 退休与 Stateless Agent 恢复底座

日期：2026-08-12

状态：完成

## 机械审计结论

旧 Campaign/Intent/ModelCall 生产闭包约 4,500 行，但不能整块删除：

- `campaign-etcdraft-v1` 的确定性多 attempt、恢复、summary 能力已由 R3 canonical/uniform Stateless
  runner 完整替代；它没有剩余独有语义；
- `campaign-etcdraft-agent-v1` 的宏观 GuardedTestIntent 规划不再符合主线，但仍保存调用前 intent、
  dispatch 后禁止盲重试、terminal result/model work 的 durable 语义；
- Campaign store/Coordinator、Agent provider、Stateless frontier-order validator 和 formal evaluator
  仍是现行主线，不能因删除旧 Planner 一并移除。

## 本阶段修改

1. 从 CLI 和 Makefile 退休 `campaign-etcdraft-v1`、旧 create/resume 目标及其专用 runner 测试；
2. canonical/uniform 继续使用 R3 的共同 Stateless runner；
3. `statelessAgentCallJournal` 可在不提供 key 时创建并增加严格恢复：
   - prepared（仅 intent）：恢复后在显式激活 key 时只 dispatch 一次；
   - completed：重新计算 exact request identity 后只重放保存 content/work，不调用 transport；
   - ambiguous（intent + dispatch，无 result）：消费 ordinal，永久拒绝自动重试；
   - rejected/failed terminal：不得越过为新调用；
4. 恢复逐文件拒绝未知文件、symlink、ordinal/root/request drift，并重新验证 intent/dispatch/result digest。

没有读取真实 key、调用模型或修改冻结 benchmark。

## 离线恢复验证

- prepared：第一次运行在 intent 落盘后因无 key 停止；恢复后提供测试 key，transport 恰好调用一次；
- completed：第二次恢复返回完全相同 content/work，transport 调用数保持不变；
- ambiguous：人工形成 durable dispatch 无 result，恢复后即使提供 key 仍拒绝并保持 0 次 transport；
- M5.23g 冻结 Agent 工件和 provider exact-request 测试保持通过。

这些测试证明恢复边界可以迁到 Stateless frontier-order 调用，不证明 Agent 搜索有效。

## 验证

- 两项离线恢复测试：普通运行 5.507 秒通过；定向 race 57.838 秒通过；
- `go test ./...`：通过，`cmd/control-experiment` 249.608 秒；
- `go vet ./...`、race manifest 精确分片、无 v1/退休 Experiment 路径审计和 `git diff --check`：通过；
- 没有重复 R3 的 etcd/raft 高成本定向 race；其 8 分钟无 race report 的未知状态继续如实保留。

当前 `cmd/control-experiment` 与 `internal/controlexperiment` 合计 28,809 行 Go，相比 M5.23g
检查点 33,832 行净减 5,023 行。

## 仍未删除的代码

`campaign-etcdraft-agent-v1`、Campaign planned-attempt/model-call 和 macro Intent 暂时保留。原因是 R4a
只完成调用级 sidecar 的恢复语义，还没有把它绑定进 R3 的 Campaign config、attempt artifact 和
observation。此时删除会造成“调用可恢复、attempt 不可恢复”的断层。

## 下一阶段：M5.23R4b

把 restricted Agent 作为第三种 Stateless Campaign strategy：

1. Agent method/knowledge/model budget 与 sidecar root 绑定进入同一 Campaign spec；
2. 每个 attempt 的 6 次 frontier calls、18 次 qualified executions、discovery 和 model work 进入同一
   sealed artifact；
3. Campaign 恢复必须先重放已完成 call，再从 prepared call 继续，ambiguous call 形成 durable failure；
4. 新入口通过离线 mock create/resume/ambiguous 门后，删除旧 Agent CLI 与无消费者
   planned-attempt/Campaign Planner/macro Intent 闭包。

R4b 完成前不开始 M5.24 holdout。
