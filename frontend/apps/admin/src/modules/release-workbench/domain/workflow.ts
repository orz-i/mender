export const commands = ["plugin.reviews","plugin.approve","plugin.reject","release.snapshot","release.create","release.canary","release.promote","release.drain","release.rollback","approval.emergency","release.emergency-disable"] as const;
export type Command = typeof commands[number];
export interface ResultRecord { key: string; title: string; state: string; facts: { label: string; value: string }[]; inputs: Record<string,string> }
export interface Result { records: ResultRecord[]; note: string }
