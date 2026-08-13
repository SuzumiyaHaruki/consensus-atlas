# A2b3b：etcd/raft 真实 DeepSeek 公开校准

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：冻结 spec 下的单次真实模型校准完成；进入 A2c masked-semantics 消融设计

## 1. 输入

本阶段没有修改 A2b3a 冻结的任何实验输入。真实调用使用：

- official etcd/raft v2 三节点 Adapter；
- M5.23e `invoked` root，固定为 28 decisions 和 15-action frontier；
- Raft family `leader-change-with-inflight-proposal` RiskWitness；
- target-local prefix projector、公开协议知识和 semantic-only hypothesis；
- depth 2、最多 16 WorkItems、12,000 search work；
- deterministic semantic best-first baseline；
- DeepSeek v4 Flash、temperature 0、max output 1,200、0 retry；
- 最多 2 calls、16,000 reported tokens。

仓库内冻结输入为
`benchmarks/experiments/etcdraft-v2-semantic-explorer-a2b3/spec.json`。文件 SHA-256 为
`87840b14a0b996b9112c103c237377e1d5dbfbb88ff3e79d8abca36f67fb9a6c`，与运行目录副本一致。

## 2. 如何处理

1. runner 从冻结 source 重新构造 root、frontier、risk、baseline 和 Explorer request；
2. 在读取显式 key 前持久化 provider intent，在发送前持久化 dispatch；
3. DeepSeek 只能返回 15 个当前 Candidate ID 的完整 permutation；
4. strict parser 验证 schema、request/queue identity、全集性、唯一性和预算；
5. 可信搜索器只采用通过验证的排列，并通过原 exact-prefix executor 物化和复核 Trace；
6. terminal artifact 绑定 provider audit、模型工作量、两侧搜索结果和机械 comparison；
7. 同目录 `-campaign-resume` 从 source 和 sidecar 重验结果，不再次调用 provider。

## 3. 真实结果

真实模型第一次响应即通过 strict validation：

| 项目 | 结果 |
|---|---:|
| Provider calls | 1 |
| Accepted / rejected proposals | 1 / 0 |
| Input / output / total tokens | 3008 / 301 / 3309 |
| Baseline / Explorer WorkItems | 16 / 16 |
| Baseline / Explorer work units | 1055 / 1055 |
| Baseline / Explorer reached candidates | 0 / 0 |
| 首个扩展是否不同 | true |

baseline 首个扩展为 `semantic-candidate-000001`，即投递 `n1 -> n3` 消息；DeepSeek 首个扩展为
`semantic-candidate-000004`，即 crash 当前协调者 `n1`。模型将三个 crash candidate 排在前三位，随后才是
message delivery 和 temporal action。这与当前风险的第一缺失里程碑
`coordinator-changed-while-inflight` 在动作类别层面一致。

两侧都因 `work-item-limit` 停止，均只扩展 2 个状态并物化 16 个 WorkItems；在冻结 depth/budget 内都没有
达到完整 RiskWitness。因此本结果证明的是 **真实模型通过受限接口改变了真实 etcd/raft 搜索前缀**，不是
风险到达率或缺陷检出的改善。

## 4. 可恢复性与内容寻址

- artifact 状态：`completed`；内部 digest：
  `daa2f85d3cb83eaa7e1659227f6d230c44dc36f121e7aa07fb90be38c987b5ba`；
- artifact 文件 SHA-256：
  `10dd2167d83a78cc283ec91f6ff31e0185dbeb799c1e6dd210d17899a65ffd1e`；
- baseline / Explorer search digest：
  `6c1a730275a9d4c3bcd97326f95c0b6bdf17be7fec8b00e9a6c62dddbf5a5cf9` /
  `1411aa1de9137cb9416e7d5db2ea4418ff5d8655f67e383ab99f224df4aa40d5`；
- provider audit 状态：`content-ready`；response digest：
  `09b0bdc53b78a2ed11c23db90c03d9b0de2951cd8fc78f7c116dee1027a94938`。

完成后使用相同 source、spec 和 artifact directory 执行一次 `-campaign-resume`。输出仍为 1 call、3309
tokens 和同一 artifact digest；文件 SHA-256 未变，证明恢复没有再次调用模型或重写结果。

完整本地产物保存在被 Git 忽略的
`artifacts/experiments/etcdraft-semantic-a2b3-real/`，其中包含 exact prompt/response sidecar；仓库文档只记录
非敏感摘要和 digest，不提交大型调用内容或凭证。

## 5. 结果边界与发现的设计问题

- 这是一次公开 calibration，样本数为 1，不是 holdout、方法排名或统计结论；
- baseline 和 Explorer 都没有到达完整 risk，不能声称 Agent 更有效；
- 没有产生 PSS/Coverage 增量、Oracle failure 或缺陷证据；
- 当前 `SemanticCandidateView` 向模型暴露 `action_kind`，但不暴露 node/role、message source/target 等
  受控语义。模型虽然选中了 crash `n1`，却无法从候选摘要直接区分三个 crash 目标；这个命中可能含有稳定
  Candidate 排序的偶然性，不能解释为模型识别了 leader；
- A2c 不应事后扩大权限或调权重。应先冻结唯一的 full-semantics 与 masked-semantics 投影，使 node role、
  message relation 等信息是否带来可重复收益成为可检验变量，同时保持 candidate set、执行预算、模型、root、
  risk 和 Oracle 不变。

## 6. 下一阶段：A2c

先做 masked-semantics 消融的预注册和 fixture 门禁，不立即扩大真实调用：

1. 明确定义 full projection 中允许公开的协议语义字段，以及 masked projection 中保留的通用 Action metadata；
2. 对稳定 ID、候选顺序和同类动作做 permutation-invariance 检查，排除 ID/排序侧信道；
3. 冻结相同 roots、candidate sets、budgets、model settings 和 evaluator；
4. 先用 deterministic fixture 验证两臂只在投影上不同，再请求真实调用授权；
5. 主要结果仍是 external risk/defect reachability 与 work-to-reach；首选动作差异只作为机制证据。
