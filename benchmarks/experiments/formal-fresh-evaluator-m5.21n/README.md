# M5.21n Formal fresh evaluator

`summary.json` 由既有 M5.18a in-process correct bundle 的公开 synthetic 复用机械生成。六个 opaque trial
都引用同一份正确 bundle，所以三个 control 均通过、三个 synthetic candidate 均 survived；该结果只验证
multi-pair evidence-map、exposure gate、MethodSpec/build/bundle/projector/monitor/budget 校验和 private ledger。

测试会把一份内部有效的归档 BuildAudit 对象重绑定到该 in-process fixture identity；这不是实际 binary
provenance，也不能进入正式 holdout 分母。CLI 多 pair 输入、真实 private candidate/control 和第二 strict
CFT 尚未完成，`formal_ready=false`。本阶段没有新增模型调用或独立 SUT execution。
