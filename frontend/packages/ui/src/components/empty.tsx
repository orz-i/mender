import type { ComponentProps } from 'react';
import { cn } from '../lib/utils';

export function Empty({ className, ...props }: ComponentProps<'div'>) {
  return <div data-slot="empty" className={cn('flex min-h-52 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-border bg-muted/20 px-6 py-10 text-center', className)} {...props} />;
}
export function EmptyHeader({ className, ...props }: ComponentProps<'div'>) { return <div data-slot="empty-header" className={cn('flex max-w-md flex-col items-center gap-2', className)} {...props} />; }
export function EmptyTitle({ className, ...props }: ComponentProps<'h3'>) { return <h3 data-slot="empty-title" className={cn('text-base font-semibold', className)} {...props} />; }
export function EmptyDescription({ className, ...props }: ComponentProps<'p'>) { return <p data-slot="empty-description" className={cn('m-0 text-sm leading-6 text-muted-foreground', className)} {...props} />; }
export function EmptyContent({ className, ...props }: ComponentProps<'div'>) { return <div data-slot="empty-content" className={cn('flex items-center gap-2', className)} {...props} />; }
