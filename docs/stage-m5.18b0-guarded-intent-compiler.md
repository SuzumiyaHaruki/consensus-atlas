# M5.18b0：Guarded TestIntent 宏观 compiler

日期：2026-08-08

状态：完成无模型 one-shot 宏观闭环；M5.18b 尚未完成

> M5.18b1 在首次真实调用前发现 backend view 缺少 `max_fault_envelope`，并补入该可信上限。本文记录的
> view/intent/plan digest 是修正前历史值；当前接口 identity 见 M5.18b1 阶段总结。Runtime 与执行
> report/bundle identity 没有因此改变。

## 结论

本阶段第一次形成当前 v2 路径上的真实 Agent 消费边界，但没有调用 LLM：人工冻结的协议知识包经真实
Manifest/Qualification 生成 defect-blind `AgentSemanticView`，一个受限 `GuardedTestIntent` 由确定性
compiler 解析到已有 qualified backend，再进入唯一 Experiment executor。

```text
ProtocolKnowledgePack + trusted backend catalog
             + real Manifest / Qualification
                         |
                         v
              AgentSemanticView
        (no build/candidate/root-cause/Oracle ID)
                         |
                  strict JSON intent
                         |
                         v
       deterministic hard-filter + preference ranking
                         |
                         v
                 CompiledIntentPlan
                         |
            recompile from trusted inputs
                         |
                         v
             existing qualified executor
                         |
          real Trace hard-action validation
```

## Agent 能做什么

第一版 intent 只允许引用：

- 知识包中已有的 risk ID；
- decisions、FaultEnvelope、额外 required capability/ActionKind；
- backend ID 和 ActionKind 偏好。

Agent 不提交未来 decision number、ActionID、节点 ID、payload、PSS state key、Oracle、Coverage 或 verdict。
proposal 使用固定 schema，拒绝未知字段、尾随 JSON 和提交者提供的 digest；digest 由可信 parser 生成。

## hard 与 prefer

知识包 risk 自带的 capability、ActionKind 和 allowed backend 不能被 intent 删除。compiler 将其与 intent
额外 `must` 求并集，然后依次检查真实 Qualification、Manifest Action、backend selector、decision ceiling
和 FaultEnvelope。任一 hard 条件不成立即返回稳定错误，不选择较弱方案。

`prefer` 不参与准入。compiler 对所有 hard-eligible backend 做固定排序，记录每次 miss；当所有 backend
偏好都不可用时，才使用 catalog 的稳定 fallback rank。当前 `CompilerWork` 记录候选检查和偏好检查，
但尚未并入多 attempt MethodLedger，因此还不是完整方法成本。

静态 `SupportedActions` 只说明 backend 可以选择该 ActionKind。执行结束后，`CompiledIntentPlan` 还会扫描
真实 Trace，要求每个 hard ActionKind 至少实际出现一次；否则 intent 失败，不能改写为 preference miss。

## defect-blind 边界

`AgentSemanticView` 保存 Pack、Profile identity、Adapter ID、validated capability、Manifest Action 和 eligible
backend，但不保存 BuildID、Qualification digest、Manifest digest、candidate/control、root cause 或 Oracle。
测试将同一 Manifest/Qualification 的 BuildID 改为另一个 opaque identity 并重新 seal，得到完全相同的
view digest。执行前 compiler 仍使用完整可信输入重建 view，因此外部不能靠自算 digest 自授 capability。

当前协议知识包是人工冻结的小型 etcd/raft 公共知识，不是 Agent 自动生成的协议事实。其文本不参与
compiler 决策，typed risk requirements 才参与；知识包内容的独立 exposure audit 留到真实模型调用前。

## 真实 etcd/raft 见证

one-shot intent 选择 `leader-change-with-inflight-proposal` risk，hard requirements 包含 invoke、自然时间、
消息投递和 crash/restart。它偏好一个不存在的 `dpor` backend：

| 项目 | 结果 |
|---|---|
| hard-eligible backend | 2 |
| backend preference miss | `dpor` |
| frozen fallback | `action-class-random` |
| compiler work | 2 candidate + 2 preference checks = 4 |
| execution | 96 decisions，98/98 primary/replay |
| hard ActionKind | invoke/deliver/temporal/crash/restart 均在 Trace 中出现 |
| replay | stable |

该执行复用 M5.17a 冻结 seed 1 的 action-class 路径：report digest 仍为
`fc0cb500876b1d3d75dc7d8f83dd672513d1c010c523069f12e3be2f6472260a`，bundle digest 仍为
`b766e3f13e8be015b950032df449762b2ee90e3c80d87e87383c8ff80ac6c9b1`。这证明 compiler 没有产生第二套
scheduler；也意味着本轮没有新的 defect verdict。

新冻结身份：

| 工件 | digest |
|---|---|
| ProtocolKnowledgePack | `1921bdc375f9618436d7ae31d426ba2087dc715443ec0f822628ce0e51199980` |
| compiler catalog | `76ab1542d5d37fcde02b0913f7080197db8c2fa16aa05c513142a05d659f89dd` |
| AgentSemanticView | `304428ebbcce8ff5b0e0ac67db392568634fec3d6e77b307737b4f639b585680` |
| GuardedTestIntent | `670583ff58ef8bbea3f12930fb57c7b7cd16bdb5509488e6b35b233bf753037e` |
| CompiledIntentPlan | `e6692986455554208d55a8d3984be5f30f65a5d5310e0512f3d3502348c27f8d` |

## 代码成本与验证

M5.18a 收口时为 17,824 行 production / 7,106 行测试。本阶段为 18,862/7,282，合计
26,144 行；净增 1,038 行 production 和 176 行测试。增量主要是五个 canonical artifact 的深拷贝、
digest/输入重算、strict proposal parser 和 Trace-backed hard validation。这个增量已偏大，因此
M5.18b1 不得同时预建 feedback 或 multi-Agent 抽象。

验证结果：

- `make test-fast`、`make test`、`go vet ./...`、`make test-race-full`：通过；
- race 使用显式 20 分钟 package ceiling，`cmd/control-experiment` 耗时 309.564 秒；
- 132 个非 `artifacts` JSON、23 个 schema JSON、109 个 Markdown 本地链接：通过；
- Python 历史 Agent 已删除，`unittest discover` 正常发现 0 项；
- 两份总体规划逐字节一致，`gofmt` 和 `git diff --check` 通过。

## 当前没有证明

- 没有调用 LLM，不能称为 Agent 方法实验；
- 没有 batch feedback view、near-miss 或 one-shot/feedback 消融；
- 没有把模型调用、token、非法输出、compiler work 纳入统一 MethodLedger；
- 没有 private holdout，也没有与 uniform、action-class、mutation 或专家 intent 比较；
- 单个手工 risk 和 etcd composition 不证明 KnowledgePack 已经普适；
- static backend catalog 仍是 target-owned composition，不是自动发现；
- 没有证明 PSS/Coverage 能预测缺陷检出或协议正确性。

## 下一步

M5.18b1 只增加最小模型调用边界和调用账本：模型只读取冻结 view JSON、只输出 strict intent proposal；
provider/model/参数、prompt/request/response digest、token、时长、parse/compiler 结果必须留痕。先做一次公开
one-shot smoke，并与完全相同 view 下的手工 intent 对照。模型成功之前不加入 feedback、多 Agent 或新搜索
backend；失败输出也必须形成可审计结果，不能静默改成人工 intent。
