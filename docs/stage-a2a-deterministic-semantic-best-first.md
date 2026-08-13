# A2a：Deterministic Semantic Best-First

日期：2026-08-13

分支：`feature/agentic-consensus-testing`

状态：协议无关确定性基线完成；未接真实模型

## 1. 本阶段解决的具体问题

A1 的 Agent 契约只能在已经完整物化的 bounded DFS corpus 中选择一个 head。它证明了权限隔离，却没有
改变搜索过程：无论 planner 如何排序，候选发现成本已经发生，也无法控制后续全局队列。

A2a 增加一个真正的第二搜索方法。它只在父 prefix 被选择时才生成 children，并为每个真实 child 计算
可信语义进度。这样 A2b 的 Explorer 可以影响“下一条扩展哪个 prefix”，而不只是从预生成结果中挑一条。

## 2. 算法边界

正式方法 identity 为：

```text
algorithm = bounded-semantic-best-first-v1
guidance  = risk-progress-lexicographic-v1
```

算法继续复用 `StatelessDFSSpec`、exact-prefix replay、child materialization/verification 和 `StatelessDFSWork`。
这避免为第二算法复制预算、执行或账本。`SemanticBestFirstResult` 是外层正式身份；其
`exact_prefix_search` 字段只是复用旧 tree/work schema，不表示实际执行了 DFS。

执行过程为：

```text
root Trace + RiskWitnessSpec
          |
          v
trusted root projection
          |
          v
expand root only -> verified children -> per-child projection
          |
          v
lazy global queue -> planner-safe SemanticQueueView
          |
          v
complete candidate permutation -> kernel validates -> choose head
          |
          v
reconstruct/expand selected prefix -> append new verified children
          |
          +-------------------------------> repeat within frozen bounds
```

队列中保存 exact Trace、WorkItem 和完整 RiskWitnessResult，但 guidance 收到的副本只有正向 allowlist 字段。
它不能看 ActionID、WorkItem digest、evidence、target identity、Oracle 或 verdict，也不能构造、删除或修改
candidate。接口接收 `context.Context`，使 A2b 的模型调用可以取消，但 A2a guidance 本身完全确定。

## 3. 冻结的字典序

`risk-progress-lexicographic-v1` 不含权重或可调参数，依次比较：

1. `target_unreached=true` 优先，避免继续深挖已达到同一目标的 prefix；
2. `len(satisfied_milestones)` 降序；
3. `semantic_bucket_visits` 升序；
4. `prefix_decisions` 升序；
5. `candidate_id` 升序，作为完全确定的最终 tie-break。

semantic bucket 由 RiskWitness status、已满足 milestone 集和缺失 milestone 集组成。一次 prefix 确实完成
扩展后才增加对应 bucket 的 visit；仅被查看、被拒绝或因重建预算不足而未展开不会增加。

这个 tuple 只是强确定性基线，不声称是最优启发式。A2b 必须读取同一 view、候选集合与预算，才能把差异
归因于 guidance，而不是换了数据生产路径。

## 4. 身份、权限与重放

- projector 是 target composition 的可信部分；每次返回都必须绑定 projector ID、Risk spec digest、
  Trace execution digest 和 target manifest digest；
- guidance 只能返回完整 candidate ID permutation；缺失、重复、伪造 ID 在继续扩展前被拒绝；
- 每个 candidate 绑定 exact WorkItem digest 和 source-bound RiskWitnessResult；
- expansion order 必须引用可扩展 candidate，其父 prefix 必须先扩展，所有已产生深层 child 必须有对应
  expanded parent；
- persisted result 的 source validation 使用相同 root、spec、Adapter factory、projector 和 guidance 重新运行
  整个确定性算法，并要求结构完全相等；
- order-only near miss 可以表达为 `target_unreached=true` 且没有虚构的 first-missing milestone。

## 5. 预算语义

Runtime work ceiling、item ceiling、depth 和 fault envelope 与 DFS 共用同一 `StatelessDFSSpec`。每次 prefix
重建、child materialization 和 fresh verification 都进入既有 `StatelessDFSWork`；超限前停止，已发生工作
保留。semantic projection 不伪装成 Runtime work：它是每个 candidate 一次、root 一次的确定性后处理，
次数可由结果机械重算。正式比较还需要同时报告墙钟、模型调用、token 和费用，不做无意义 padding。

## 6. fixture 证据

定向测试已证明：

- 算法只在首次 guidance 前物化 root children，没有预先生成深层 DFS corpus；
- 同一首次 queue view digest、candidate set、算法、root 和预算下，仅替换为另一合法 guidance 就选择不同
  root child，并产生不同 depth-2 Trace；
- 冻结 projector 也使 semantic baseline 首先扩展与 canonical DFS 不同的 root child；
- 两个候选 milestone 数相同但属于不同语义桶时，第二轮优先未访问桶；
- 缺失、重复和 invented candidate permutation 均在已发生 work 可审计的前提下 fail closed；
- 换成同 ID 但不同语义行为的 projector，完整 source validation 失败；
- 重封装 expansion order、跨 JSON round-trip 来源脱离、planner view 字段扩权均不能通过；
- milestone 全部出现但顺序错误时，view 保留 order-only near miss，不伪造 missing milestone。

## 7. 没有证明什么

- 没有真实 LLM、single Explorer 或多 Agent；
- 没有真实 etcd/raft/OmniPaxos projector 实验；
- 没有执行反馈后的第二次计划；
- 没有 Random/DFS/Agent 的正式效果排序；
- 没有 hidden candidate/control、缺陷检出或 false-positive 结论；
- 没有新增 PSS、Oracle、DPOR、operator catalog、预算 DSL 或第二套 Ledger。

## 8. 验证与代码增量

- `go test ./...`：通过；既有 `cmd/control-experiment` 包耗时 218.528 秒；
- `go vet ./...`：通过；
- A2a 定向普通测试：通过，0.271 秒；
- A2a 定向 `go test -race`：通过，4.096 秒；
- `git diff --check`：通过；
- 仓库与桌面的两份总体规划 SHA-256 一致；
- 按既有约定没有再次运行已知会超过时限的 full-race。

A2a 新增生产文件 500 行、测试文件 479 行，未新增 CLI、JSON 实验工件、通用 DSL 或第三方依赖。新增
生产代码集中在一个协议无关文件；如果 A2b 不能直接复用其 queue view 和 guidance seam，应先审查并删减，
而不是平行再建一套搜索路径。

## 9. 下一步与删除条件

A2b 只把 single Explorer 接到同一 `SemanticQueueView` 和完整 permutation 边界，并为每次模型调用保存
严格请求/响应与成本审计。首个公开闭环必须出现可复核的
`queue -> accepted/rejected proposal -> selected prefix -> Trace -> mechanical feedback -> next proposal` 因果链。

如果 Explorer 在固定重复中与 deterministic tuple 等效，应保留负结果并停止扩权限；如果 A2b 为了接入
模型必须绕过 candidate set、Runtime budget 或 trusted projector，则回退 A2b，而不是修改 A2a 可信边界。
