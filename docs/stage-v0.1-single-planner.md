# v0.1 阶段总结：单 Planner 覆盖闭环

日期：2026-08-05

## 阶段结论

本阶段完成了第一个真实的 Agent 测试闭环：DeepSeek Planner 读取冻结
Profile、当前 Coverage Debt、剩余预算和上一轮机械 finding，提交一个受限
Test Plan；确定性 Coordinator 负责验证、执行、重放、Oracle、计分和停止。

基础设施阶段已完成，但“Agent 能可靠修复失败计划并优于固定策略”尚未成立。
最终 live run 在第一次计划中获得真实进展，随后因同一错误因果结构反复出现而被
去重器和 no-progress 策略终止。这是可信的负结果，也是引入独立
Scenario/Critic Agent 前的合适停点。

## 已实现边界

```text
Frozen Profile + private Coverage Ledger
                 |
        detached Debt/Budget view
                 v
        DeepSeek Planner subprocess
                 |
          untrusted Proposal
                 v
schema + target + budget + causal-shape validation
                 |
       deterministic Test Plan concretizer
                 v
official etcd/raft -> replay -> Oracle -> Evidence Matcher
                 |
       private Ledger + mechanical Finding
                 +---------------------------> next attempt
```

- `internal/campaign.Session` 在多个增量计划之间私有持有同一个 Ledger；
- `internal/agentcampaign.Coordinate` 是唯一编排与停止决策者；
- Blackboard 只追加 request、generation audit、proposal、execution 和 finding，
  并用 SHA-256 父链检测篡改；
- Agent 不能提交 observation、evidence、Oracle 结论、分数或新义务；
- 未知/已覆盖目标、超预算计划、重复 ID 和重复协议因果结构在执行前拒绝；
- proposal 的随机 seed 和不透明应用值不参与因果结构去重，避免仅改名重试；
- 只有 replay stable、conformant 且无执行错误的 run 可提供强覆盖证据；
- 模型进程不继承父进程环境，key 只由受限 Python 子进程读取；
- 即使 proposal schema 无效，已发生的模型 token usage 仍计入预算和审计；
- 达到 attempt、连续无进展、run、decision 或 token 上限会机械停止。

## 最终 live run

命令：

```bash
make agent-campaign-etcdraft
```

新机器默认从仓库根目录的 `key.txt` 读取凭据，也可使用
`DEEPSEEK_KEY_FILE=/path/to/key.txt` 覆盖；只分析已提交报告不需要模型凭据。

冻结身份：

- Profile：`raft-campaign/raft-three-node-cft-v1`；
- Profile digest：`09e9e507c84234c15cebf942d5d43aa08511cc0d6a48d7c3dc36aa3399dc14b6`；
- SUT：官方 `go.etcd.io/raft/v3 v3.6.0`；
- Model：`deepseek-v4-flash`，temperature 0，thinking disabled；
- 上限：6 attempts、3 consecutive no-progress、20 runs、1024 decisions、
  200000 model tokens。

最终结果：

| 指标 | 结果 |
| --- | ---: |
| Stop status | `no_progress_limit` |
| 固定义务覆盖 | 16 / 55 |
| 加权分数 | 31.00 |
| Unsupported | 2 |
| Actionable debt | 37 |
| 实际 runs | 4 |
| 实际 scheduler decisions | 128 |
| Model tokens | 36,534 |
| Blackboard records | 18 |
| Replay failures | 0 |
| Oracle violations | 0 |

逐轮行为：

| 轮次 | 机械结果 | 新覆盖 | 说明 |
| ---: | --- | ---: | --- |
| 1 | `progress` | 16 | 4 runs 中 1 个有效轨迹产生证据，另 3 个 run 有执行错误 |
| 2 | `plan_execution_error` | 0 | prepare 在 leader 尚未建立时要求执行 propose，稳定 selector 无匹配 |
| 3 | `duplicate_proposal` | 0 | 只改变 seed/应用值，协议级因果结构与第 2 轮相同 |
| 4 | `duplicate_proposal` | 0 | 再次重复同一结构，触发连续三轮无进展停止 |

运行时报告生成在 `artifacts/agent-campaigns/deepseek-etcdraft-v0.1.json`。
仓库提交了本轮正式报告的确定性 gzip 快照
`artifacts/agent-campaigns/deepseek-etcdraft-v0.1.json.gz`，以及三个基线的
`.json.gz`；其余 `artifacts/` 仍被 git 忽略，避免提交构建缓存和调试轨迹。
可使用 `gzip -dc report.json.gz | jq` 直接分析。报告也可由上述命令重建，但模型
服务不是冻结二进制，因此不能假设未来响应逐字相同。

## 基线与能够得出的结论

固定的 partition-recovery 计划分别使用 Random 和 DFS，在 256 个实际 decision
下均得到 25/55、45.00 分、0 replay failure、0 Oracle violation。完整人工专家
Suite 使用 1624 个实际 decision 得到 39/55、66.38 分。

| 方法 | 计划来源 | 实际 decisions | 覆盖 | 分数 |
| --- | --- | ---: | ---: | ---: |
| DeepSeek Planner v0.1 final | Agent 自适应 | 128 | 16/55 | 31.00 |
| Fixed partition + Random | 人工固定 | 256 | 25/55 | 45.00 |
| Fixed partition + DFS | 人工固定 | 256 | 25/55 | 45.00 |
| Expert Suite v1 | 人工固定 | 1624 | 39/55 | 66.38 |

这些数字只能用于 plumbing 和失败模式审计，不能证明方法优劣：样本只有一次，实际
预算不同，fixed partition 基线是人工设计，模型 endpoint 也可能更新。此前开发
smoke 曾在 256 decisions 达到 21/55，但它发生在无效 proposal token 计费和新
因果去重修复之前，因此不作为最终主结果。

目前能够可靠声称的是：

1. Agent 可以在不知道专家计划的情况下产生可执行计划并取得真实强证据；
2. Agent 无法自报覆盖，所有 16 项由冻结 matcher 和 Ledger 判定；
3. 错误计划会得到精确 finding，语义重复不会继续消耗 Runtime 预算；
4. 单 Planner 没有可靠利用失败反馈，当前没有显示优于固定计划的证据。

## 自审查与下一阶段

本阶段的可信执行目标通过；M4.6 的“自动修复失败计划”退出条件未通过。主要问题
不是开放权限不足，而是角色混合：一个 Planner 同时选择债务、构造低层 prepare
并自行诊断失败，Flash 模型容易复制上一轮结构。

下一阶段应保持当前可信内核不变，拆成三个窄角色：

1. Coverage Planner 只选择一组债务和测试意图；
2. Scenario Agent 只把意图翻译成 Test Plan，并优先使用短 prepare、把并发工作留
   给 Explorer；
3. Critic Agent 读取结构化 execution finding，对失败的具体 step/selector 给出
   与原因果结构不同的修复约束。

完成上述拆分后，再做至少 20 个独立 campaign、固定模型快照（若服务支持）、统一
decision/token/wall-clock 报告，并与 Random、DFS、无反馈 Agent、单 Agent 和
Planner+Scenario+Critic 做消融。只有到那时才能评价 Agent 是否提高覆盖效率。

## 验证命令

```bash
make campaign-baselines-etcdraft
make agent-campaign-etcdraft   # real API cost
make test
go vet ./...
go test -race ./...
```
