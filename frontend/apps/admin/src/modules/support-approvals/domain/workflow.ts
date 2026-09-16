export const commands = ["approval.get","approval.list","approval.approve","approval.reject","support.request","support.approve","support.reject","support.activate","support.runs","support.revoke"] as const;
export type Command = typeof commands[number];
export interface ResultRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string,string> }
export interface Result { records: ResultRecord[]; note: string }
