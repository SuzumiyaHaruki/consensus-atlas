# M5.13 one-call DeepSeek Planner smoke

`attempt.json` 是第一次真实受限 Planner transport 的公开开发工件。生成命令为：

```bash
make experiment-etcdraft-v2-deepseek-planner
```

本次只允许一次 `deepseek-v4-flash` API 调用：temperature 0、thinking disabled、最大 1,800 output
tokens、5 分钟总超时、无自动重试。模型只收到 Scope digest、run/decision/replay 投影、三个公共节点、
允许的协议无关 Action kinds、目标描述和 Proposal schema；没有收到 Runtime seed、PSS ID、Oracle、
Coverage、历史 trace 或密钥。

## 结果

- model calls：1；prompt/completion/total tokens：555 / 189 / 744；
- proposal 编译：accepted；
- run 1：32 primary decisions + 32 fresh replay decisions，完整结束；
- run 2：完成 2 个 primary decisions，在 decision 3 失败；
- 状态：`execution-failed`；
- 原因：`EXPERIMENT_POLICY_RULE_NOT_ENABLED`；
- 位置：`primary-policy / run 2 / decision 3`；
- primary work：2 setup + 34 decisions = 36 units；
- replay work：1 setup + 32 decisions = 33 units；
- proposal digest：`913a357d5ef9677ddaa3e36782f917d6afdf4a60ee6caf3a1477e49fe079217e`；
- attempt digest：`e90e464a3a1fc010cb80fb60eeafff90c77ba4f6d2c4ec27ccf6bf93b225e87f`；
- 文件 SHA-256：`7d6658693ebaf3e05b6e2a130cfd65f79350a2b649276a26c01b8c2fa2689e71`；
- 文件大小：3,727 bytes。

失败来自 run 2 decision 3 的 exact rule：模型请求 `drop-message` 且 `node=n1`，但该 decision 的真实
enabled set 没有完全匹配的 Action。系统没有放宽 rule、重新询问模型、修改 Runtime，也没有把部分
轨迹聚合成 `measurement-complete`。

## 解释边界

该工件证明真实模型调用、严格解析、审计、机械编译、部分执行计费和显式失败路径已经连通。它不证明
Agent 策略有效，更不证明 Agent 优于 fixed/random。由于运行没有完成，本工件没有 PSS discovery
summary，也不能和 M5.10/M5.11 的完整 64-decision 结果比较。

下一步只能基于公开机械 finding 设计一次受限 repair 机制；不能把真实 enabled set、trace、Oracle
或 Coverage 私有输出直接交给模型，也不能重写本次工件。
