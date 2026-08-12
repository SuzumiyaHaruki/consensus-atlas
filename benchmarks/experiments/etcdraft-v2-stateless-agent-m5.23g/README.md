# etcd/raft v2 真实 Search Agent 多 root pilot（M5.23g）

本目录冻结 M5.23 的最后一次真实模型实验。它复用 M5.23e 在搜索前声明的 0/28/54-decision root corpus，
每个 root 固定 depth=2、items=6、work ceiling=1,500；模型只能排列当前 trusted frontier 的全部 ActionID。

六次 DeepSeek v4 Flash 调用均在联网前依次保存 `intent.json` 和 `dispatch.json`，随后只保存一次 terminal
`result.json`；`MaxRetries=0`。6/6 个提案通过完整 permutation 校验，三 root 共形成 18 个 WorkItem，
全部经原 qualified executor 执行并 strict Replay，再由注册的 Core PSS mapper 只读重投影。

实际结果：Agent 使用 28,335 tokens，得到 15 个 corpus-novel PSS states；状态集合及三 root Action 顺序
均与 canonical 完全相同。冻结 uniform seeds 的结果为 13/15/14。因此本实验是一份诚实的负结果：它证明
真实 Agent 已进入受限搜索闭环和完整成本账本，但不证明其优于 canonical/random、覆盖完备或发现缺陷。

阅读顺序：`summary.json` → `model-calls/*/{intent,dispatch,result}.json` →
`roots/*-agent-result.json` → `roots/*-discovery.json` → `agent-discovery.json`。
