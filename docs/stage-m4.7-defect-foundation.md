# M4.7 第一阶段总结：外部缺陷评价基础

> 日期：2026-08-06
> 阶段结论：协议无关评价基础完成；尚未形成 etcd/raft 缺陷效果结论

## 为什么改变下一阶段

此前 Coverage Obligation 和 PSS 同时承担搜索反馈与结果解释。若 Agent 直接优化这些
内部指标，再用分数上升证明 Agent 有效，会形成循环论证；原子义务还可能被多个
互不相关的简单测试分别刷满。继续增加复合义务或 Agent 角色无法解决这个评价问题，
只会提高系统复杂度。

本阶段因此先引入独立外部结果：隐藏历史缺陷/语义 mutant 的独立根因检出，以及正确
control 的误报。Coverage/PSS 保留为解释变量，不进入 kill 判定。

## 已完成

### 1. Campaign report v2 完整成本

`internal/scenario.RunWithCost` 即使在 prepare selector 失败时也返回已发生的成本。
`internal/campaign` 分别累计 primary 与 replay：

- fresh SUT/setup attempts；
- setup/prepare/stimuli steps；
- setup drain 实际执行的 Runtime events；
- Explorer measurement events；
- 确定性 work units。

Agent finding 和两个 Campaign CLI 同时显示 primary/replay work。历史 decision-only
报告不再能进入新 Defect Benchmark。

### 2. 私有 Defect Benchmark

`internal/defectbench` 实现：

- 私有 Manifest、canonical digest 与防枚举 blinding nonce；
- 只暴露 opaque trial ID 和共享预算的 Agent-facing Manifest；
- protocol/Profile/SUT Manifest identity 验证；
- run、decision、primary work 冻结预算；
- Campaign 成本、trace/decision 数量和 trace-integrity 复核；
- 从 setup + measurement trace 重新执行可信 Oracle；
- `killed/survived/control-pass/false-positive/invalid` 五种状态；
- 多 mutant 按独立 `root_cause_id` 聚合；
- root-cause kill rate、control false-positive rate 和完整成本。

`cmd/defect-eval` 可以在私有环境生成 blind Manifest，并根据 blind submission 产生
可信评价报告。schema 位于 `benchmarks/`。

### 3. 非循环正反例

fixture 实验通过真实 Campaign 执行 control 和 mutant：

| trial 类型 | Coverage | Agreement | 外部结果 |
|---|---:|---|---|
| 正确 control | 100 | PASS | control-pass |
| mutant A | 100 | FAIL | killed |
| mutant B（同根因） | 100 | FAIL | killed |

两个 mutant variant 最终只计一个 root cause。测试还清空了 Campaign 保存的 Oracle
结果，evaluator 仍能从轨迹重算 kill，证明存储字段和 Coverage 不能直接伪造结论。

## 已验证

```text
go test ./...                 PASS
python3 unittest (10 tests)  PASS
go vet ./...                 PASS
go test -race ./...           PASS
jq schema syntax check       PASS
git diff --check             PASS
```

## 本阶段没有证明

- 没有证明 DeepSeek Planner、Random 或 DFS 中任何一个更好；
- 没有接入真实 etcd/raft 历史缺陷；
- 没有证明当前 55 项义务或 Raft PSS 与缺陷检出正相关；
- 没有发现或预设必须发现 etcd/raft 新漏洞；
- 没有实现通用 LTL、义务笛卡尔积或更多 Agent；
- 还没有隔离的 hidden-SUT runner/build attestation；
- 在线 Agent Coordinator 仍按 run/decision/token 停止，primary work 由 benchmark
  统一检查，尚未做到 work-unit 精确在线截断。

## 下一阶段的单一目标

冻结第一批小规模 etcd/raft private pilot：

1. 选择少量可复现、根因独立的历史缺陷与正确 parent/fixed controls；
2. 只加入不要求 Agent 可见 patch/触发器的协议级语义 mutants；
3. 建立 development/calibration/holdout 分组和 opaque runner；
4. 在相同 primary work 下运行 Random、DFS、专家计划和当前单 Planner；
5. 比较 root-cause kill、false positive、first-kill work，并分析 Coverage/PSS 是否有
   解释力；
6. 只有实际漏检指向“缺少时间关系表达”时，才增加少量复合时序义务；只有漏检指向
   Planner 角色混合时，才接入 Scenario/Critic。

如果 Agent 在 holdout 缺陷上不优于简单基线，应如实形成负结果或缩小研究主张，
而不是继续增加自定义指标维持结论。
