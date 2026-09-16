import { Workbench, type WorkbenchAction } from '@mender/ui';
import type { Gateway } from '../application/gateway';
const actions: WorkbenchAction[] = [
  {
    "id": "platform.workspaces",
    "label": "读取工作区",
    "description": "选择记录将精确版本填入操作表单。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "workspace.freeze",
    "label": "冻结工作区",
    "description": "每次使用期望版本；版本冲突需刷新，不能盲重试。",
    "fields": [
      {
        "name": "workspace_id",
        "label": "工作区 ID"
      },
      {
        "name": "expected_revision",
        "label": "期望版本（使用服务端记录的精确值）"
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
    "id": "workspace.unfreeze",
    "label": "解除工作区冻结",
    "description": "每次使用期望版本；版本冲突需刷新，不能盲重试。",
    "fields": [
      {
        "name": "workspace_id",
        "label": "工作区 ID"
      },
      {
        "name": "expected_revision",
        "label": "期望版本（使用服务端记录的精确值）"
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
    "id": "platform.providers",
    "label": "读取供应商状态",
    "description": "显示运行隔离状态，不代表商业入驻审批。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "provider.quarantine",
    "label": "隔离供应商",
    "description": "隔离阻止新任务，但保留已有任务的安全控制与对账。",
    "fields": [
      {
        "name": "provider_id",
        "label": "Provider ID"
      },
      {
        "name": "expected_revision",
        "label": "期望版本（使用服务端记录的精确值）"
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
    "id": "provider.restore",
    "label": "恢复供应商",
    "description": "隔离阻止新任务，但保留已有任务的安全控制与对账。",
    "fields": [
      {
        "name": "provider_id",
        "label": "Provider ID"
      },
      {
        "name": "expected_revision",
        "label": "期望版本（使用服务端记录的精确值）"
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
    "id": "platform.incidents",
    "label": "读取事故",
    "description": "当前最多50条，按后端更新顺序。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "incident.create",
    "label": "登记事故",
    "description": "记录目标和原因，不授予业务写权限。",
    "fields": [
      {
        "name": "target_kind",
        "label": "目标类型",
        "options": [
          "workspace",
          "provider"
        ]
      },
      {
        "name": "target_id",
        "label": "目标 ID"
      },
      {
        "name": "severity",
        "label": "严重性",
        "options": [
          "info",
          "warning",
          "critical"
        ]
      },
      {
        "name": "code",
        "label": "事故代码"
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
    "id": "incident.resolve",
    "label": "解决事故",
    "description": "仅追加处理事实，不删除审计历史。",
    "fields": [
      {
        "name": "incident_id",
        "label": "事故 ID"
      },
      {
        "name": "expected_revision",
        "label": "期望版本（使用服务端记录的精确值）"
      },
      {
        "name": "resolution",
        "label": "处理结论",
        "multiline": true,
        "maxLength": 1000
      }
    ],
    "mutation": true
  },
  {
    "id": "platform.audit",
    "label": "读取审计",
    "description": "只读最近的有界审计页，可导出本页JSON。",
    "fields": [],
    "mutation": false
  }
];
export function PlatformOperationsPage({gateway}: {gateway: Gateway}) { return <Workbench title="平台运营与异常" eyebrow="Mender / admin / S4" description="工作区冻结、供应商隔离、事故处理与受限审计。操作对象与理由都需明确。" boundary="平台身份不等于租户成员。冻结或隔离不会删除历史事实；仅服务端可裁决权限及状态。导出仅包括本页安全字段。" workspaceScoped={false} actions={actions} session={gateway.session} execute={gateway.execute} />; }
