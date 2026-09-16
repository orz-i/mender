import { Workbench, type WorkbenchAction } from '@mender/ui';
import type { Gateway } from '../application/gateway';
const actions: WorkbenchAction[] = [
  {
    "id": "billing.summary",
    "label": "读取账单汇总",
    "description": "数据来自不可变分录，而非前端估算。",
    "fields": [
      {
        "name": "currency",
        "label": "币种",
        "initial": "USD"
      }
    ],
    "mutation": false
  },
  {
    "id": "billing.reconciliation",
    "label": "读取用量账本对账",
    "description": "结果不明保持待对账，不因超时自动退款。",
    "fields": [
      {
        "name": "currency",
        "label": "币种",
        "initial": "USD"
      }
    ],
    "mutation": false
  },
  {
    "id": "approval.commerce",
    "label": "申请退款或调账审批",
    "description": "审批绑定业务键、依据、方向、金额和理由。",
    "fields": [
      {
        "name": "action",
        "label": "审批动作",
        "options": [
          "commerce.refund",
          "commerce.adjustment"
        ]
      },
      {
        "name": "business_key",
        "label": "幂等业务键（同一次重试保持一致）"
      },
      {
        "name": "basis_kind",
        "label": "业务依据类型",
        "options": [
          "usage_settlement",
          "run",
          "incident",
          "reconciliation"
        ]
      },
      {
        "name": "basis_id",
        "label": "业务依据 ID"
      },
      {
        "name": "direction",
        "label": "分录方向",
        "options": [
          "credit",
          "debit"
        ]
      },
      {
        "name": "amount_micro",
        "label": "金额（micro整数，不使用小数）"
      },
      {
        "name": "currency",
        "label": "币种（ISO大写，例如 USD）",
        "initial": "USD"
      },
      {
        "name": "reason",
        "label": "操作理由（必填，最多1000字）",
        "multiline": true,
        "maxLength": 1000
      },
      {
        "name": "ttl_seconds",
        "label": "审批有效秒数",
        "initial": "900"
      }
    ],
    "mutation": true
  },
  {
    "id": "billing.refund",
    "label": "追加已批准退款分录",
    "description": "只补充分录；这一步不是PSP实际退款。",
    "fields": [
      {
        "name": "business_key",
        "label": "幂等业务键（同一次重试保持一致）"
      },
      {
        "name": "basis_kind",
        "label": "业务依据类型",
        "initial": "usage_settlement",
        "readOnly": true
      },
      {
        "name": "basis_id",
        "label": "业务依据 ID"
      },
      {
        "name": "direction",
        "label": "分录方向",
        "initial": "credit",
        "readOnly": true
      },
      {
        "name": "amount_micro",
        "label": "金额（micro整数，不使用小数）"
      },
      {
        "name": "currency",
        "label": "币种（ISO大写，例如 USD）",
        "initial": "USD"
      },
      {
        "name": "reason",
        "label": "操作理由（必填，最多1000字）",
        "multiline": true,
        "maxLength": 1000
      },
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      }
    ],
    "mutation": true
  },
  {
    "id": "billing.adjustment",
    "label": "追加已批准调账分录",
    "description": "不可回写原分录；必须与独立审批精确一致。",
    "fields": [
      {
        "name": "business_key",
        "label": "幂等业务键（同一次重试保持一致）"
      },
      {
        "name": "basis_kind",
        "label": "业务依据类型",
        "options": [
          "run",
          "incident",
          "reconciliation"
        ]
      },
      {
        "name": "basis_id",
        "label": "业务依据 ID"
      },
      {
        "name": "direction",
        "label": "分录方向",
        "options": [
          "credit",
          "debit"
        ]
      },
      {
        "name": "amount_micro",
        "label": "金额（micro整数，不使用小数）"
      },
      {
        "name": "currency",
        "label": "币种（ISO大写，例如 USD）",
        "initial": "USD"
      },
      {
        "name": "reason",
        "label": "操作理由（必填，最多1000字）",
        "multiline": true,
        "maxLength": 1000
      },
      {
        "name": "approval_id",
        "label": "已存在的审批 ID"
      }
    ],
    "mutation": true
  },
  {
    "id": "payment.intents",
    "label": "读取sandbox支付意图",
    "description": "pending、settled和failed只来自服务端。",
    "fields": [
      {
        "name": "provider_id",
        "label": "支付 Provider ID"
      },
      {
        "name": "provider_account_id",
        "label": "支付账户 ID"
      }
    ],
    "mutation": false
  },
  {
    "id": "payment.callbacks",
    "label": "读取sandbox回调",
    "description": "安全字段投影，不含raw body、签名或transaction handle。",
    "fields": [
      {
        "name": "provider_id",
        "label": "支付 Provider ID"
      },
      {
        "name": "provider_account_id",
        "label": "支付账户 ID"
      }
    ],
    "mutation": false
  },
  {
    "id": "payment.reconciliation",
    "label": "读取sandbox支付对账",
    "description": "逐币种显示预期、确认、差额和隔离事件。",
    "fields": [
      {
        "name": "provider_id",
        "label": "支付 Provider ID"
      },
      {
        "name": "provider_account_id",
        "label": "支付账户 ID"
      },
      {
        "name": "currency",
        "label": "币种",
        "initial": "USD"
      }
    ],
    "mutation": false
  },
  {
    "id": "payment.create",
    "label": "创建sandbox支付意图",
    "description": "只绑定既有charge/refund journal，不会发起真实网络收款。",
    "fields": [
      {
        "name": "business_key",
        "label": "幂等业务键"
      },
      {
        "name": "provider_id",
        "label": "支付 Provider ID"
      },
      {
        "name": "provider_account_id",
        "label": "支付账户 ID"
      },
      {
        "name": "mode",
        "label": "支付模式",
        "initial": "sandbox",
        "readOnly": true
      },
      {
        "name": "purpose",
        "label": "意图用途",
        "options": [
          "collect_charge",
          "execute_refund"
        ]
      },
      {
        "name": "billing_journal_id",
        "label": "既有账本 Journal ID"
      },
      {
        "name": "amount_micro",
        "label": "金额（micro整数，不使用小数）"
      },
      {
        "name": "currency",
        "label": "币种（ISO大写，例如 USD）",
        "initial": "USD"
      }
    ],
    "mutation": true
  }
];
export function BillingWorkbenchPage({gateway}: {gateway: Gateway}) { return <Workbench title="账单与模拟支付" eyebrow="Mender / admin / S4" description="读取业务账本、审批补充分录，查看sandbox支付意图与对账差额。" boundary="不是生产收款。金额以micro整数精确传输，不在浏览器换汇。付款pending不等于到账；浏览器没有回调验签或直接入账能力。" workspaceScoped={true} actions={actions} session={gateway.session} execute={gateway.execute} />; }
