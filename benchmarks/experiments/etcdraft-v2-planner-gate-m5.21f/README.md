# M5.21f Planner Usefulness Gate Pilot

## 边界

本目录保存一次预注册的 public calibration。它使用 official etcd/raft v2、2 attempts、
32 decisions per attempt、seeds 171/172，以及 `campaign-preference-only-v2` 精确输出契约。
DeepSeek transport 固定为 `deepseek-v4-flash`、temperature 0、thinking disabled、0 retry，
最多 2 calls/8192 tokens per attempt。

本实验检查 proposal validity、valid execution yield 和行为选择，不是 holdout、缺陷检出或
Agent 优势实验。PSS 与 monitor 只用于描述执行结果。

## 实际结果

| 项目 | 结果 |
|---|---:|
| Campaign status | `stopped/attempt-limit` |
| valid proposals / calls | 2 / 2 |
| committed attempts | 2 |
| primary / replay work | 66 / 66 |
| model work | 2 calls / 6027 tokens |
| PSS samples / unique | 66 / 32 |
| workload planned / offered / completed / pending | 2 / 0 / 0 / 2 |
| monitor checks / triggers | 4 / 0 |

两次回复都严格使用 `prefer.backend_ids` 和 `prefer.actions` 数组，通过 strict parser、
preference-only validator 和 trusted compiler。M5.21e 第二轮出现的字段漂移在本次没有复现；
这只是一份 2-call 可靠性见证，不代表输出契约已经统计稳定。

两轮 Agent 都按相同顺序选择：

```text
action-class-random, admissible-uniform
```

因此实际 backend 都为 `action-class-random`。第一轮没有 feedback，Agent、zero-model 和新自适应
基线选择一致。第二轮输入明确包含第一轮 25 个新增 PSS、workload 0 completed/1 pending；Agent
仍与 zero-model 一样保留 `action-class-random`，而同视图同权限的确定性自适应基线会选择尚未
尝试的 `admissible-uniform`。

Agent 还在两轮都列出两个 backend 共同支持的全部 8 个 `Prefer.Actions`。这些偏好不会改变当前
compiler 选择的 backend/strategy，却会改变 intent、compiler-work、plan 和
`IntentExecutionInstance` digest。这说明不能把完整审计 identity 的变化直接计为行为差异；后续
gate 必须比较 target、effective backend/strategy、seed、decision/fault/work budget 和 Runtime
composition 等真正送入执行器的受信任输入。纯偏好元数据差异不能触发重复 SUT 执行或被计为
Agent 搜索增益。

## 可审计 identity

- Summary digest：`d4e1c1a00ad441fcbb7d551ffdf91f95b4c74fcdcce71a54651fa9b4f3b43ba5`；
- Observation digest：`1426105aa284f8ae424b01ebaa6d324a7a6020ac6e7b592e24c0bcc396386050`；
- Core PSS state-set digest：`6cc2545b8da9bc2ffc6ac41974f31f0b6fb35be6c87ca2994c6aedac700acfef`；
- attempt 1 plan / instance：`d182c6d24388c0ed7f78b769f259fbaffa214b365044fda7b32579a3dc167f22` /
  `7e5ff505fd9d3d6e229a38cb58a61d84ac75213b4da7fc6f1f8ac6d47a48c6a8`；
- attempt 2 plan / instance：`d182c6d24388c0ed7f78b769f259fbaffa214b365044fda7b32579a3dc167f22` /
  `638a2160dfab542d78147e12959ce4ed767bad59b6d244f50d8f9461259d5628`。

Exact-config recovery 产生字节一致的 Summary 和 Observation，没有新增模型调用。完整 Campaign、
Summary 和 Observation 使用确定性 gzip 保存；密钥不进入 request、result、工件或 Git。

- Campaign archive SHA-256：`a0ccca92d064f78cc07f3d0a3edb63d77a9ef1b866b9283ee5cdead965534afd`；
- Summary archive SHA-256：`dcdd15a11f867bc86828de5595b171bb00a87f4d05fda465bbd2e8d35cfd6159`；
- Observation archive SHA-256：`1e3ca48852c152ff34e7edaf81079a2e98beaf5f5caa8e5f712afff12daa95c8`。
