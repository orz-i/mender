// Explicit one-off reconciliation for the original S4 tasks. Never approves
// tasks or phases. --receipts only accepts actual recorder outputs with hashes.
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { spawnSync } from 'node:child_process';
import { planFingerprint, taskStatus, renderProjectStatus } from './lib/project-status.mjs';

const receipts = process.argv.slice(2).join(' ') === '--receipts';
if (process.argv.length > 2 && !receipts) throw new Error('Usage: review-s4-closeout-status.mjs [--receipts]');
const read = (path) => readFileSync(new URL('../' + path, import.meta.url));
const save = (path, value) => writeFileSync(new URL('../' + path, import.meta.url), typeof value === 'string' ? value : JSON.stringify(value, null, 2) + '\n');
const digest = (bytes) => createHash('sha256').update(bytes).digest('hex');
const ref = (path) => ({ path, sha256: digest(read(path)) });
const git = spawnSync('git', ['rev-parse', 'HEAD'], { encoding: 'utf8', shell: false });
if (git.status !== 0 || !/^[a-f0-9]{40}$/u.test(git.stdout.trim())) throw new Error('Cannot establish reviewed HEAD');
const head = git.stdout.trim();
const data = JSON.parse(read('docs/planning/project-data.json'));
const fingerprint = planFingerprint(data.tasks);
if (fingerprint !== data.execution_tracking.plan_fingerprint) throw new Error('Original task scope changed; explicit baseline review required');
const ui = 'docs/engineering/2026-09-16-s4-workbenches.md';
const tools = 'docs/engineering/2026-09-16-s4-distribution-quality.md';
const ops = 'docs/engineering/2026-09-16-s4-operations-runbook.md';
const summary = 'docs/engineering/2026-09-16-s4-closeout.md';
const mapping = {
  'S4-01': ['scoped_implemented', '受审manifest、发布状态机、精确独立审批和发布API保留；对应S4-09/10工作台已接入', ['原任务完整范围正式验收；生产供应商/插件准入条件不得由本地测试代签'], ui, 'postgres'],
  'S4-02': ['scoped_implemented', '危险操作与JIT以及refund/adjustment消费者保留，新增精确审批读取和工作台；撤销权限错误保持403', ['原范围正式验收；生产MFA和泛化危险动作不由本轮认证'], ui, 'postgres'],
  'S4-03': ['scoped_implemented', '不可变业务账本、追加分录、退款/调账及对账保留，S4-11界面与真实退款重放验证已接入', ['原范围验收；ProviderCostEvent及真实资金不在已认证范围'], ui, 'postgres'],
  'S4-04': ['scoped_implemented', 'cohort灰度、排空、回退与紧急停用已接入工作台并纳入真实数据库回归', ['原任务验收；百分比灰度与自动指标晋级非当前范围'], ui, 'postgres'],
  'S4-05': ['scoped_implemented', '工作区冻结、Provider隔离、事故与审计后端及运营UI已接入，权限/版本冲突通过真实浏览器验证', ['商业供应商准入与正式运营验收；外部告警责任人须配置'], ui, 'postgres'],
  'S4-06': ['scoped_implemented', '供应商中立sandbox意图、验签、回调与对账及状态UI保留，不提供真实充值', ['原sandbox范围签署；真实PSP/商户资格/地区/真钱仍不可放行'], ui, 'postgres'],
  'S4-07': ['scoped_implemented', '官方Go SDK CLI与Skill已实现；通过同MCP授权/预算/Run执行、幂等重放、查询、取消和精确参数合同', ['用户在已授权实际环境安装与接入验收；不声明未经验证的第三方客户端'], tools, 'postgres'],
  'S4-08': ['scoped_implemented', '只读MCP发现/合同快照、漂移拒绝、有界采样任务及无脚本质量报告已实现；不覆盖当前发布版本', ['负责人确认受控工具集探测范围；平台内多租户质量趋势持久化及付费上游功能任务不由此自动认证'], tools, 'postgres'],
  'S4-09': ['scoped_implemented', '发布者工作台及真实Chromium/HTTP/隔离PostgreSQL草稿、预检、独立审核、发布与负向流程', ['首发发布者用户验收；上游功能试调用需真实授权且受限计费'], ui, 'postgres'],
  'S4-10': ['scoped_implemented', '发布审核/回退、冻结/事故/审计和JIT工作台复用真实后端；CSRF、精确版本、不同审核主体及越权场景已验证', ['正式运营用户验收和生产身份/MFA保证；不得由浏览器确认替代服务端批准'], ui, 'postgres'],
  'S4-11': ['scoped_implemented', '账本、对账、部分退款/调账审批与sandbox支付状态界面已有真实浏览器验证，不将跳转当到账', ['财务用户验收；真实商户/支付网络及商业资格仍阻断'], ui, 'postgres'],
  'S4-12': ['scoped_implemented', '已有实际CLI/Skill、五个工作台、质量、制品签名、依赖扫描和恢复引导；文档无真实秘密', ['面向首发用户复核文档和所有失败指引，正式产品验收待签署'], summary, 'project'],
  'S4-13': ['scoped_implemented', '发布/回退/审批/Admin合同、真实数据库及Chromium工作台回归已执行；不只核对文件存在', ['原T22～24/T26/T31的正式范围复核与真实用户验收'], ui, 'postgres'],
  'S4-14': ['scoped_implemented', '不可变账本、sandbox回调、部分退款重放、浏览器账务与CLI同授权/同预算路径已执行', ['原T20/T21/T27/T29正式验收；实际PSP/商业环境不由测试替代'], summary, 'postgres'],
  'S4-15': ['scoped_implemented', '汇总原18任务、实际命令/日志/源码指纹及G4阻断清单；工程验证不伪造Gate批准', ['项目负责人审阅范围并作出G4正式决议；现仍pending'], summary, 'project'],
  'S4-16': ['scoped_implemented', '真实Ed25519签名/外部可信公钥验证、不可变制品摘要与路径限制、网络依赖扫描已实现；修复两项实际可达漏洞', ['生产密钥托管/信任锚与部署准入联调；不可达依赖告警风险评审；远端CI与分支保护实测'], tools, 'dependencies'],
  'S4-17': ['scoped_implemented', '已提供工作台回退/对账手册与自有临时容器pg_dump/pg_restore，核对Run/Job/预算/账本/支付/迁移事实，恢复后不启动Worker', ['真实值班人、外部告警、备份保留与角色/密钥/对象存储恢复配置；生产RPO/RTO演练不在本地结果内'], ops, 'postgres'],
  'S4-18': ['partial', '商业前置条件/供应商权利/地区/数据/支付及责任人清单已整理，未取得外部证明或签署', ['由业务负责人提交真实服务条款、供应商分发许可、支付/数据地区依据并签署；不得自动批准'], summary, null],
};
const dir = 'docs/engineering/verification/s4-closeout';
mkdirSync(new URL('../' + dir + '/', import.meta.url), { recursive: true });
const verified = {};
if (receipts) for (const mode of ['tools', 'project', 'postgres', 'dependencies']) {
  const path = `${dir}/${mode}.json`, record = JSON.parse(read(path));
  if (record.record_type !== 'suite-verification' || record.result !== 'passed' || record.exit_code !== 0 || record.source_unchanged !== true
    || !Array.isArray(record.files) || record.files.length === 0 || !Number.isFinite(Date.parse(record.finished_at))
    || digest(read(record.log.path)) !== record.log.sha256 || record.files.some((file) => digest(read(file.path)) !== file.sha256)) throw new Error(`${mode}: evidence or tested source drift`);
  verified[mode] = { path, record };
}
for (const task of data.tasks) {
  // This exact file was reviewed: a new isolated recovery subtest before the
  // existing deliberate migration checksum corruption; original assertions stay.
  for (const old of task.execution.evidence) if (old.path === 'backend/tests/integration/postgres_test.go') {
    if (old.sha256 !== '2819bb1f9758e2d3ee7cb6d956399d67f58e036dd93d15c2b331d144a5f574e9' && old.sha256 !== ref(old.path).sha256) throw new Error('Unexpected prior integration evidence version');
    old.sha256 = ref(old.path).sha256;
  }
  const item = mapping[task.id];
  if (!item) continue;
  const [implementation, scope, remaining, source, mode] = item;
  const evidence = task.execution.evidence.filter((e) => ![source, summary].includes(e.path) && !e.path.startsWith(dir + '/'));
  evidence.push(ref(source));
  if (source !== summary) evidence.push(ref(summary));
  const execution = { ...task.execution, implementation, verification: 'recorded', acceptance: 'pending', scope, remaining,
    evidence, verification_receipt: null, acceptance_record: null };
  if (receipts && mode) {
    const { record, path } = verified[mode];
    const receiptPath = `${dir}/${task.id}.json`;
    save(receiptPath, { record_type: 'task-verification', task_id: task.id, result: 'passed', exit_code: 0,
      command: record.command, environment: record.environment, head: record.head, source_sha256: record.source_sha256,
      finished_at: record.finished_at, log: record.log, suite_receipt: ref(path), scope,
      limitation: 'Scoped automated verification, not full task or human acceptance.' });
    execution.verification = 'passed'; execution.verification_receipt = ref(receiptPath);
    execution.evidence.push(ref(path));
  }
  task.execution = execution; task.status = taskStatus(execution); task.progress = null;
  task.evidence = execution.evidence.map((e) => e.path).join(';');
}
const tracking = data.execution_tracking;
tracking.reviewed_at = new Date().toISOString().slice(0, 10); tracking.reviewed_head = head;
tracking.review_basis = receipts ? '本轮实际工具、依赖、pnpm、隔离PostgreSQL/Chromium/恢复回执；保留旧记录时间范围，未取得人工/商业批准' : '实现文件与已执行本地验证已复核；最终完整回执待汇总，不将已有记录当本次全部通过';
tracking.next_task_ids = ['S4-15', 'S4-17', 'S4-18'];
const g4 = tracking.gates.find((g) => g.id === 'G4');
g4.state = 'pending'; g4.evidence = null;
g4.note = 'S4本地工程与自动化验证已汇总；原任务剩余范围、正式用户验收、值班/远端门禁和商业签署仍待负责人确认，不自动授予G4商业放行';
if (planFingerprint(data.tasks) !== fingerprint) throw new Error('Reconciliation modified original task scope');
save('docs/planning/project-data.json', data);
save('docs/planning/current-status.md', renderProjectStatus(data));
console.log(`Reviewed 18 original S4 tasks; 92-task scope unchanged; actual receipts=${receipts}; G4 approval remains pending.`);
