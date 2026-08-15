# 当前阶段

日期：2026-08-15

分支：`feature/agentic-consensus-testing`

阶段：A8cR4a in-memory paired method evaluation

## 一句话状态

ConsensusAtlas 已进入效果评测阶段：etcd/raft 的确定性方法与 Scenario Agent 已接入同一个 paired trial runner，
共享 root、自然推进、执行预算、qualified executor、Replay 和 Oracle；真实 OpenRouter 公开预跑已经完成，
evaluator 已能从该执行单位恢复两 arm 的 Bundle 和完整 Campaign 成本。现在它还能先对全部
opaque trial 的 BuildAudit、二进制和预期身份做集合预检，只有全部通过才按稳定 Trial ID 顺序启动。
`defect-eval -paired-scenario` 已暴露该路径；重运行时完整 trial 由 evaluator 直接恢复，只有中断 trial 才重新启动
SUT 并进入既有 Campaign resume。完整批次随后在内存中分别聚合 deterministic 和 Agent 的 control pass、
false positive、candidate survived/killed 与 invalid，不再把方法轴与 SUT 版本轴混在一起。

## 输入什么

一个目标接入当前需要：

- thin Adapter：把官方实现的节点、消息、时间、生命周期和 host effect 映射为公共 Action/Item；
- qualification：机械声明并验证该 Adapter 实际拥有的控制能力；
- protocol knowledge 与 TestHypothesis：人工可编辑 JSON，描述协议风险而不包含 verdict；
- workload 与实验预算：同一 JSON 中声明目标操作、Runtime、fault envelope、episode/token/work 上限；
- Risk/Semantic projector、PSS mapper 和独立 monitor；
- 显式 opt-in 的 OpenRouter 模型与 key 文件，仅在真实 Agent 运行时提供。

当前活动输入位于：

- `plans/agent/etcdraft-leader-change-inflight-v1.json`；
- `plans/agent/omnipaxos-message-loss-before-decision-v1.json`；
- `benchmarks/experiments/etcdraft-v2-root-corpus-m5.23e/root-corpus.json`（仅用于既有公开实验复现；paired
  runner 缺省从当前 SUT 生成 fresh root）；
- `adapters/<target>/` 与 `qualifications/<target>/`。

## 如何处理

```text
knowledge/hypothesis/workload + qualified Adapter
                         |
                         v
            Explorer 或 Scenario Agent
                         |
               当前可信 Frontier
                         |
                         v
        Control Runtime 具体化并执行 Action
                         |
          +--------------+--------------+
          |                             |
          v                             v
     fresh Replay                PSS / Risk / Oracle
          |                             |
          +--------------+--------------+
                         v
             Campaign artifact/summary
```

职责边界：

1. Agent 可以排列可信候选、选择一个当前 Action，并根据机械 feedback 修正计划；
2. Agent 不能设置 root、预算、fault envelope、Oracle、PSS 等价关系或最终 verdict；
3. Runtime 只执行当前 enabled Action，消息在 release 后由 Runtime 持有；
4. 自然时间只通过到期 temporal Action 前进，不提供任意强制超时；
5. completed episode 进入唯一 qualified executor，生成完整 ExecutionBundle；
6. fresh Replay、PSS/Risk projector 与独立 Oracle 从真实 Trace 重算结果；
7. Campaign 负责 episode/work/token/time 预算、checkpoint、终止原因和恢复；
8. 已终止 Campaign 可只依赖 config/checkpoint/artifact 恢复，不需要 SUT、provider 或 key。

## 得到什么

一次 Session 当前输出：

- durable provider 请求/响应与 transport attempt 审计；
- 每步 Action 选择、机械反馈和自然推进记录；
- 完整 Trace、Snapshot、OperationHistory 与 ExecutionBundle；
- primary/replay/model work 和 wall-clock 统计；
- fresh Replay 是否稳定；
- Core PSS samples、跨 episode 唯一状态并集；
- RiskWitness reached/not-reached 与满足的 milestones；
- target-local 和通用 Oracle violations；
- completed、stopped、failed 与明确 stop reason。

PSS/Risk 是解释性指标，不直接形成 defect kill。正式 finding 必须由 replay-stable、conformant 的独立
Oracle/evaluator 产生。

## 当前实现能力

| 目标 | 控制与执行 | Agent Session | 结果验证 |
|---|---|---|---|
| etcd/raft | strict scheduler-owned 消息、自然时间、crash/restart、持久化 effect | 可恢复多 episode | Replay、Core PSS、Risk、TraceIntegrity、Agreement、target-local monitors |
| OmniPaxos | 外部 worker、消息丢弃、自然时间、workload | 可恢复多 episode | fresh worker Replay、Core PSS、Risk、TraceIntegrity、Agreement |
| HashiCorp Raft | 部分 interceptable 黑盒能力 | 未进入正式 Agent Session | 用于暴露黑盒适配上限 |

公共 `internal/` 生产代码没有导入具体共识实现。目标专用 projector、monitor 和 composition 留在 Adapter、
Qualification 或 `cmd/control-experiment` 边界。

## 当前实验证据

- etcd/raft Session 已连续完成两个 qualified episode，并验证同 episode feedback、跨 episode root 隔离、
  PSS 状态并集、成本归属和无 provider/SUT 恢复；
- OmniPaxos 已用同一 Campaign coordinator 完成两个 qualified episode，Risk reached、fresh Replay stable、
  Oracle 0 violation，并在终止恢复时不重访 worker/provider/key；
- 真实 OpenRouter 校准证明 bounded retry、structured output、调用计费和 durable recovery 可用；
- 2026-08-14 的 A8 入口预跑使用 `deepseek/deepseek-v4-flash` 完成两个 episode：4 次模型调用、30,772 tokens、
  Replay 全部稳定、Risk reached、Oracle 0 violation；但两个 episode 的 Trace 与 38 个 PSS 状态完全相同，
  因而它是可用性证据和重复性负证据，不是 Agent 效果证据；
- A8b 真实同底座 paired preflight 使用同一 root 和一个 episode：deterministic 与 Agent 都消耗 2,443 primary、
  1,253 replay work，得到相同 Trace、38 个 Core PSS 状态、Risk reached 和 0 Oracle violation；Agent 额外使用
  2 次模型调用、15,418 tokens。该公开样本是可归因的负结果，不是 Agent 优势证据；
- A8cR2 在缺省 paired 路径中按当前 SUT 的 source trace 生成 fresh corpus；默认身份与构建时注入的替代
  SUT 身份都通过同一 paired 集成测试，没有读取或放宽官方 corpus identity；
- A8cR3a 已用本地伪二进制贯通 evaluator-owned 进程启动、fresh-root summary、两 arm Campaign 恢复和
  BuildAudit/Bundle BuildID 匹配；该测试没有读取 key 或访问模型服务；
- A8cR3b 已用六个 opaque fixture 验证全集预检、稳定启动顺序和证据身份回绑；篡改最后一个
  候选的预期二进制摘要时，六个 runner 调用都不会发生，也不会创建批次工件目录；
- A8cR3c 已验证首轮执行六个 trial、终端重运行零 runner/零 key 访问，以及未审计 semantic bytes 在任何
  SUT 启动前被拒绝；该测试使用本地 fixture，未访问 OpenRouter；
- A8cR4a 将六个 fixture 对每种方法独立归类为 3 control pass + 3 candidate survived；人为仅修改 Agent 轴一个
  Bundle schema 时，Agent 记录 1 invalid，deterministic 仍为 0 invalid；
- 两协议的 artifact 都保存完整 Bundle/Risk/Oracle，并能拒绝篡改；
- deterministic canonical/uniform stateless Campaign 和 Semantic Explorer 仍可作为 A8 对照方法。

这些结果只证明闭环和证据边界可用，不证明 Agent 比 baseline 更有效，也不证明目标协议正确。

## A8a：同执行底座对照

原 stateless DFS 会在生成 PSS 汇总后丢弃完整 Bundle，而且它的遍历单位与 Scenario Agent 的单步干预加自然推进
不同。直接比较两者会把搜索底座差异误记为 Agent 效果，也不能为每个 Scenario 产出同形的 Replay/Oracle 证据。

A8a 因此没有新增另一套 Runtime、schema、hash、gate 或评分公式，而是：

- 把 Scenario episode core 与 OpenRouter journal 解耦，planner 只提供 ScenarioPlan JSON 和模型成本；
- 增加 target-local 的确定性 etcd/raft planner；
- 该 planner 机械选择 `crash current coordinator`，经过相同可信 natural-progress closure 后选择
  `restart old coordinator`；
- 两种 planner 最终进入同一 `executeEtcdraftScenarioQualified`，得到同形 Bundle、Risk、PSS、Replay 和 Oracle；
- 集成测试证明确定性路径不访问 provider、模型成本为 0，并达到相同三个 Risk milestones。

增加该对照是为了解决“异构方法输出无法归因”的具体失败，不把它称为效果结果。当前尚未增加实验 CLI，
也没有用公开 etcd/raft 校准宣称方法优劣。

## A8b：同预算 paired trial runner

新策略 `etcdraft-a8-paired-scenario-v1` 复用现有 Campaign 数据面，在同一根目录下运行：

- `deterministic/`：一个零模型成本的确定性 Scenario episode；
- `agent/`：一个 OpenRouter Scenario episode；
- `summary.json`：只汇总两 arm 的执行预算、实际 primary/replay work、model work、Trace、PSS、Risk 和 Oracle。

两 arm 的 execution budget 必须完全一致；Agent 的 calls/tokens 作为额外成本单列。每个 arm 的完整 Bundle 仍只存一份
在原 Campaign artifact 中，根 summary 不复制大型 Trace。停止后恢复会从两个 Campaign 重算 summary，不重复访问
provider 或 key。fixture 配对实验中两 arm 得到相同 Trace、PSS/Risk/Oracle 和 primary/replay work，Agent 额外使用
2 次模型调用；这正是公开校准应报告的负结果形态，而不是把“LLM 参与”自动写成优势。

首次真实 OpenRouter paired preflight 在 deterministic arm 完成后，于 Agent 首次响应读取阶段终止。工件显示
`duration=60002ms`、`transport_attempts=1` 和零 token；检查发现 etcd/raft Session 虽把 JSON 中
`model_max_retries=2` 绑定进实验输入，执行时却误用了 CLI client 的默认 0。现已让执行端使用校验后的 authoring
client，并用 Session/A8 回归测试确认 artifact 中实际 retry 上限为 2。失败目录保持不变，不能重命名为有效 trial；
随后一次运行在请求 dispatch 后被外部中断，恢复按设计返回 `STATELESS_AGENT_CALL_RECOVERED_AMBIGUOUS`，没有
不安全地重复发送可能已被服务端处理的调用。第三个全新目录中的真实 paired preflight 完成：两次调用均一次返回
`content-ready`，最终结果与上面的 A8b 真实负结果一致。

默认 etcd/raft authoring input 已从两个 episode 改为每 trial 一个 episode。多 episode coordinator 与恢复能力继续由
显式测试覆盖；真实 A8 重复通过独立 trial 目录组织，避免同一 root 的重复 Trace 被算作独立状态发现。

## A8c：evaluator 侧 paired Campaign evidence

`cmd/defect-eval` 现在能读取一个 paired trial 下的 `deterministic/` 与 `agent/` Campaign，并保留：

- 各 arm 的完整 `ExecutionBundle`；
- Campaign config/checkpoint 与 primary/replay/model 总成本；
- planner 身份、target identity、terminal outcome 和 artifact 链接。

读取器机械拒绝不同 SUT identity、不同 execution budget、未终止 Campaign、非 stable artifact 布局和
不合理的模型成本。它没有把 deterministic 当作 control、也没有把 Agent 当作 candidate：前者是
方法轴，后者是 SUT 版本轴。

这个分离很重要：真实 A8b 中每 arm 的 Campaign 成本为 2,443/1,253，而嵌套 Bundle 的局部执行成本
只有 56/56。直接把 Bundle 交给旧 `FreshBundleEvidence` 会漏记 root 构建、frontier 重建和规划成本。

旧 `root-corpus.json` 仍精确绑定官方 SUT 的 Manifest、source Bundle 和 Trace；显式提供该文件时，任何身份
或配置错配仍会被拒绝。A8cR2 通过另一条缺省 paired 路径解决 candidate 构建的输入问题，不放宽旧校验。
A8cR3a 已补 evaluator-owned opaque binary 启动与 BuildAudit 关联，A8cR3b 已补内部多 trial 预检与编排。
A8cR3c 已补可恢复 CLI，A8cR4a 已补内存中的双方法 candidate/control 聚合。当前仍缺双方法契约和 sealed result，
因此这条 fresh-root 路径还不是正式 private pair 结果。

## A8cR1：Scenario materialization 去重

优化前，Scenario 每处理一个 Action 会 fresh Replay prefix 重建 Frontier，选定 Action 后又 fresh Replay
同一 prefix 以 materialize child，最后再用新 Adapter Replay child 做独立验证。现在第一步返回的
短生命 Runtime 由 materialization 直接消费。Select 前会重新核对 prefix digest/长度、snapshot digest 和
Action 成员关系；Select 后立即关闭该 Runtime，fresh child Replay 不变。因此 Agent 仍然只看到独立
验证过的状态。DFS 和 Semantic Explorer 原有 materialization 路径继续使用独立重建。

对 A8b 同一条 28→54 decision Trace 按当前 ledger 机械重算：

- Primary：`2443 -> 1338`，减少 1,105（45.23%）；
- Replay：仍为 `1253`，因为 26 个 child 仍逐个 fresh 验证；
- Bundle：仍为 `56/56`，Agent model work 仍独立计数。

A8 配对与确定性 Scenario 集成测试保持 Trace/Risk/Oracle 行为不变。成本回归测试要求
`ChildMaterialization` 不再新增 setup/prepare/prefix replay，而 `ChildVerification` 仍有 fresh setup。
per-SUT fresh root 已在 A8cR2 完成；segment-level Replay 批处理留作后续独立优化。

## A8cR2：per-SUT fresh root

paired runner 在未提供 `-stateless-corpus` 时，先由当前 SUT 生成 source Bundle，再以同一个 target-local 规则
选择三个 Action 里程碑：初态、首次 Invoke、首次 Invoke 后 crash 节点的 Restart。该规则从当前 Trace 计算
decision 位置，不复制官方 corpus 的 0/28/54、prefix digest 或 Manifest。生成的 corpus 继续由现有
`StatelessRootCorpus` 校验并绑定当前 source/Trace/Manifest。

fresh source、corpus、root、frontier 和 spec 只构造一次，并作为同一个 prepared inputs 分别传给 deterministic
与 Agent Campaign；两 arm 的逻辑账本仍各自计入 source work。根 summary 仅在 fresh 模式记录 source、corpus、
selection rule、root digest 和实际 root decisions，旧的显式 corpus 模式保持原有 summary，便于复现 A8b 工件。

集成测试同时覆盖默认 build identity 和通过 `ldflags` 注入的替代 SUT identity。两者都完成 paired Scenario，
说明新路径不依赖官方 corpus identity。它尚未启动真实 source-variation binary，也未产生正式 candidate/control
verdict；A8cR3a 已在 evaluator 侧补齐单 SUT launcher，正式多 trial 聚合仍未完成。

## A8cR3a：evaluator-owned paired binary launcher

`cmd/defect-eval` 现在可以接收一份已由现有 `loadFreshTrials` 校验过的 BuildAudit 与二进制，在独立临时路径
恢复可执行文件，并在 evaluator 超时下启动 `etcdraft-a8-paired-scenario-v1`。命令不传
`-stateless-corpus`，所以被测二进制必须走 A8cR2 的 SUT-local fresh-root 路径。

进程成功后，evaluator 严格读取根 `summary.json` 和 deterministic/Agent 两个 Campaign，检查 fresh-root mode、
selection rule、source/corpus/root digest、共享 SUT identity、相同执行预算及实际 model work。两个终端 Bundle
中的 Manifest BuildID 还必须等于 BuildAudit 的 `SUTBuildIdentity`，避免“二进制摘要正确但 Campaign 来自另一
构建”的证据替换。

本地集成测试使用只复制预建 Campaign fixture 的临时脚本；脚本遇到 `-stateless-corpus` 会立即失败。正向路径
完成全部恢复，篡改 BuildAudit BuildID 后被拒绝。这里没有调用 OpenRouter，也没有把 launcher 暴露成新的正式
CLI 模式。

## A8cR3b：multi-SUT batch preflight

evaluator 现在复用已有 formal fresh inputs 的 opaque trial 描述，在执行任何 SUT 之前完成整体预检：

- BuildAudit 本身有效，TrialID 与预期 build identity 对应；
- 审计文件摘要、二进制摘要与 formal variant 全部相等；
- 审计中的二进制摘要与实际加载 bytes 一致；
- Trial ID 唯一，输入数与预期 variant 数相等；
- OpenRouter 输入路径、key 路径、模型 ID 和超时参数在创建工件目录之前完成基本校验。

预检全部通过后，trial 按 Trial ID 排序，逐个交给 A8cR3a 单 SUT launcher。每个返回证据还会重新核对
BuildAudit、binary、fresh-root 规则和两 arm Bundle BuildID。这解决了“执行到最后一个候选才发现输入错配，
前面候选已消耗模型调用”的具体失败。

本阶段没有新增结果 schema、评分或 gate。现有 formal evaluator 只接收一个 MethodSpec 和 v3 Bundle，而 paired
Scenario 是两个 Campaign method 和 v2 终端 Bundle；在语义没对齐前不强行复用旧 verdict 形式。R3b 只实现顺序
内部编排，当时尚无 resume 或正式 CLI；这两项由 R3c 补齐。R3b 本地测试只用 fixture runner，未读取 key、
未访问 OpenRouter，也没有产生非公开 candidate/control 结论。

## A8cR3c：resumable paired batch CLI

`defect-eval -paired-scenario` 现在复用 formal contract、exposure audit、fresh inputs 和 artifact root，显式接收
semantic JSON、OpenRouter key 路径、model ID 和每 trial 超时。该模式不接收 `-out`、MethodSpec 或已提交 Bundle，
因为它只负责产生两种 method 的 Campaign evidence，还不生成 candidate/control verdict。

恢复直接复用现有目录状态：

- 完整 trial 从 `summary.json` 和两个终端 Campaign 严格恢复，不启动 SUT、不读 key；
- 不完整 trial 使用同一审计二进制和 `-campaign-resume`，由已有 Campaign journal 决定继续位置；
- 批次根目录中的未知项、非目录 trial 或符号链接会被拒绝，不覆盖原工件。

formal exposure audit 还必须包含这次 CLI 实际读取的 semantic JSON 字节摘要。这一检查针对具体的
时间差失败：先审计文件 A，执行时通过 CLI 换成含私有标签的文件 B。Git 提交、版本号、类型和普通测试都不能约束
运行时选择的外部文件；因此这里复用既有 SHA-256 exposure digest 做字节关联，没有新增摘要类型或独立 gate 体系。

当前仍是顺序执行，没有真实非公开 inputs，也没有调用 OpenRouter。批次不额外写另一份状态或大型 JSON；
Campaign 目录就是唯一恢复依据。

## A8cR4a：in-memory paired method evaluation

新的内存评测原语按单个 method 接收完整 opaque Bundle 集。它复用 formal contract 中与 SUT 有关的 pair、
control/candidate、root cause、Profile、预期 build/config、预算和 trusted composition，并对每个 Bundle 执行：

- Bundle 自身校验、同 method schema 一致性、Profile/build/config 匹配；
- decision/primary-work 预算、projector 校验和 TraceIntegrity；
- contract 指定 monitor 的精确解析和执行；
- control-pass/false-positive、candidate-survived/killed 或 invalid 分类。

paired evaluator 把 deterministic 和 Agent Bundle 拆成两个独立集合，分别执行上述原语。CLI 只在 stdout 报告两边的
`killed/candidates`、false positive 和 invalid 计数；不写新 JSON、不 seal 为 formal verdict。`BundleTrialResult` 中的 work 仍是局部
Bundle 成本，效率比较必须继续使用外层 Campaign work，不能把 56/56 再次误当为 2,443/1,253。

这仍是 calibration 而不是正式 holdout result。v1 `FormalBenchmarkContract` 同时绑定单个 MethodSpec 和 v3 schema，
无法同时表达 paired Scenario 的两个 method。R4a 因此只复用其 SUT/composition 子集，method schema 从各自第一个 control
Bundle 取得并对同方法全集强制一致。在定义双方法契约前，不持久化这份评测。

## 本轮减负结果

本轮已完成：

- 删除 etcd/raft 与 OmniPaxos 两套被 Session 覆盖的单 episode Scenario CLI；
- Session 策略改为稳定名称 `etcdraft-agent-session-v1` 与 `omnipaxos-agent-session-v1`；
- 合并公共 semantic authoring 校验与 etcd/raft Ready effect 构造；
- 删除无用目标类型别名；
- 将 durable Agent journal、Semantic Explorer 文件及 experiment/execution 文件改为职责名称；
- 将 30 个阶段编号测试改为行为名称并同步现有 race shard 清单；
- 把 `control-experiment.run()` 收敛为 flag 解析与四路显式分派；
- 删除 11 份阶段流水账，历史由 Git 保存。

当前 Go 规模（包含 A8cR4a）：

- 生产代码：34,101 行；
- 测试代码：15,542 行；
- 合计：49,643 行。

A8 前减负净减少 647 行 Go；A8a 在不增加新文件的情况下净增加 154 行 Go。CLI 主入口的最高圈复杂度热点仍已
消除；A8b 为可执行 paired runner 增加 428 行，其中生产 342 行、测试 86 行。测试约占 31%，这是确定性执行、
恢复和反例验证的主要可信边界，不进行比例式删除。

## 当前边界

尚未完成：

- 非公开 candidate/control 正式效果实验；
- paired trial 的双方法正式契约与 sealed candidate/control 结果；
- A9 的双角色 Agent 消融；
- 多次重复实验与置信区间；
- fixed Profile obligation coverage 接入当前 Session 汇总；
- PSS/义务指标与隐藏根因检出的相关性验证；
- OmniPaxos target-local 共识 monitor；
- Protocol/Hypothesis Agent 与 Scenario Agent 的正式双角色在线衔接。

另外：

- projector identity 是随实现维护的显式版本 ID，A8 必须同时记录 Git 提交；
- HashiCorp Raft 的部分能力不能被描述为 scheduler-owned；
- 旧阶段 artifact 需要 checkout 对应 Git 提交解释，不维持当前 HEAD 的兼容 CLI；
- 普通测试不会读取 key 或调用外部模型。

## A8 下一步

A8 先形成一个最小、可预注册的配对实验：

1. 准备非公开 candidate/control pair，并保持 Planner 输入对身份与 verdict 盲化；
2. 固定模型、总 model calls/tokens、primary/replay work、wall clock 与 semantic exposure；
3. A8 先比较同底座 deterministic Scenario 与单 Scenario Agent；双角色 Agent 留在 A9；
4. 主要指标为独立根因检出、正确 control false positive 与有效 trial 数；
5. PSS、Risk、义务覆盖、计划修正次数和成本只作为解释性指标；
6. 每个 arm 重复多次，不以单次成功或状态数宣称优势。

下一小步是根据 R4a 实际证据字段，定义能同时绑定 deterministic/Agent 的最小 method contract；
只有它能解决现有单 MethodSpec 不能表达双方法的具体失败，才允许增加 sealed 结果。
在预注册前不增加第三 Agent、通用 DSL、新 PSS 维度或新的评分公式。

## 当前验证

已通过：

- `go test ./... -count=1`；
- `go vet ./...`；
- etcd/raft Adapter 全包；
- etcd/raft 与 OmniPaxos 双 episode Session；
- etcd/raft deterministic/Agent 共用 Scenario core 与 qualified testing 的集成测试；
- A8 单 episode paired runner 的同预算、同 Trace 和无 provider/key 恢复测试；
- evaluator 侧 paired Campaign 恢复、完整成本保留和 SUT identity 错配拒绝；
- Scenario prepared Runtime 消费、materialization 去重和保留 fresh child verification 的成本回归；
- per-SUT fresh root、两 Planner 共享 prepared inputs，以及替代 SUT build identity 回归；
- evaluator-owned paired binary 启动、fresh-root summary 严格恢复和 BuildAudit/Bundle identity 关联；
- multi-SUT 全集预检、稳定 trial 顺序、返回证据重验及篡改时零 runner 调用；
- paired Scenario CLI 参数隔离、semantic exposure bytes 关联、已完成 trial 零 SUT/key 恢复和中断目录 resume；
- deterministic/Agent 独立 monitor 聚合、control/candidate 分类及单轴 schema 漂移不污染另一轴；
- semantic authoring、CLI 参数边界与 qualified execution；
- `internal/controlexperiment` 与 `internal/defectbench`；
- unused production check；
- race shard manifest；
- 旧路径与旧名称引用检查；
- 两份总体规划的字节一致性检查；
- `git diff --check`。

本轮没有改变并发执行语义，因此不重复运行耗时的全仓 race；已有 race shard 清单已完成机械审计。

## 阅读顺序

1. [总体规划](ConsensusAtlas-总体规划.md)
2. [架构](architecture.md)
3. [Control Runtime](control-runtime-v2.md)
4. [指标](metrics.md)
5. [Coverage Kernel](coverage-kernel.md)
6. [Defect Benchmark](defect-benchmark.md)
