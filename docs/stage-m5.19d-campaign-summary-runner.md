# M5.19d：Campaign Summary、Artifact Reader 与离线 Runner

日期：2026-08-09

状态：设计冻结，实现在推进

## 目的

M5.19c 已证明真实 provider 能进入可恢复 Campaign，但目前只能由 Go 回归直接组装，调用方还必须理解
checkpoint 私有目录布局。本阶段增加最小读取面和显式 runner，使系统第一次具备“给定配置，运行一段
Campaign，得到小型结果索引与完整内容寻址证据”的用户流程。

```text
new/resumed Campaign directory + exact expected config
                         |
                         v
RecoverCampaignDirectory -> validated CampaignRecovery
                         |
             +-----------+-----------+
             v                       v
     CampaignSummary             ReadAttemptArtifact
  small records/totals/status    exact committed bytes
             |                       |
             +-----------+-----------+
                         v
 explicit etcd/raft runner -> existing M5.19c provider/coordinator
```

## 冻结边界

1. Summary 是协议无关派生视图，只包含 config/target/spec/head identity、每个小型 terminal record、
   checkpoint 前后 digest、累计 WorkLedger、elapsed、stop reason 和可选 durable failure marker。
2. Summary 不嵌入 artifact bytes、Report、ExecutionBundle、PSS samples 或 Oracle 结论；其大小不随单个
   trace 内容线性膨胀。artifact 仍只在 M5.19a 内容寻址目录中保存一次。
3. `ReadAttemptArtifact(ordinal)` 只能从带私有校验令牌的完整恢复状态解析已提交 ordinal；读取后必须
   再核对 regular-file、大小和 SHA-256。不能借此读取 orphan、pending 或任意路径。
4. Summary 状态只有 `running`、`stopped`、`failed`：可恢复的 running head 不是完整结果；机械 stop
   不是“测试通过”；failure marker 不是零成本 terminal attempt。
5. Runner 仍位于 etcd/raft composition root。它只构造 M5.19c spec/provider/config、创建或明确恢复
   Campaign、调用通用 Coordinator、构造 Summary 并写入全新输出文件。
6. 新运行必须要求 Campaign 目录不存在；恢复必须由显式 flag 请求且目录已存在。config/spec/target
   identity 任一漂移均在运行 SUT 前拒绝。
7. summary 输出文件必须不存在，不能覆盖旧结果。Coordinator 返回普通错误时，只要可信恢复状态仍可
   汇总，runner 必须先持久化 failed/running summary 再返回错误。
8. wall-clock 仍只是运维 ceiling；attempts、primary/replay work、decisions 和 model cost 才是账本。
9. 本阶段不聚合跨 attempt PSS/Coverage，不运行 Oracle，不生成最终评分，不读取 key，不调用模型。

## 最小验收

- [ ] stopped/running/failed 三类 Summary 均可 canonical seal/revalidate；
- [ ] Summary totals 等于 terminal records 逐项求和，head/previous digest 顺序不能篡改；
- [ ] Summary JSON 不包含真实 artifact payload，读取 API 只返回 exact committed ordinal；
- [ ] artifact 缺失、替换、超限、ordinal 越界和被修改的 recovery token 均被拒绝；
- [ ] 新 runner 完成至少一个多-attempt etcd/raft Campaign 并输出可重验 Summary；
- [ ] 显式 resume 延续同一 config/head，非 resume 不接管已有目录，summary 不覆盖旧文件；
- [ ] runner 失败路径在返回错误前保存可验证 summary；
- [ ] 普通全量、vet、受影响 race、结构审计、总体规划镜像和 `git diff --check` 通过；
- [ ] 不读取 key、不调用模型、不提交大型实验 artifact。

## 明确不在本阶段完成

- 跨 attempt PSS/Coverage/Oracle 聚合和最终测试评分；
- Agent 根据 Summary 或 feedback 生成下一 attempt；
- 第二个协议的 Campaign provider/runner；
- 对后台 goroutine 或外部进程进行强制终止；
- 声称 etcd/raft、ConsensusAtlas 或测试方法正确、完备或更优。
