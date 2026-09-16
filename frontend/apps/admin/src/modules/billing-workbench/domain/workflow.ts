export const commands = ["billing.summary","billing.reconciliation","approval.commerce","billing.refund","billing.adjustment","payment.intents","payment.callbacks","payment.reconciliation","payment.create"] as const;
export type Command = typeof commands[number];
export interface ResultRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string,string> }
export interface Result { records: ResultRecord[]; note: string }
