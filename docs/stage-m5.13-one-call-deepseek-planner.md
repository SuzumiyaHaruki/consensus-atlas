# M5.13 单调用 DeepSeek Planner smoke

日期：2026-08-08

## 结论

M5.13 已完成第一个真实受限 Planner transport。系统实际调用一次 `deepseek-v4-flash`，严格解析其
Proposal，将它交给 M5.12 编译器和唯一 v2 Execute/Replay 路径。调用没有重试。

结果是可信的 `execution-failed`，不是 `measurement-complete`：模型 proposal 通过机械编译，run 1
完整执行并严格 replay；run 2 在 decision 3 请求当时不可达的 exact message rule，系统停止并保存
全部已消费模型和执行成本。

这说明真实模型边界已经连通，也直接暴露了“一次性静态 Agent 无法预知瞬时 enabled set”的限制。

## 输入边界

模型只收到以下只读投影：

- Scope digest；
- 冻结 run numbers、每 run decision budget 和 mandatory replay；
- `n1/n2/n3` 公共节点；
- `complete-effect/deliver/drop/duplicate/fire-temporal/crash/restart` Action kinds；
- “three-node etcd/raft through protocol-neutral Actions”的公开目标描述；
- `planner-proposal/v1` 的精确 JSON 形状和约束。

模型没有收到 Runtime seed、PSS ID、experiment ID、Action ID、当前/未来 enabled set、trace、PSS
discovery、Coverage、Oracle、历史缺陷信息或密钥。Prompt 不允许模型提交 score、coverage claim、
Oracle 结论或协议消息内容。

## 传输与审计边界

- 单次 API call；没有 SDK/HTTP retry；
- official DeepSeek endpoint allowlist；
- temperature 0、thinking disabled、`max_tokens=1800`、5 分钟外层 deadline；
- 子进程只继承 PATH/locale 和三个显式 DeepSeek 配置变量；
- key 文件必须是权限不宽于 `0600` 的普通文件，Python 再以 no-follow/fstat 方式读取；
- stdout 1 MiB、stderr 64 KiB；
- wrapper 和 proposal 均 strict JSON decode，拒绝 unknown/trailing JSON；
- provider completion JSON 现在额外拒绝 duplicate keys；
- Go 侧计算 request/response digest，并保存 prompt digest、请求/响应模型、response ID、finish reason、
  provider usage、duration、thinking/temperature/max-token policy。

两个旧模型客户端已迁移到 82 行的共享 `internal/modelcommand` 边界，删除各自重复的进程/environment/
buffer 实现；共享层不解析模型响应、Proposal、协议或评分。

## 实际结果

工件：`benchmarks/experiments/etcdraft-v2-deepseek-planner-m5.13/attempt.json`。

模型生成：

```text
run 1: crash(n3)@5, restart(n3)@10, progress fallback
run 2: drop-message(node=n1)@3, duplicate-message(node=n2)@7, progress fallback
```

机械结果：

| 项目 | 数值 |
|---|---:|
| Proposal attempts / model calls | 1 / 1 |
| Prompt / completion / total tokens | 555 / 189 / 744 |
| API duration | 1,321 ms |
| Compilation | accepted |
| Run 1 primary / replay decisions | 32 / 32 |
| Run 2 primary decisions before failure | 2 |
| Primary setup / decisions / work units | 2 / 34 / 36 |
| Replay setup / decisions / work units | 1 / 32 / 33 |
| Final status | `execution-failed` |
| Failure | `primary-policy / run 2 / decision 3` |
| Reason | `EXPERIMENT_POLICY_RULE_NOT_ENABLED` |

run 2 的 exact rule 同时要求 `drop-message` 和 `node=n1`。该 decision 的真实 enabled set 没有完全
匹配项。可信层没有忽略 node、退回 priority、改变 decision、放宽 rule 或再次调用模型。

身份：

- proposal digest：`913a357d5ef9677ddaa3e36782f917d6afdf4a60ee6caf3a1477e49fe079217e`；
- scope digest：`bf186b1304c1ae0f86d1cb566dd03c680ca72619c91a30d5f8608c5610bb5c11`；
- attempt digest：`e90e464a3a1fc010cb80fb60eeafff90c77ba4f6d2c4ec27ccf6bf93b225e87f`；
- 文件 SHA-256：`7d6658693ebaf3e05b6e2a130cfd65f79350a2b649276a26c01b8c2fa2689e71`；
- 文件大小：3,727 bytes。

M5.12 stub 工件 fresh 运行后 SHA-256 仍为
`38bd333fb18af03ba958863cfbab83356f09689325c4361e4568ab8c3db60f52`。

## 代码体积

相对 M5.12，本阶段 Go 生产代码约净增 306 行，Python 生产代码净增 125 行，测试约净增 271 行。
新增量主要是 94 行 model audit、163 行目标 composition transport、116 行受限 Python prompt/client
和 82 行共享命令边界；同时从两个旧客户端及 CLI 删除约 86 行重复实现。

没有新增 Action、Adapter、Runtime、Replay、PSS、Coverage 或 Oracle 路径。真实 Planner 仍只调用
M5.12 的 `RunAuditedPlannerAttempt -> CompileProposal -> Execute`。

## 证明与未证明

已证明：

- 当前真实模型、API、受限进程、strict decode、usage audit、机械编译和部分执行账本可以端到端工作；
- 编译合法不等于运行期可达；失败不会被隐藏，也不会产生不完整 PSS summary；
- 单次模型响应不能修改可信 Scope 或运行/评测组件。
- checked-in 真实工件由默认 Go 测试重新反序列化、校验 digest/status/work 并绑定文件 SHA-256。

未证明：

- Agent 可以生成跑满预算的计划；
- Agent 比 fixed/random 更有效；
- 744 tokens 或本次失败具有统计代表性；
- PSS、Coverage 或缺陷检出有任何改进；
- provider usage 是密码学签名的账单证据；它是被 request/response digest 绑定的 provider-reported
  metadata；
- 一次性静态 proposal 能可靠引用瞬时 message/effect Action。
- API/子进程/JSON 在形成有效 audit + proposal 前失败时，当前 CLI 返回错误而不生成 PlannerAttempt；
  provider 也可能不给 usage。正式多调用实验前还需要独立 transport-attempt ledger，避免此类调用
  从总尝试成本中消失。

## 下一步

M5.14 先冻结最小机械 repair 输入和 transport-attempt ledger；repair 只公开本次的 phase/reason/run/
decision 和对应 proposed rule，不公开 enabled set、Action IDs、trace、PSS、Coverage 或 Oracle。
随后才允许恰好一次 repair call，并在总账中累计两次 proposal/model cost。目标只是验证“机械
finding 能否让 Agent 形成可执行计划”，仍不进行方法优势比较。
