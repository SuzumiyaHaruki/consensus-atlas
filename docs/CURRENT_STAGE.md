# 当前阶段

日期：2026-08-08

阶段：M5.13 单调用 DeepSeek Planner smoke 完成

## 当前输入、处理与输出

```text
新共识实现 + 官方接口/源码 + 最小协议知识
                    |
                    v
       Execution Binding + Semantic Mapping
                    |
       (planned Adapter kit) + Core PSS IR
                    |
                    v
统一 Action / Runtime / Replay / Coverage / Agent
```

M5.13 已在 M5.12 边界上进行一次真实 `deepseek-v4-flash` 调用。Proposal 编译成功，run 1 完成
32 primary + 32 replay decisions；run 2 在 decision 3 的 exact `drop-message/node=n1` rule 不可达，
得到显式 `execution-failed`。本次累计 1 call、744 tokens、34 primary 和 32 replay decisions；没有
重试、PSS summary 或方法优势结论。

## 本轮冻结决策

- 基础 Action 保持协议无关，不为 EPaxos、Raft 或 BFT 新增协议动作；
- 新目标只增加薄 `Execution Binding` 和声明式 `Semantic Mapping`；
- Manifest、Check、Yield、Collect、digest、审计和通用资格逻辑由规划中的共享 Adapter kit 承担；
- PSS 改为固定的 Core PSS IR：参与者、epoch、decision unit、value、evidence 及有限关系；
- Family/协议特有状态只能作为可选 Extended PSS，不能改变 Core IR 或通用统计器；
- EPaxos 是第三目标候选，用于测量边际接入成本和否证抽象，不因为 Agora 使用过就降低准入要求。
- EPaxos framing 必须留在薄 Binding；Runtime 继续只接收协议无关的完整 Message Item；
- M5.8b worker witness 通过，但 Runtime integration、完整 Adapter 和 Qualification 仍为 false；
- legacy consumer 集合只能显式收缩，新增边由 `make test` 机械拒绝；
- 在线采样器只观察已完成 Action，不拥有 `EnabledActions` 或 `Select`；
- 初始 step 0 和每个已应用公共 Action 都采样，内部 `RawNode` 微步不单独采样；
- 20 unique states 只是单轨迹状态发现数，不是覆盖率或质量评分；
- 未采样的 decision 仍进入预算、curve 和 prefix area，不能通过删除无新状态步骤优化指标；
- 45 unique states 是两轨迹 union，不是固定分母、覆盖率或策略优势；
- v2 报告状态只能是 `measurement-complete`；Oracle/Coverage 前不给出“测试通过”；
- 后续策略必须复用唯一 `controlexperiment` 路径，不得再建第二套 run/report/replay 实现。
- policy seed 只允许进入 Policy/config digest，禁止传入 Runtime seed、Adapter Reset 或原生 entropy；
- 单个公开 Random seed 的 PSS 差异不是方法优越性结论。
- Planner scope 只能由可信 composition root 生成；Agent proposal 的结构中不存在 PSS、Runtime、
  run set、decision budget 或 replay 开关；
- 模型输出必须经拒绝 unknown/trailing JSON 的唯一 decoder，再机械编译；
- `proposal-rejected` 与 `execution-failed` 都必须计 proposal attempt，后者还保存失败前实际执行工作；
- Planner 仍必须复用唯一 `Execute` 和 `Replay`，不得创建 Agent 专用执行路径。
- 真实 Planner transport 每次只允许一次 API call；模型客户端不得隐藏 HTTP retry；
- 模型只读取 Scope digest/run/budget/replay、公共节点/Action kinds、目标描述和 Proposal schema；
- Runtime seed、PSS ID、enabled set、Action IDs、trace、Coverage 和 Oracle 均不得进入 prompt；
- model usage 来自 provider response 并与 Go 计算的 request/response digest 绑定，但不是签名账单证据；
- 运行期不可达不能自动回退、忽略 selector 或免费重试。

本轮边界和未证明事项见 [M5.13 单调用 DeepSeek Planner](stage-m5.13-one-call-deepseek-planner.md)。

## 本阶段完成

- `internal/psscore` 提供封闭 schema、Runtime Control Context projector、participant/value canonicalizer
  和 digest 校验；
- etcd/raft v2 的 Semantic Mapping 只读取稳定公开 Evidence，不导入原生 Raft 类型；
- 三节点真实自然选主集成测试与 native identity/absolute time/payload/旧 incarnation 变形测试通过；
- 两个现有 Adapter 的重复职责已审计；因为行为差异大，本阶段没有创建空 `adapterkit`；
- Runtime、Action、Adapter 契约、Replay、Oracle、Coverage 和旧 PSS 均未修改。
- EPaxos source/tree/file digest、12 项事实、5 个 blocker 和下一步限制已冻结为可复核工件；
- 官方生产包在 legacy GOPATH 模式构建成功，三节点进程完成一个命令；
- feasibility 的“事实到决策”由测试机械推导，源码事实本身仍是人工审阅并由 digest 绑定；
- EPaxos 未进入生产依赖图，未创建第三套 Adapter 或空共享 kit。
- 55 字节帧为一次底层 Write，5,139 字节帧为两次 Write，否证 chunk-as-message；
- codec-aware assembler 对 5,139 字节帧得到稳定 ID，release 转发 5,139 字节、drop 转发 0 字节；
- probe 以 race detector 运行并逐字节匹配冻结结果；新增生产 Go 0 行、test-only Go 212 行。
- 28 条 legacy execution 生产 import 边已冻结并纳入默认测试；
- 两个 92 行的实现专用 qualification CLI 合并为 60 行统一 CLI；
- etcd/raft 与 HashiCorp 两份 qualification report SHA-256 均保持不变。
- `protocolstate.Discover` 不再读取 v1 `core.TraceRecord` 或协议快照；
- Raft v1 精确保持 step 1/4/5、unique 1/1/2 的 discovery 曲线和首见 witness；
- 三节点 etcd/raft v2 自然选主的 Core PSS 直接复用通用账本；
- discovery 生产文件从 68 行降为 61 行，整个 `internal/protocolstate` 生产代码净增 0；
- Action、Runtime、Agent、Coverage、schema 均未修改，旧执行边重新审计仍为 28 条。
- 协议无关 `OnlineSampler` 对连续 step、record/snapshot、Evidence digest 和 logical time 做机械校验；
- etcd/raft `CorePSSMapper` 只包装已有 Semantic Mapping，没有新协议控制；
- 三节点轨迹的 30 个 samples 和 20 个 unique states 在新 Runtime 上逐值一致；
- legacy cross-run experiment 原样隔离到 `internal/protocolstate/legacyexperiment`；
- `internal/psscore` 生产依赖不包含 legacy execution 包，并加入 architecture gate。
- 通用 Aggregate 接受显式 measured decisions 与 optional samples，保留共享 root、全局预算和首见 witness；
- v1 compatibility adapter 由 141 行降为 69 行，根 Aggregate 为 117 行，不存在两份统计算法；
- 旧 128-decision Random 产物与 fresh 运行的 discovery 子树 SHA-256 相同；
- v2 progress/lifecycle 各 32 decisions，各自 23 states，union 45，prefix area 1454；
- 两个 v2 run 都用全新 Adapter/Runtime 做严格 replay。
- 协议无关 `controlexperiment` 冻结等长 run budget、声明式 Action policy 和强制 fresh replay；
- 报告保存 config/manifest/trace/sample/report identity、Core PSS witnesses 和 primary/replay work；
- 首个公开 v2 工件为 193,818 字节，primary/replay 各 2 setup + 64 decisions = 66 work units；
- 落盘 JSON 可重新机械校验，报告 digest 为
  `8d05dae4da3a9fe2a3d8765276aa77d99107f96bc75dc1faadc9a97740f8ec27`；
- 通用执行器纳入 v2 architecture gate，不导入具体共识或 legacy Runtime。
- `uniform-random-policy/v1` 以 public seed、decision 和 enabled-set digest 做确定性拒绝采样；
- 相同 policy seed 的 report/trace digest 一致，不同 policy seed 的 report digest 不同；
- policy seed 变化不改变 Runtime config 或 Runtime seed digest；
- Random seed 1 的两个 run 为 29/26 states，union 53，prefix area 1843，严格 replay 2/2；
- Random 与 fixed 的 primary/replay 都是 2 setup + 64 decisions = 66 work units；
- M5.10 fixed 文件 SHA-256 保持
  `bf3c2cb50c8cfa37c8c6d9e90cc0a56575f18e3ad3ab9a3a9d07eec957091247`；
- M5.11 生产 Go 净增 84 行，没有新增 package、执行路径或 report schema。
- 可信 `PlannerScope` 冻结 experiment/PSS identity、Runtime config、run set/order、budget 和 replay；
- `PlannerProposal` 只含每 run priority/rules/public seed，编译器拥有 Policy version/ID 和 Config；
- strict decoder 拒绝 unknown field 与 trailing JSON；run set/policy 非法产生稳定 rejection code；
- `ExecutionFailure` 保存 phase、reason、run/decision 和截至失败点的真实工作账本；
- Replay progress 复用唯一 Replay 算法，失败时不再丢失已完成初始化/decision 成本；
- deterministic stub 为 0 model calls/tokens、1 proposal attempt、2×32 decisions、45 states、
  prefix area 1454，attempt digest 为
  `16f75884e58703bfaad970e06d455573a410600c72a193c25a1a8ef87a3715b9`；
- M5.10 fixed 与 M5.11 random fresh 运行均和既有 JSON 逐字节相同；
- 本阶段生产 Go 约净增 579 行、测试约净增 200 行；没有新增 package、Action、Adapter、执行/
  Replay 算法、PSS 或 Coverage 路径。
- 共享 `internal/modelcommand` 统一三个模型客户端的环境隔离、进程执行和输出上限；
- DeepSeek Planner 使用 official endpoint allowlist、key metadata/no-follow 检查、temperature 0、
  thinking disabled、1,800 max tokens、5 分钟 deadline 和一次调用；
- strict completion parser 新增 duplicate JSON key 拒绝；
- `PlannerModelAudit` 保存 prompt/request/response digest、模型身份、response metadata、usage 和 duration；
- 真实 proposal 编译 accepted；run 1 完成并 replay，run 2 decision 3 机械不可达；
- 真实工件计 555/189/744 prompt/completion/total tokens，primary/replay work 36/33 units；
- 真实 attempt digest 为
  `e90e464a3a1fc010cb80fb60eeafff90c77ba4f6d2c4ec27ccf6bf93b225e87f`；
- M5.12 stub 工件 fresh SHA-256 仍为
  `38bd333fb18af03ba958863cfbab83356f09689325c4361e4568ab8c3db60f52`；
- M5.13 Go/Python 生产代码净增约 306/125 行、测试约净增 271 行；同时删除三个旧入口中的
  86 行重复命令/secret-file 实现，没有新增执行、Replay、PSS、Coverage 或 Oracle 路径。

## 当前没有完成

- progress/lifecycle 是两条固定策略，不是 Random/DFS/Agent 方法对比；
- Random 只有一个公开 base seed，53 对 45 不能证明方法优势；
- summary 只保存 trace/sample identity，不是可单独送入 Replay 的完整 bundle；
- wall time、CPU 和 peak RSS 明确未采集，不能用逻辑 work units 替代；
- Snapshot 尚无 Adapter YieldID，同 logical-time evidence/snapshot 配对仍依赖可信 composition root；
- 旧 Raft PSS 仍是当前 v1 实验消费者；旧指标与新 Core 指标明确分版，不能拼接；
- EPaxos 只在 test-only worker 中冻结 frame，尚未由 Control Runtime 持有，也没有 validated capability；
- 自动构造器后的无竞争安装、完整 RPC registry 和三节点消息闭环尚未证明；
- stable yield、自然时间、持久恢复和严格 replay 仍 missing/Unsupported；
- Coverage、Oracle、Agent、Campaign 和 benchmark 尚未迁移到 v2；
- Core dependency shape 仍是 coarse kind multiset，且当前 participant canonicalization 只面向最多八节点
  的有界小集群；
- 本轮不产生新的协议控制、覆盖百分比或缺陷检出结果。
- 当前真实 Agent proposal 没有跑满预算，因而没有 PSS discovery summary；
- 一次性静态 proposal 无法预知瞬时 message/effect enabled set；
- provider usage 不是密码学签名的账单证据；
- transport 在形成有效 proposal/audit 前失败时尚无独立 attempt ledger；
- 没有第二次模型调用、repair 结果或 Agent 与 fixed/random 的可比完整轨迹；
- `bindings`、runner、Campaign、autoonboard、candidate catalog 与 migration harness 仍是直接消费者，
  因此 legacy Engine/Host/Driver 删除门仍未打开。

## 下一步

进入 M5.14：先冻结 transport-attempt ledger 和最小机械 repair 输入，只公开 phase/reason/run/decision
和对应 proposed rule；不公开 enabled set、Action IDs、trace、PSS、Coverage 或 Oracle。之后才允许
恰好一次 repair call，并累计两次 proposal/model cost。目标只是验证修复闭环能否形成可执行计划，
不做方法优势声明。

## 阅读入口

1. [M5.13 单调用 DeepSeek Planner](stage-m5.13-one-call-deepseek-planner.md)
2. [M5.12 受限 Planner 编译边界](stage-m5.12-restricted-planner-boundary.md)
3. [M5.11 确定性 Random 基线](stage-m5.11-deterministic-random-baseline.md)
4. [M5.10 可保存的 v2 实验执行器](stage-m5.10-v2-experiment-executor.md)
5. [M5.9d 通用跨运行状态聚合](stage-m5.9d-generic-cross-run-aggregate.md)
6. [M5.9c 可信在线 Core PSS 采样](stage-m5.9c-online-core-pss-sampling.md)
7. [M5.9b 通用状态发现账本](stage-m5.9b-generic-discovery-ledger.md)
8. [M5.9a v1 消费者冻结与第一批减负](stage-m5.9a-v1-consumer-freeze.md)
9. [M5.8b EPaxos message-port worker](stage-m5.8b-efficient-epaxos-message-port.md)
10. [M5.8a EPaxos 可行性检查](stage-m5.8a-efficient-epaxos-feasibility.md)
11. [M5.7a Core PSS 与 etcd/raft Mapping](stage-m5.7a-core-pss-etcdraft.md)
12. [M5.7 接入与 PSS 目标收敛](stage-m5.7-onboarding-scope-convergence.md)
13. [Control Runtime v2 设计](control-runtime-v2.md)
14. [M5.2.5 v1/v2 对照与删除门](stage-m5.2.5-v1-v2-comparison.md)
15. [总体规划](ConsensusAtlas-总体规划.md)
