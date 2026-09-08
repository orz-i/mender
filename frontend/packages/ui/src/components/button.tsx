// Adapted from shadcn/ui new-york Button (MIT), 2026-09-08.
// Source: https://ui.shadcn.com/r/styles/new-york/button.json
import type { ComponentProps } from 'react';
import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import { cn } from '../lib/utils';

const variants = cva(
  'inline-flex min-h-11 items-center justify-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-ring disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        outline: 'border border-border bg-background text-foreground hover:bg-muted',
      },
    },
    defaultVariants: { variant: 'default' },
  },
);

export function Button({ className, variant, asChild = false, ...props }:
  ComponentProps<'button'> & VariantProps<typeof variants> & { asChild?: boolean }) {
  const Component = asChild ? Slot : 'button';
  return <Component className={cn(variants({ variant }), className)} {...props} />;
}
