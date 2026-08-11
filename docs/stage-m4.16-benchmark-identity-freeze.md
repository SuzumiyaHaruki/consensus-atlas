# M4.16 阶段总结：评测身份冻结预检

> 日期：2026-08-06
> 阶段结论：新的正式 benchmark 将 Family/PSS identity 纳入冻结 Manifest；evaluator 不再靠独立参数选择 trusted monitor

## 已完成

- 新增 private benchmark Manifest v2 与 schema。它在原有 protocol、Profile digest、预算、
  variant/source/build identity 之外，强制包含冻结的 `pss_id`。
- Blind Manifest 保留同一 `pss_id`，因此公开 runner 看到的是 Profile 级的公开 family identity，
  而非 candidate、root cause、source transformation 或 kill monitor。
- `defect-eval` 从 v2 Manifest 读取 PSS identity 并注册 trusted monitor；Campaign report 的
  `pss_id` 不一致会机械标记为 `invalid`，不能得到 kill credit。
- 为不可改写的公开 v1 pilot 保留显式 `-legacy-pss-id` 复验入口。缺失 PSS identity 的归档
  Manifest 不会被当作新的正式 Manifest；新的 v2 输入也不能依赖该参数补全身份。

## 本阶段没有做的事

- 没有生成 private candidate/control 样本，没有调用模型，也没有运行方法比较。
- 没有把公开 calibration、ReadIndex 或 Ready.MustSync pilot 改标为 holdout。
- `pss_id` 固定 monitor 选择，但不证明候选之间的根因独立性；这仍由 curator 冻结样本时负责。

## 下一项最小工作

建立私有 benchmark v2：至少三个互不重复的历史 root cause、对应正确 controls、已审计的
build/profile/monitor identity 和共同 primary-work 上限。完成 exposure audit 后，先用相同
scope 产生 Random、DFS、专家与无反馈 Planner 的提交，再运行 Blind Planner。结果只以
root-cause kill 与 control false-positive 为主结论，Coverage/PSS 仅作解释曲线。
