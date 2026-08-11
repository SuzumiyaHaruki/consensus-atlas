# M5.18b4 Preference Ablation Request Freeze

日期：2026-08-09

## 阶段目标

在读取 key 或调用模型之前，把 no-feedback/with-feedback 两臂的可见输入、hard baseline、source
成本、共同 seed、transport 参数和精确请求字节全部冻结。这个阶段不调用模型、不执行两臂 follow-up，
也不修改 Runtime、Action、PSS、Oracle 或 Adapter。

## 冻结结构

```text
same AgentSemanticView + same hard baseline
                    |
        +-----------+-----------+
        |                       |
 agent_batch_feedback=null   trusted feedback v2
        |                       |
        +-----------+-----------+
                    |
     same prompt template / transport limits
                    |
     two exact prompt + request byte digests
                    |
      shared seed 4 and 98/98 execution ceiling
```

两臂的 system prompt 完全相同。结构化 user input 除 `agent_batch_feedback` 为 JSON `null` 或可信
feedback 对象外，由同一个 builder 生成。hard baseline 固定：

- view `d20b9fea...2e036`；
- risk `leader-change-with-inflight-proposal`；
- 96 decisions；
- crash 1/concurrent 1、drop 2、duplicate 1、partition 0；
- 空 preference，避免在输入中预选 backend。

模型回复未来必须分别通过 `ValidatePreferenceOnlyProposal(baseline, proposal)`，再做两臂 pairwise
校验。仅比较两份回复彼此相同是不够的，因为两边可能同时非法改变 hard 字段。

## 预算与 transport

`AgentFollowUpSpec/v1` 继续冻结 source seeds 1/2/3、unseen seed 4 和完整成本：source 6 attempts、
588/588；每臂 follow-up 最多 1 attempt、98/98；每臂连同 source ceiling 为 7 attempts、686/686。
模型 transport 固定为 `deepseek-v4-flash`、JSON object、thinking disabled、temperature 0、1200 output
tokens、每臂最多 1 call、0 retry。

`deepSeekPreparedRequest` 使 request 构造与 transport 分离。冻结阶段不需要 HTTP client 或 key；未来
调用只能把这里承诺的原始 request bytes 交给 `invokePrepared`，后者会在发送前重算 prompt/request
digest 并校验 request 内的 messages 与冻结 prompt bytes 一致。

## 实际结果

| arm | feedback | prompt bytes | request bytes | request digest |
|---|---:|---:|---:|---|
| no-feedback | no | 6,446 | 6,608 | `562d3bda...31ce5c` |
| with-feedback | yes | 8,800 | 8,962 | `eb44d738...f6826f` |

共同 identity：

- feedback：`94b0862b...c66bf`；
- follow-up spec：`bf632a68...af262`；
- hard baseline：`8261d484...79c05`；
- request freeze：`2591cf89...eed8c1`。

机械泄漏检查确认两份 request 都不含 b3 report/bundle、b4-pre report/outcome、`not-reached`、
`root_cause` 或 `oracle_status`。no-feedback request 不含 feedback digest；with-feedback request 包含。
模型调用数为 0。

小型承诺工件位于
[`benchmarks/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4/`](../benchmarks/experiments/etcdraft-v2-agent-b4-freeze-m5.18b4/README.md)。
精确 prompt/request body 可确定性再生，保存在 ignored `artifacts/`，避免在 Git 中重复约 31 KB JSON。

## 当前没有证明

- 没有产生任何模型回复、GuardedTestIntent proposal 或 Agent execution；
- 没有证明 feedback 会改变 preference，或改变后更好；
- 没有 RiskWitness、缺陷 verdict、Coverage 完整度或方法效果结论；
- b4 仍只允许评价 backend-preference micro-ablation，不是协议时序规划效果实验。

## 验证

- `make test`、`go vet ./...`、v1/retired-path audit、HashiCorp determinism、portable CFT matrix
  和 control-surface audit：通过；最终 `cmd/control-experiment` 普通测试耗时 90.785 秒；
- request-freeze 定向回归通过；生产 constructor 已完成 source-backed 校验，persist 前会再次做完整
  输入校验；两臂解码后的结构在移除 feedback 后必须逐字节等价；
- workload/trace-mutation 两项定向 race 通过，耗时 84.420 秒；
- `make test-race-full` **未通过**：两次均在 20 分钟 package ceiling 超时，分别运行到既有的 adjacent
  trace mutation 和 M5.17c2 method 回归；两次都没有报告 data race。按阶段 timebox 停止，不抬高
  timeout，也不把“未见 race 告警”记成完整门禁通过；
- 147 个非 `artifacts` JSON 可解析、23 个 schema JSON 语法通过、4 个 M5.18a build input/audit
  实例通过相应 schema、146 个 Markdown 本地链接有效；
- 两份总体规划逐字节一致，三份 checked freeze/spec/baseline 与 ignored 再生产物一致，
  `internal/controlexperiment` dependency closure 不含 etcd、Raft 或 HashiCorp 包；
- 模型调用数为 0，未读取 key。

## 下一步

下一步不是继续扩展接口，而是实现“从冻结 request bytes 到 response audit、strict parse、baseline
校验、plan v2、seed-4 instance、existing executor 和 IntentOutcome”的单一消费路径。只有用户明确要求
运行模型实验时，才读取 key 并依次执行两臂各一次、无重试；任一失败都原样计费并停止该臂。
