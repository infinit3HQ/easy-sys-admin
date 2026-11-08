import { docsHref, plans } from '@esa/plans'
import type { Plan, PlanCta } from '@esa/plans'
import type { ReactNode } from 'react'
import { Button } from '#/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '#/components/ui/dialog'
import { cn } from '#/lib/utils'

function ctaHref(cta: PlanCta): string | undefined {
  if (cta.kind === 'docs') return docsHref(cta.path)
  if (cta.kind === 'mail') return cta.href
  return undefined
}

function PlanCtaButton({ plan }: { plan: Plan }) {
  if (plan.cta.kind === 'current') {
    return (
      <Button type="button" disabled aria-current="true" className="w-full">
        {plan.cta.label}
      </Button>
    )
  }
  const href = ctaHref(plan.cta)
  return (
    <Button
      asChild
      variant={plan.state === 'current' ? 'default' : 'outline'}
      className="w-full"
    >
      <a href={href}>{plan.cta.label}</a>
    </Button>
  )
}

/** The three plans. Used on the pricing page, the landing section, and in the dialog. */
export function PricingCards({ idPrefix = 'plan' }: { idPrefix?: string }) {
  return (
    <div className="grid items-stretch gap-4 lg:grid-cols-3">
      {plans.map((plan) => {
        const current = plan.state === 'current'
        const titleId = `${idPrefix}-${plan.id}-title`
        return (
          <article
            key={plan.id}
            aria-labelledby={titleId}
            className={cn(
              'flex min-w-0 flex-col rounded-md border bg-bg p-5',
              current
                ? 'border-amber shadow-[0_0_0_1px_rgb(255_176_0/0.35)]'
                : 'border-line',
            )}
          >
            <div className="flex items-start justify-between gap-3">
              <h3
                id={titleId}
                className="font-display text-2xl leading-none font-bold tracking-wide uppercase"
              >
                {plan.name}
              </h3>
              <span
                className={cn(
                  'shrink-0 rounded-sm border px-1.5 py-1 font-mono text-[10px] tracking-widest uppercase',
                  current
                    ? 'border-amber/70 text-amber'
                    : 'border-line-2 text-faint',
                )}
              >
                {plan.stateLabel}
              </span>
            </div>
            <p className="mt-5 font-display text-5xl leading-none font-bold tracking-tight">
              {plan.price}
            </p>
            <p className="mt-2 font-mono text-[11px] tracking-wide text-muted-foreground uppercase">
              {plan.priceDetail}
            </p>
            <p className="mt-4 text-sm leading-relaxed text-ink-2">
              {plan.summary}
            </p>
            <ul className="mt-4 flex-1 space-y-2.5">
              {plan.features.map((feature) => (
                <li
                  key={feature}
                  className="flex gap-3 text-sm leading-snug text-ink"
                >
                  <span
                    className="mt-1.5 size-1.5 shrink-0 bg-amber"
                    aria-hidden
                  />
                  {feature}
                </li>
              ))}
            </ul>
            <div className="mt-6">
              <PlanCtaButton plan={plan} />
            </div>
          </article>
        )
      })}
    </div>
  )
}

const intro =
  'Three ways to run Easy Sys Admin. The public beta is free and is the plan in force today. Self-Hosted and Managed Cloud are not available yet, and nothing on this page takes a payment.'

export function PricingSection({ id = 'pricing' }: { id?: string }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-14">
      <h2
        id={`${id}-title`}
        className="font-display text-5xl leading-[0.9] font-bold uppercase sm:text-6xl"
      >
        Plans
      </h2>
      <p className="mt-6 max-w-2xl text-base leading-relaxed text-ink-2">
        {intro}
      </p>
      <div className="mt-10">
        <PricingCards idPrefix={id} />
      </div>
    </section>
  )
}

/**
 * The plans dialog. Radix traps focus, restores it on close, and closes on Escape.
 * Pass `trigger` for a button, or control `open` from a menu.
 */
export function PricingDialog({
  trigger,
  open,
  onOpenChange,
}: {
  trigger?: ReactNode
  open?: boolean
  onOpenChange?: (open: boolean) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {trigger ? <DialogTrigger asChild>{trigger}</DialogTrigger> : null}
      <DialogContent
        className="flex max-h-[min(92dvh,920px)] w-full max-w-[calc(100%-1rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-5xl"
        aria-describedby="pricing-dialog-description"
      >
        <DialogHeader className="border-b px-5 py-4">
          <DialogTitle>Plans</DialogTitle>
          <DialogDescription id="pricing-dialog-description">
            {intro}
          </DialogDescription>
        </DialogHeader>
        <div className="overflow-y-auto px-4 py-4 sm:px-5 sm:py-5">
          <PricingCards idPrefix="dialog-plan" />
        </div>
      </DialogContent>
    </Dialog>
  )
}
