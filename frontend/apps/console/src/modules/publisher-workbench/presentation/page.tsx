import { Workbench, type WorkbenchAction } from '@mender/ui';
import type { Gateway } from '../application/gateway';
const actions: WorkbenchAction[] = [
  {
    "id": "publisher.snapshot",
    "label": "读取发布者与插件",
    "description": "先读取当前工作区的发布者、插件和版本。",
    "fields": [],
    "mutation": false
  },
  {
    "id": "publisher.create",
    "label": "创建发布者",
    "description": "仅创建当前工作区归属的发布者。",
    "fields": [
      {
        "name": "publisher_id",
        "label": "发布者 ID"
      },
      {
        "name": "display_name",
        "label": "发布者名称"
      }
    ],
    "mutation": true
  },
  {
    "id": "publisher.rename",
    "label": "修改发布者名称",
    "description": "不会变更所有者或授权。",
    "fields": [
      {
        "name": "publisher_id",
        "label": "发布者 ID"
      },
      {
        "name": "display_name",
        "label": "发布者名称"
      }
    ],
    "mutation": true
  },
  {
    "id": "plugin.create",
    "label": "创建插件",
    "description": "为已存在的发布者创建插件身份。",
    "fields": [
      {
        "name": "publisher_id",
        "label": "发布者 ID"
      },
      {
        "name": "plugin_id",
        "label": "插件 ID"
      }
    ],
    "mutation": true
  },
  {
    "id": "version.create",
    "label": "创建版本草稿",
    "description": "不会发布或启用运行路由。",
    "fields": [
      {
        "name": "manifest",
        "label": "声明式 Manifest JSON",
        "multiline": true,
        "initial": "{\n  \"apiVersion\": \"mender.io/plugin/v1alpha1\",\n  \"plugin_id\": \"example.tool\",\n  \"version\": \"1.0.0\",\n  \"publisher_id\": \"publisher_example\",\n  \"display_name\": \"示例能力\",\n  \"description\": \"仅引用已审核能力；请替换为实际绑定。\",\n  \"capabilities\": [\n    {\n      \"kind\": \"api_tool\",\n      \"tool_version_id\": \"tool_version_example\"\n    }\n  ]\n}"
      }
    ],
    "mutation": true
  },
  {
    "id": "version.update",
    "label": "修改版本草稿",
    "description": "从服务器记录选择版本；非草稿修改由服务端拒绝。",
    "fields": [
      {
        "name": "manifest",
        "label": "声明式 Manifest JSON",
        "multiline": true,
        "initial": "{\n  \"apiVersion\": \"mender.io/plugin/v1alpha1\",\n  \"plugin_id\": \"example.tool\",\n  \"version\": \"1.0.0\",\n  \"publisher_id\": \"publisher_example\",\n  \"display_name\": \"示例能力\",\n  \"description\": \"仅引用已审核能力；请替换为实际绑定。\",\n  \"capabilities\": [\n    {\n      \"kind\": \"api_tool\",\n      \"tool_version_id\": \"tool_version_example\"\n    }\n  ]\n}"
      }
    ],
    "mutation": true
  },
  {
    "id": "version.preflight",
    "label": "预检版本",
    "description": "只验证引用能力和发布前置条件，不执行上游调用。",
    "fields": [
      {
        "name": "plugin_id",
        "label": "插件 ID"
      },
      {
        "name": "version",
        "label": "版本",
        "initial": "1.0.0"
      }
    ],
    "mutation": true
  },
  {
    "id": "version.submit",
    "label": "提交独立审核",
    "description": "绑定精确版本，不能自行批准。",
    "fields": [
      {
        "name": "plugin_id",
        "label": "插件 ID"
      },
      {
        "name": "version",
        "label": "版本",
        "initial": "1.0.0"
      }
    ],
    "mutation": true
  },
  {
    "id": "version.publish",
    "label": "发布已批准版本",
    "description": "重新预检并原子消费独立审批，过期或漂移会被拒绝。",
    "fields": [
      {
        "name": "plugin_id",
        "label": "插件 ID"
      },
      {
        "name": "version",
        "label": "版本",
        "initial": "1.0.0"
      }
    ],
    "mutation": true
  }
];
export function PublisherWorkbenchPage({gateway}: {gateway: Gateway}) { return <Workbench title="发布者工作台" eyebrow="Mender / console / S4" description="从声明式草稿到预检、提交审核和发布；使用已存在的工具版本与受审部署。" boundary="预检是服务端能力校验，不会试调用供应商。提交者不能自审；已发布版本不可覆盖。" workspaceScoped={true} actions={actions} session={gateway.session} execute={gateway.execute} />; }
