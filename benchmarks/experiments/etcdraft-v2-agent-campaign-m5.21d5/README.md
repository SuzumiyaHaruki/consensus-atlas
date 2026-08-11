# etcd/raft v2 Agent Campaign M5.21d5

## 定位

这是 durable Agent Campaign 的首次真实外部连通性校准，不是 Agent 规划质量或方法优势实验。
输入在调用前固定为 official etcd/raft v2、1 attempt、8 decisions、seed 151、180000 ms
wall ceiling 和 1 call/8192 tokens allowance。transport 固定为 `deepseek-v4-flash`、temperature 0、
thinking disabled、1200 max output tokens、0 retry。

## 实际结果

| 项目 | 结果 |
|---|---:|
| Campaign status | `stopped` / `attempt-limit` |
| committed attempts | 1 |
| model work | 1 call / 2203 input / 214 output / 2417 total tokens |
| primary / replay | 8 decisions, 9 / 9 work units |
| PSS | 9 samples / 9 unique states |
| fault usage | 1 crash |
| workload | 1 planned / 0 offered / 1 pending |
| monitors | Agreement 1, TraceIntegrity 1, 0 triggers |

model result 状态为 `completed`，finish reason 为 `stop`。strict parser 接受 preference-only
proposal；proposal 保留 hard baseline，并将 `action-class-random` 放在 backend preference 首位。
trusted compiler 选择 `workload-action-class-random-b4`，使用冻结 seed 151 进入已有 qualified executor。

8-decision 轨迹没有选中 Invoke，所以 workload 仍为 pending。这是有界调度轨迹事实，不是执行错误；
同样，monitor 零触发不证明协议正确。

## 持久化与复核

- call intent 在 key read 前以 no-replace 文件落盘；
- 目录内只有 1 个 dispatch 和 1 个 result；
- 以 exact config 读取 stopped Campaign 时没有新调用，重建的 Summary/Observation 与保存文件
  字节一致；
- Summary digest：`71c7fb352bb44e12a5a1ae635d5b8a74723dd4eac93d707f00af6a4ef326f5d2`；
- Observation digest：`0cbe407c67f8641f38988c594f143d5f8b9f04519ee53af3e2214f387725c553`；
- 实验目录与 key 内容做了 exact-match 扫描，同时扫描 `Authorization`/`Bearer`/`key.txt`，
  均无命中。

## 不支持的结论

- Agent 比 zero-model、random、DFS 或专家计划更好；
- Agent 能稳定生成高质量或全面的测试；
- PSS/Coverage 完备或能预测缺陷检出；
- etcd/raft 或 ConsensusAtlas 正确、无故障或无误报。
