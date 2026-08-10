# M5.21c：Durable Planned Attempt

## 目标

M5.21b 已能把已提交 attempt 的 PSS 增量归因到可信 choice，但 etcd/raft Campaign 仍在启动时编译
固定 plan。M5.21c 要完成第一个 zero-model 自适应闭环：

`prefix Observation -> Planner View -> proposal -> compiled plan -> instance -> execution -> checkpoint`

并保证规划结果在进程恢复后不被重新计算或重复计费。

## 设计边界

不把 Planner 塞进通用 Coordinator。Coordinator 继续只拥有：

- exact base request 与 allowance；
- provider 调用；
- terminal result 校验；
- artifact、WorkLedger、checkpoint 和 failure marker 提交。

新增协议无关 `CampaignPlannedAttempt/v1` envelope，并由一个 planned provider 组合 Planner、可信
compiler 和既有 target provider。

## Planned Attempt 内容

Envelope 保存并做 canonical digest binding：

- exact `CampaignAttemptRequest` digest；
- 完整 `CampaignPlannerView`；
- preference-only proposal；
- `CompiledIntentPlanV2`；
- coordinator-owned `IntentExecutionInstance`；
- `CampaignExecutionChoice`；
- 本次 planning 的 `ModelWork`。

验证必须重新执行 proposal 权限检查、plan-intent、instance-plan、choice-instance 链，并检查
planning work 位于 request 的 model allowance 内。Envelope 不包含 target trace、PSS witness、Oracle
或 candidate/control identity。

## 持久化状态机

Campaign store 增加 `plans/000...N.json`，使用与 artifact/checkpoint 相同的 fsync + no-replace
提交规则：

1. 根据当前 head 构造 exact base request；
2. 若 `plans/N` 已存在，重验其 request/head/view/plan 链并直接复用，Planner 调用数必须保持不变；
3. 若不存在，构造 prefix Observation 和 Planner View，调用 zero-model Planner，可信编译并生成
   instance/choice；
4. 先 durable commit `plans/N`，然后才能进入 target execution；
5. terminal artifact 必须引用 planned-attempt digest；checkpoint record 的 `InputDigest` 也必须等于
   该 digest，而不再只引用未规划的 base request；
6. 恢复时允许且最多允许一个 `head+1` envelope；更远 ordinal、缺失的已提交 envelope、内容篡改、
   request/head/config 漂移均 fail closed。

Planning `ModelWork` 与 execution work 只在 terminal record 中合并一次。未提交 terminal record 的
pending envelope 不进入 Campaign totals；恢复复用时也不重复增加 planning work。

## 明确不声称 exactly-once 的窗口

本阶段 Planner 为 zero-model deterministic fixture，calls/tokens 为 0。若进程在 envelope durable 后、
terminal checkpoint 前中断，规划不会重算，但 target execution 仍可能再次执行；这是既有 Campaign
的 execution-at-least-once 窗口，本阶段不改变。

真实远程模型调用仍不接入。若进程在外部服务已收费、响应尚未 durable 时中断，仅凭本地状态无法
区分“未调用”和“已调用但未记录”。未来必须增加 durable call-intent + ambiguous terminal 状态，
或使用服务端幂等键；在此前不得自动重试并声称模型调用 exactly-once。

## 代码预算与验收

- M5.21c Go 净增不超过 700 行，包含测试；
- 不新增 Runtime、scheduler、target executor、CLI strategy 或模型 transport；
- 原 `CampaignCoordinator` 只允许最小的 provider-supplied input digest 绑定，不解释 Planner 类型；
- store 测试覆盖 no-replace、tamper、head+1 resume、future ordinal、缺失 committed plan；
- 组合测试模拟“plan 已落盘后进程中断”，恢复后 Planner call count 不增加；
- 真实 3-attempt etcd/raft Campaign 每个 checkpoint record、artifact、Observation choice 必须引用同一
  planned-attempt 链；
- 全量 test/vet、相关定向 race、`git diff --check` 通过。

## 完成前不作出的结论

- 不证明 deterministic fixture 优于 Random/DFS/专家；
- 不证明 PSS 发现量代表完备覆盖；
- 不证明远程模型调用可恢复；
- 不证明 SUT execution exactly-once；
- 不接真实模型。

