import { Workbench, type WorkbenchAction } from '@mender/ui';
import type { Gateway } from '../application/gateway';
const actions: WorkbenchAction[] = [
  {
    "id": "plugin.reviews",
    "label": "读取插件审批",
    "description": "显示Requester、精确revision与到期时间。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "plugin.approve",
    "requiresRecord": true,
    "label": "批准插件发布",
    "description": "不得审核自己的请求；服务端重新验证权限和revision。",
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
    "id": "plugin.reject",
    "requiresRecord": true,
    "label": "拒绝插件发布",
    "description": "不得审核自己的请求；服务端重新验证权限和revision。",
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
    "id": "release.snapshot",
    "label": "读取发布计划与路由",
    "description": "只读当前计划、路由和审计。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "release.create",
    "label": "建立发布计划",
    "description": "必须引用已发布插件、Toolset、ToolVersion及同Provider受审部署。",
    "fields": [
      {
        "name": "plugin_id",
        "label": "插件 ID"
      },
      {
        "name": "plugin_version",
        "label": "版本",
        "initial": "1.0.0"
      },
      {
        "name": "toolset_version_id",
        "label": "Toolset版本 ID"
      },
      {
        "name": "tool_version_id",
        "label": "工具版本 ID"
      },
      {
        "name": "provider_id",
        "label": "Provider ID"
      },
      {
        "name": "stable_deployment_revision",
        "label": "稳定部署版本"
      },
      {
        "name": "candidate_deployment_revision",
        "label": "候选部署版本"
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
    "id": "release.canary",
    "label": "开始cohort灰度",
    "description": "观察窗口至少60秒；不是随机百分比分流。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
      },
      {
        "name": "observation_seconds",
        "label": "观察秒数",
        "initial": "300"
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
    "id": "release.promote",
    "label": "晋级稳定版本",
    "description": "影响新受理，现有Run与账本不重写。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
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
    "id": "release.drain",
    "label": "排空候选部署",
    "description": "影响新受理，现有Run与账本不重写。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
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
    "id": "release.rollback",
    "label": "回退到稳定部署",
    "description": "影响新受理，现有Run与账本不重写。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
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
    "id": "approval.emergency",
    "label": "申请应急停用审批",
    "description": "先向独立复核人申请；审批绑定当前发布revision。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
      },
      {
        "name": "ttl_seconds",
        "label": "审批有效秒数",
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
    "id": "release.emergency-disable",
    "label": "执行已批准停用",
    "description": "精确审批消费一次；不自动终止已提交的上游任务。",
    "fields": [
      {
        "name": "release_id",
        "label": "发布计划 ID"
      },
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
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
export function ReleaseWorkbenchPage({gateway}: {gateway: Gateway}) { return <Workbench title="插件审核与发布" eyebrow="Mender / admin / S4" description="审核插件版本，控制固定 cohort 的灰度、排空、回退和应急停用。" boundary="这里不做百分比随机分流。路由只影响新受理；历史Run保持固定版本。应急停用必须有独立的精确审批。" workspaceScoped={true} actions={actions} session={gateway.session} execute={gateway.execute} />; }
