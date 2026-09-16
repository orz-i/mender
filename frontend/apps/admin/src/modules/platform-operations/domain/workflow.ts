export const commands = ["platform.workspaces","workspace.freeze","workspace.unfreeze","platform.providers","provider.quarantine","provider.restore","platform.incidents","incident.create","incident.resolve","platform.audit"] as const;
export type Command = typeof commands[number];
export interface ResultRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string,string> }
export interface Result { records: ResultRecord[]; note: string }
