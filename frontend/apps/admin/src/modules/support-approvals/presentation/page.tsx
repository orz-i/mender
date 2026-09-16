import { Workbench, type WorkbenchAction } from '@mender/ui';
import type { Gateway } from '../application/gateway';
const actions: WorkbenchAction[] = [
  { "id": "approval.get", "label": "读取精确审批", "description": "先读取并核对具体审批绑定；平台财务和支持人员不必加入租户。", "mutation": false, "fields": [{ "name": "approval_id", "label": "审批 ID" }] },
  {
    "id": "approval.list",
    "label": "读取危险审批",
    "description": "Workspace授权接口；平台支持审批通过下方专用接口按ID复核。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "approval.approve",
    "requiresRecord": true,
    "label": "批准危险操作",
    "description": "核对action、对象版本、摘要、金额与到期时间；提交者不能自审。",
    "fields": [
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      },
      {
        "name": "note",
        "label": "独立复核意见",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "approval.reject",
    "requiresRecord": true,
    "label": "拒绝危险操作",
    "description": "核对action、对象版本、摘要、金额与到期时间；提交者不能自审。",
    "fields": [
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      },
      {
        "name": "note",
        "label": "独立复核意见",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "support.request",
    "label": "申请临时只读支持",
    "description": "固定run:read首发只读范围，有效期5～60分钟。",
    "fields": [
      {
        "name": "scopes",
        "label": "只读范围",
        "initial": "run:read",
        "readOnly": true
      },
      {
        "name": "ttl_seconds",
        "label": "授权有效秒数",
        "initial": "900"
      },
      {
        "name": "reason",
        "label": "操作理由（必填，最多1000字）",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "support.approve",
    "requiresRecord": true,
    "label": "独立批准JIT申请",
    "description": "使用平台支持专用授权，不要求成为租户成员。",
    "fields": [
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      },
      {
        "name": "note",
        "label": "独立复核意见",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "support.reject",
    "requiresRecord": true,
    "label": "拒绝JIT申请",
    "description": "使用平台支持专用授权，不要求成为租户成员。",
    "fields": [
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      },
      {
        "name": "note",
        "label": "独立复核意见",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "support.activate",
    "label": "激活已批准JIT",
    "description": "必须由原申请人激活；批准本身不等于已有授权。",
    "fields": [
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      }
    ],
    "mutation": true
  },
  {
    "id": "support.runs",
    "label": "读取JIT允许的Run",
    "description": "仅返回安全运行元数据，过期或撤销后读取失败。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "support.revoke",
    "label": "撤销JIT授权",
    "description": "由独立复核人或平台运营人员撤销grant；支持申请人本身没有复核权限。",
    "fields": [
      {
        "name": "grant_id",
        "label": "JIT Grant ID"
      },
      {
        "name": "reason",
        "label": "操作理由（必填，最多1000字）",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  }
];
export function SupportApprovalsPage({gateway}: {gateway: Gateway}) { return <Workbench title="危险审批与临时支持" eyebrow="Mender / admin / S4" description="独立复核危险动作，申请并激活有时限的只读支持访问。" boundary="申请、独立审核、激活分别执行。JIT不会创建租户成员，也不允许读取秘密或修改租户业务。过期与撤销由数据库强制生效。" workspaceScoped={true} actions={actions} session={gateway.session} execute={gateway.execute} />; }
