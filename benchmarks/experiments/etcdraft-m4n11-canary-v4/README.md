# M4n11 etcd/raft canary v4

本目录保存非正式 capability canary 的结论和压缩审计证据。`evidence/` 包含完整
provider journal、原始与 evaluator fresh Bundle、evaluator 报告、上游测试日志和可读取
本地 SUT commit 的 Git bundle。它足以复核 Prompt、Action binding、Replay 和当时源码，
但本结果仍不是预注册的 formal trial。

## 固定边界

- ConsensusAtlas 基线：`1103a52`；
- 五节点 etcd/raft，`public-fixed`，单 Episode；
- Risk/Scenario 均使用 DeepSeek 官方 `deepseek-v4-flash`；
- 最多 12 calls、240,000 observed tokens、64 Scenario decisions；
- Agent 只获得协议知识、TargetSurface、中立源码搜索和一次 bounded read；
- Agent 可读源码、Go local replacement 与执行 SUT 均来自同一个干净 checkout；
- SUT 只保留三处语义修改，没有专用测试或解释性修改注释。

## 结果

Risk Agent 用三次调用完成 `search → bounded read → portfolio`，自主提出
`duplicate-vote-resp-quorum`，并真实引用其读取的 `raft.go`。Scenario Agent 使用满八次
调用，总计 30 个 post-root decisions，但停在 `candidate-term` 之前，故 candidate 只是
机械可执行，witness 未实例化。

模型侧共有 11 个 dispatch 和 11 个 result，全部为 `content-ready`、
`finish_reason=stop`，合计 202,046 observed tokens，没有 unreconciled 调用。

Bundle 的 root boundary 为 40，Agent path 为 30。在线 Oracle 把 step 37 的
election-safety violation 归入 root prefix，没有给 Agent finding credit。独立
`evaluator-replay-v1` 重新执行 70 个 Action，得到相同 Trace/Bundle digest；随后 saved
Bundle audit 重算五个 monitor，仍只得到：

```text
term 2 leader n1 is not backed by a legal election quorum: voters votes=2/5 need=3
```

该结果证明 canary 管线和 root/post-root attribution 正常，不证明 Agent 已发现问题。
受控 SUT 的普通上游测试也已直接失败，因此不能宣称未知缺陷或 Agent 独占发现。

这里的源码 grounding 状态是 `completed-used`：候选确实引用了真实读取片段。它不表示
该片段定位了缺陷；本轮读取位置与实际受控修改位置不同。

## 下一决策

不要直接启动六 Episode 长跑。当前 deterministic root 在 Agent 接管前已经暴露目标异常；
若继续用同一 root，任何后续 election finding 都只能是 Oracle sensitivity。下一步应先选择
或构造一个在相同受控 SUT 上 Oracle-clean、且不泄露修改信息的 root，再用独立 canary 验证
它确实把潜在 finding 留在 Agent path。Formal root boundary 目前仍是多字段交叉验证的
方法侧事实，不是独立 evaluator-owned boundary。
