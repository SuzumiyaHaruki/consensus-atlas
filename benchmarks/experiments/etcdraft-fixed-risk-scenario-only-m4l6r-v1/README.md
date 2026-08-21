# M4l6R fixed-Risk Scenario-only pilot

本实验在提交 `2cf7999` 上运行。两个 arm 使用同一个公开、重新资格审查的
`append-response-loss-with-alternate-quorum` Risk，Risk Agent 不参与；只改变
`closure_mode=public-fixed|target-local`。

## 结果

两个 arm 都满足：

- `risk_calls=0`；
- fixed Risk digest 为
  `91646150ca0bbddc7613bcd6f94917c641e31911e6704ccb91124e9272506da8`；
- Risk qualified；
- fresh Replay stable；
- Oracle finding 为 0。

但两个 arm 都没有到达 Risk：

| arm | Scenario calls | tokens | Scenario decisions | qualified trace decisions | 结果 |
|---|---:|---:|---:|---:|---|
| public-fixed | 3 | 82,848 | 5 | 33 | call budget exhausted |
| target-local | 3 | 92,913 | 7 | 35 | call budget exhausted |

target-local arm 在第 2 次调用真实执行了目标 `MsgAppResp n2→n1` drop，但同一
计划随后要求投递尚未 enabled 的 `MsgAppResp n3→n1`，以 `no-match` 停止。第 3 次
调用完成一个 n3 effect 后又过早要求同一投递，再次 `no-match`。当前 closure 只在
计划完整结束后的自然推进阶段接管，因此没有被激活。

## 结论边界

本实验成功消除了上一轮“两个 arm 使用不同 Risk”的混杂变量，也证明 fixed Risk
Scenario-only 模式与成本账本工作正常。但它没有证明 target-local closure 优于
public fixed progress，也没有发现协议问题。

新的具体缺口是：Scenario Agent 不知道应在完成干预后把闭合交给 Target backend，
因而继续猜测后续消息时序。下一轮应先让 Scenario view/prompt 明确表达可用的
post-intervention closure，或提供显式、零 Action 的“交给 closure”语义；不能通过
增加调用预算把该接口问题掩盖掉。

大 Bundle 和 provider journal 保留在本机忽略目录 `artifacts/agentic/`，不进入 HEAD。
