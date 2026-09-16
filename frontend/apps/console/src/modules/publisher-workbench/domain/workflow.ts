export const commands = ["publisher.snapshot","publisher.create","publisher.rename","plugin.create","version.create","version.update","version.preflight","version.submit","version.publish"] as const;
export type Command = typeof commands[number];
export interface ResultRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string,string> }
export interface Result { records: ResultRecord[]; note: string }
