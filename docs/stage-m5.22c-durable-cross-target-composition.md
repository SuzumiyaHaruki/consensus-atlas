# M5.22c：可恢复的跨目标组合账本

日期：2026-08-11

## 结论

M5.22c 将 M5.22b 的同一父 Intent 双目标执行放进了现有 Campaign 的 durable planned-attempt 边界。每个
目标仍拥有独立 `CampaignConfig`、checkpoint chain、plan 文件和内容寻址 artifact；跨目标层新增的只是
一个 reference-only composition ledger。没有修改历史 Campaign identity，没有新增 Coordinator，也没有
把 report、bundle、trace 或 Qualification 复制进外层账本。

## 执行和恢复顺序

```text
common parent intent
        |
        +--> target A child plan --> PreparePlannedAttempt --> execute --> CommitAttempt --+
        |                                                                           |
        +--> target B child plan --> PreparePlannedAttempt --> execute --> CommitAttempt --+
                                                                                    |
                              reopen both Campaign directories ---------------------+
                                                                                    |
                         CrossTargetCampaignLedger (digest references + work only)
```

target artifact 保存 report、bundle 和 outcome；planned attempt 已存在 Campaign `plans/`，artifact 只保存其
digest，避免再复制完整 planner view。恢复时重新验证 checkpoint chain、artifact digest、planned input
digest、parent/child intent 语义、report/bundle/outcome 关系、Manifest identity 和 WorkLedger。

## 真实结果

| target | decisions | primary/replay | artifact | 恢复 |
|---|---:|---:|---|---|
| etcd/raft v3.6.0 | 42 | 44 / 44 | `7542194…5674b` | 成功 |
| OmniPaxos 0.2.2 | 29 | 31 / 31 | `dafb872f…0a51d` | 成功 |

composition ledger digest 为 `5ff91b96…a93347`，只汇总 75/75 work units。两个 PSS identity 仍作为
target-local 引用存在，状态数和状态 key 均未进入总账。测试把临时目录中的一个 artifact 替换为其他字节
后，`RecoverCampaignDirectory` 拒绝恢复。

## 代码体积约束

本阶段没有引入第二套 runner/coordinator/store。新增生产代码集中在一个通用 artifact/reference ledger
文件（344 行）和一个实验 composition root（229 行），合计 573 行；同时删除了已被该闭环取代的独立
M5.22b runner，M5.22b 测试改为复用 M5.22c 的同一次真实执行，避免增加第二轮双目标测试。仓库只保存
小型 summary，不保存两份完整 Campaign artifact。后续若不需要多轮跨目标 batch，不应继续扩展这两个类型。

## 未证明

- 没有 LLM 调用或 Agent 方法比较；
- 当前是每目标一个 attempt，不是多轮自适应跨目标 Campaign；
- 没有跨目标 PSS 等价关系、总覆盖率或状态数排名；
- 没有 candidate/control verdict 或缺陷发现；
- 没有证明 BFT、leaderless 或新协议零配置接入。

## 下一阶段

M5.22d 不再扩展持久化模型。最小任务是让同一受限 Planner proposal 在一次共同 request 中产生 parent
preference，再由可信层投影为两个已经存在的 planned attempt；用 deterministic baseline 与 blind model
各运行一个小型、相同预算的双目标 batch。若没有可比较的新增行为或外部结果，应停止增加跨目标结构，
转入 holdout/mutant 评测准备。
