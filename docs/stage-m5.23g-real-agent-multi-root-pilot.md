# M5.23g：真实 Agent 多 root 有界实验与 M5.23 关闭

日期：2026-08-12

## 结论

M5.23g 完成了一次真实、显式 opt-in、失败不重试的 Search Agent pilot。实验使用 M5.23e 已冻结的
etcd/raft 三 root corpus 和同一组 depth/item/work ceilings；Agent 每次只看到 frozen
`ProtocolKnowledgePack`、当前 `ActionFrontierView` 与前面 root 已完成的 coarse discovery history。
唯一授权输出是当前 ActionID 集合的完整排列。

## 持久化与权限边界

每次 provider 调用先保存 exact prompt/request 的 `StatelessAgentCallIntent`，再保存 dispatch marker，最后
保存 completed/failed/proposal-rejected 三种 terminal result。dispatch 一经持久化便消费调用序号；本策略
预算 6 calls、0 retries，不提供恢复后重发接口。terminal result 绑定 provider response、token work 和通过
trusted validator 的 proposal digest。密钥不进入任何结构或工件。

三 root 每个最多展开 2 个 frontier、生成 6 个 WorkItem；Agent 不能选择 root、改变预算、构造 Action、
查看当前 child 未来结果、写 PSS/Oracle/verdict。18 个 WorkItem 均回到原 qualified executor，要求
strict Replay，然后由注册 SemanticMapper 从 bundle 只读重算 PSS。

## 实际结果

| 指标 | 结果 |
|---|---:|
| provider/model | DeepSeek / `deepseek-v4-flash` |
| 模型调用/接受 | 6 / 6 |
| retries | 0 |
| input/output/total tokens | 24,876 / 3,459 / 28,335 |
| roots / qualified attempts | 3 / 18 |
| Agent corpus-novel PSS | 15 |
| canonical / uniform 01/02/03 | 15 / 13 / 15 / 14 |
| Agent vs canonical Action 顺序 | 3/3 root 完全相同 |
| Agent vs canonical novel PSS set | 完全相同 |
| Agent marginal evidence work | 2,397 |

Agent 没有使用已授权的排列自由度，退化成 canonical。这不是执行失败：所有调用、校验、搜索、执行、
Replay 和重投影均完成；它是关于当前 prompt/knowledge 在这个小型 corpus 上缺少新增价值的真实负证据。

## 已证明与未证明

已证明：真实模型可被限制在 frontier permutation 权限内；逐调用证据在 transport 前后可审计；实际模型
成本与执行成本分开记录；Agent 与冻结 baseline 可在相同 root/ceiling 下比较；完整闭环可以产生并诚实保留
负结果。

未证明：Agent 优于 canonical、随机或专家方法；PSS 是完备覆盖度；当前 root corpus 有统计代表性；
Agent 能发现缺陷；跨协议有效性。单次 pilot 不用于显著性或普遍优势声明。

M5.23 至此关闭。下一主线 M5.24 是外部有效性：先冻结重复试验与非公开候选/对照协议，再扩大 corpus，
验证 Agent/约简/基线在缺陷检出、PSS/义务发现和完整成本上的关系；不得通过继续调 prompt 来解释本次负结果。
