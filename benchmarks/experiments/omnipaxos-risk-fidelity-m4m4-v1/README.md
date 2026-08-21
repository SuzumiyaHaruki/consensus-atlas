# M4m4：OmniPaxos Risk fidelity 离线重投影

本阶段修复 M4m3 暴露的具体语义缺口：旧 Risk 将 Invoke 之后、下一次决定之前的任意
Drop 都投影为 `operation-stage=inflight`，因此 `prepare` Drop 也能满足自然语言所说的
“operation-carrying replication message loss”。

修复保持公共 Action、Runtime、Trace、Replay 和 Oracle 不变，只增强 OmniPaxos
Observation 与 Risk predicate：

```text
workload-invoked(request-id = R, role = coordinator)
→ message-dropped(request-id = R, role = operation-replication, stage = inflight)
→ decision-advanced(request-id = R)
```

`operation-replication` 只由带有 `entry_count > 0` 和 `request_id` 的
`sequence-paxos/accept-sync|accept-decide` 产生。Decision 的 RequestID 从导致决定的消息
及其可信 item dependency 链回溯到对应 operation-carrying message；找不到唯一请求时不
生成 RequestID，不以当前客户端请求作猜测性回填。

为保留 M4m3 的预注册身份，结构化结果继续记录运行当时的旧路径
`plans/agent/omnipaxos-message-loss-risk-v1.json`；内容逐字节一致的当前归档副本为
`benchmarks/experiments/omnipaxos-closure-pair-m4m3-v1/risk-input-v1.json`，当前活动严格输入为
`plans/agent/omnipaxos-message-loss-risk-v2.json`。M4m3 的原始结果也不覆盖。

## 回归与重投影

普通测试覆盖：

- Drop `prepare`：只满足 Invoke milestone；
- Drop operation-carrying `accept-sync` 且决定同一请求：三个 milestone 全部满足；
- Drop 请求 R、决定请求 S：停在 Drop milestone；
- Drop 正确但请求 pending：停在 Drop milestone；
- `entry_count=0`、缺失 RequestID 和依赖链请求冲突不产生 operation binding。

随后临时加载 M4m3 的四个 canonical Bundle，使用新 projector 和 v2 predicate 离线
重投影；临时测试在生成本报告后删除，日常测试不依赖历史 experiment 目录。

| Episode | M4m3 旧机械结果 | v2 假设实例化 | v2 satisfied milestones |
|---|---|---|---|
| g01-public-fixed | reached | not-reached | Invoke |
| g01-target-local | reached | reached | Invoke → operation Drop → same-request Decision |
| g02-target-local | reached | reached | Invoke → operation Drop → same-request Decision |
| g02-public-fixed | reached | not-reached | Invoke |

结构化结果见 `final-summary.json`。这是对已有公开轨迹的事后诊断，不是 M4m3
预注册结论的替换，也没有调用模型或运行真实问题版本。
