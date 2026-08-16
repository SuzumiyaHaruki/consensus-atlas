# etcd/raft stateless root corpus

`root-corpus.json` 是当前 deterministic baseline 与 Semantic Explorer 共用的公开输入。它包含同一可信
source execution 的 0/28/54 decision prefix，分别表示初态、workload 已进入执行、发生故障的节点已重启。

文件在加载时由现有类型校验，并与 fresh source execution 对照；这里不再提交方法运行摘要，也不把历史
digest 当成测试质量证明。该 corpus 只是可复现实验起点，不是 Coverage、缺陷检出或 Agent 优势结论。
