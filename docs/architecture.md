# Architecture

## 信任边界

```text
Profile + TestSpec
        |
        v
Scenario Interpreter --> Deterministic Engine --> Protocol Adapter
                               |                    |
                               +---- raw trace <----+
                                         |
                         +---------------+---------------+
                         |               |               |
                    Canonicalizer      Oracle      Coverage Ledger

Agent Planner ---- proposes constraints only ----^
```

Go 核心必须在不启动 Python Agent 的情况下独立运行。相同 Profile、TestSpec 和 Adapter 版本必须产生相同执行指纹。

## 已实现的事件语义

每个协议 Adapter 接收一个外部事件，并返回：

- `Observations`：已发生的语义事实；
- `Effects`：新产生的消息、持久化、超时等可调度事件；
- `Status`：事件被应用或忽略。

Effect 可以通过本批次内的 `After` 引用建立依赖。例如：

```text
persist vote (key=persist-vote)
send response (after=persist-vote)
```

只有依赖事件成功应用，后续事件才会进入 enabled 集合。删除或忽略持久化事件会阻止依赖的发送事件。

## etcd/raft Adapter 的下一阶段接口映射

建议将 etcd/raft 的执行生命周期拆成显式阶段：

```text
Step message/tick/campaign
        |
        v
Collect Ready
        |
        +--> persist HardState/Entries/Snapshot
        |          |
        |          v
        +--> release dependent outbound messages
        |
        +--> apply committed entries
                   |
                   v
                Advance
```

必须保证：

- 同一 `RawNode` 最多存在一个 outstanding Ready；
- 发送依赖于相应持久化完成；
- crash 可以插在 write/fsync/send/apply/Advance 之间；
- restart 只读取 durable storage，而不是内存快照；
- 所有随机 election timeout 抽样均被记录和重放。

## 还没有实现的研究组件

1. PSS 编译器和协议关系抽象；
2. term/view 平移、日志/QC 形状和值相等关系规范化；
3. 因果图和保守独立关系；
4. DPOR、SAMC 语义约简和 ordered t-way 生成；
5. 安全属性之外的部分同步活性检查；
6. Twins 风格的有限 BFT 攻击算子；
7. etcd/raft、HotStuff/Tendermint 的实际 Adapter 和 Oracle。

这些组件应逐层加入，不能通过扩大 toy Profile 来代替。
