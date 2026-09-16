import { createHash } from 'node:crypto';
import { posix } from 'node:path';

// Presentation and consistency helpers only. This module never runs tests or approves work.
export const implementationLabels = {
  not_reviewed: '待复核', not_started: '未开始', partial: '部分实现',
  scoped_implemented: '限定范围已实现', implemented: '原任务范围已实现',
};
export const verificationLabels = {
  not_reviewed: '待复核', not_run: '未执行', recorded: '已有记录（非本次重跑）',
  passed: '有执行回执', failed: '未通过',
};
export const acceptanceLabels = { pending: '待验收', scoped_go: '范围化放行', accepted: '已验收' };
export const canonicalAliases = { 'S4-A': 'S4-01', 'S4-B': 'S4-04', 'S4-C': 'S4-02', 'S4-D': 'S4-05' };

const planKeys = ['id', 'phase', 'title', 'role', 'effort', 'start', 'end', 'deps', 'deliverable', 'acceptance', 'req', 'doc', 'priority'];
export function planFingerprint(tasks) {
  return createHash('sha256').update(JSON.stringify(tasks.map((task) =>
    Object.fromEntries(planKeys.map((key) => [key, task[key]]))))).digest('hex');
}

export function taskStatus(execution) {
  if (execution.acceptance === 'accepted') return '已完成';
  if (execution.implementation === 'not_reviewed') return '待复核';
  if (execution.implementation === 'not_started') return '未开始';
  return execution.implementation === 'partial' ? '进行中' : '待验收';
}

function cell(value) { return String(value).replaceAll('|', '／').replaceAll('\n', ' '); }
function link(path, text = path) {
  return `[${cell(text)}](${encodeURI(posix.relative('docs/planning', path))})`;
}

export function renderProjectStatus(data) {
  const tracking = data.execution_tracking;
  const lines = [
    '# Mender 当前实施与验收状态', '',
    '> 自动生成：`node scripts/render-project-status.mjs --write`。唯一可编辑状态源是 [project-data.json](project-data.json) 的任务 `execution` 字段；不要手改本页。', '',
    `复核日期：${tracking.reviewed_at}。本地复核提交：\`${tracking.reviewed_head}\`。`, '',
    '本页区分实施、自动化证据和原任务验收。已有证据文件不等于本次测试通过；切片 COMPLETE 不等于原任务完整验收。未复核的条目明确标记待复核，不推断为没有代码。', '',
    `当前按原计划处于 **${tracking.current_phase}**。Core Alpha / G3 内部集成已范围化放行；第三方客户端、外部用户、生产运维与真实支付仍不据此获得认证。`, '',
    '## 计划与进度口径', '',
    `保留 ${data.tasks.length} 个原任务、原估算与 S0–S5 六阶段。历史执行字段保存在各任务 baseline_execution，Word／PDF／Excel 是 v1.1 历史快照，不作为当前进度看板。`, '',
    '没有完整验收证据的 progress 为 null，不能把旧的 0% 当作未开发，也不能按完成切片数计算总体完成率。手工批准、商业许可和远端门禁不由此文件自动授予。', '',
    '## 历史别名与当前任务', '',
    '| 历史切片名 | 原任务 |', '| --- | --- |',
    ...Object.entries(tracking.canonical_aliases).map(([alias, id]) => `| ${alias} | ${id} |`), '',
    '旧证据文件按历史语义保留，不再创造新 S4 字母批次。后续工作从原 S4-01～S4-18 选择。', '',
    '## Gate 与外部条件', '',
    '| Gate | 当前记录 | 证据／限制 |', '| --- | --- | --- |',
    ...tracking.gates.map((gate) => `| ${gate.id} | ${cell(gate.state)} | ${cell(gate.note)}${gate.evidence ? `；${link(gate.evidence.path, '决议记录')}` : ''} |`), '',
    ...tracking.external_conditions.map((item) => `**${cell(item.id)} — ${cell(item.state)}：** ${cell(item.note)}`), '',
    '## 原 S4 全量范围', '',
    '| 原任务 | 实施 | 自动化证据 | 原任务验收 | 已有范围／剩余条件 |', '| --- | --- | --- | --- | --- |',
    ...data.tasks.filter((task) => task.phase === 'S4').map((task) => {
      const e = task.execution;
      return `| ${task.id} ${cell(task.title)} | ${implementationLabels[e.implementation]} | ${verificationLabels[e.verification]} | ${acceptanceLabels[e.acceptance]} | ${cell(e.scope)}；**剩余：**${cell(e.remaining.join('；'))} |`;
    }), '',
    '## 下一步仍使用原任务', '',
    ...tracking.next_task_ids.map((id) => {
      const task = data.tasks.find((item) => item.id === id);
      return `- **${id} ${cell(task.title)}**：${cell(task.execution.remaining.join('；'))}`;
    }), '',
    '当前状态以逐项范围和真实执行回执为准；本地工程与自动化验证不能代替用户、商业及生产签署。', '',
    '## 全任务实施状态与证据索引', '',
    '| 任务 | 当前状态 | 实施／验证／验收 | 证据 |', '| --- | --- | --- | --- |',
    ...data.tasks.map((task) => {
      const e = task.execution;
      return `| ${task.id} ${cell(task.title)} | ${task.status} | ${implementationLabels[e.implementation]}／${verificationLabels[e.verification]}／${acceptanceLabels[e.acceptance]} | ${e.evidence.map((ref, i) => link(ref.path, `证据${i + 1}`)).join('、') || '未复核；不推断未实现'} |`;
    }), '',
    '## 检查边界', '',
    '`pnpm check:status` 检查原任务编号、别名、三态关系、证据摘要、收口限制和本页生成漂移；它不会运行数据库／浏览器测试，也不能证明人工批准或 GitHub 分支保护已生效。业务测试仍由原 pnpm / Go / PostgreSQL 检查入口实际运行。', '',
  ];
  return lines.join('\n');
}
