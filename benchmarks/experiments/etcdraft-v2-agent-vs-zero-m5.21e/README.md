# M5.21e Matched Agent/Zero-model Pilot

## 预注册边界

两臂共享 official etcd/raft v2、2 attempts、32 decisions per attempt、seeds 161/162、
trusted compiler/executor/replay/PSS/monitor 链。Zero-model 使用确定性 planner；Agent 使用
preference-only DeepSeek planner，最多 2 calls/16384 tokens。只匹配执行 ceiling，Agent 的模型
成本单独列出；这不是相同总成本、holdout 或方法优势实验。

Zero-model 先运行，Agent 后运行。没有换 seed、补跑或对失败的 Agent Campaign 做 resume
重调。

## 实际结果

| 项目 | Zero-model | Agent |
|---|---:|---:|
| status | `stopped/attempt-limit` | `failed/provider-error` |
| committed attempts | 2 | 1 |
| actual scheduler decisions | 64 | 32 |
| primary / replay work | 66 / 66 | 33 / 33 |
| model work | 0 | 2 calls / 5211 tokens |
| PSS samples / unique | 66 / 46 | 33 / 23 |
| workload planned / offered / pending | 2 / 0 / 2 | 1 / 0 / 1 |
| monitor checks / triggers | 4 / 0 | 2 / 0 |

Agent 第一次调用消耗 2431 tokens，产生与 zero-model 第一次完全相同的
`action-class-random` choice、compiled plan 和 execution bundle。在共同 32-decision prefix 上：

- choice digest 和 bundle digest 相同；
- PSS discovery curve 逐点相同；
- Agent 的 23 个状态全部在 zero-model 集合中；
- intersection=23、zero-only=23、agent-only=0；zero-only 部分全部来自 Agent 未执行的第二 attempt，
  不能归因为搜索质量差异。

Agent 第二次调用已完成并消耗 2780 tokens，但 proposal 违反类型契约：`prefer`
使用了单值 `backend_id` 和额外 `strategy`，而契约要求 `backend_ids`/`actions`。strict
unknown-field parser 以 `EXPERIMENT_GUARDED_INTENT_JSON_INVALID` 拒绝；未产生第二个 plan、
artifact 或 PSS evidence。第二次 1 call/2780 tokens 由 failure marker 引用 durable result 进入
Summary，证明 M5.21d4 terminal accounting 在真实调用下有效。

## 解释

该 pilot 支持两个有限结论：

1. 当前 Agent 在第一个共同前缀中没有产生可观测的搜索差异；
2. 当 `Prefer` 的精确输出形状没有以机械 schema/template 明示时，多轮模型输出可能漂移。

第二点是下一阶段应修复的 Agent 服务面问题：在 prompt/view 中提供精确、可机械生成的
preference-only 输出 schema 和枚举值，而不是放宽 parser 或在同一 pilot 中重试。

PSS 数、workload pending 和 monitor 零触发均不支持 Agent 优劣、Coverage 完备性、缺陷检出或
协议正确性结论。

## 工件与恢复

为避免将约 2.6 MB trace 和派生曲线 JSON 展开为数万行 Git diff，每臂保留可直接
阅读的 `summary.json`；`observation.json` 与 crash-safe `campaign/` 以确定性 gzip 保存。
Campaign archive：

- zero archive SHA-256：`7cb3dbfbf132261e8b9f08f4d4d5b06253655ce6f21a0eccb3a7a337bb46ddd5`；
- agent archive SHA-256：`14c3988ae11bc650de7476e0d765562d9ace377ea9ab2ffa8536a881f059fa28`。

Observation archive：

- zero SHA-256：`ba8789e74b2172c3a67e2257a241c7d5edbd986f289b7c942ff510ad99cde84e`；
- agent SHA-256：`3e279eb094a0ac0048d6c6904aa4a29287dada46b07bf9bcccf5feae291d47a7`。

在新目录中执行 `tar -xzf <arm>/campaign.tar.gz` 即可恢复 `campaign/`；使用
`gzip -dc <arm>/observation.json.gz` 读取 Observation。归档前两臂均以
exact config 恢复读取；重建 Summary/Observation 与保存文件字节一致。Agent 恢复返回
durably failed 且未读 key/调用 transport。所有 JSON/artifact 在归档前通过语法校验；工件中无
key exact match、`Authorization`、`Bearer` 或 `key.txt`。
