import { useEffect, useRef, useState } from 'react'
import type { ButtonHTMLAttributes, ReactNode } from 'react'
import type { ServerStatus } from '@esa/api'

export function cx(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(' ')
}

// ---- Status --------------------------------------------------------------------

const statusStyle: Record<ServerStatus, { dot: string; text: string; label: string }> = {
  ok: { dot: 'bg-ok shadow-[0_0_10px_var(--color-ok)]', text: 'text-ok', label: 'OK' },
  warning: { dot: 'bg-warn shadow-[0_0_10px_var(--color-warn)]', text: 'text-warn', label: 'Warning' },
  failing: { dot: 'bg-fail shadow-[0_0_12px_var(--color-fail)] animate-blink', text: 'text-fail', label: 'Failing' },
  unreachable: { dot: 'bg-down', text: 'text-down', label: 'Unreachable' },
  pending: { dot: 'bg-pending', text: 'text-pending', label: 'Pending' },
}

export function StatusDot({ status, className }: { status: ServerStatus; className?: string }) {
  return <span aria-hidden className={cx('inline-block size-2 shrink-0 rounded-full', statusStyle[status].dot, className)} />
}

export function StatusBadge({ status }: { status: ServerStatus }) {
  const s = statusStyle[status]
  return (
    <span className={cx('inline-flex items-center gap-2 font-mono text-[11px] uppercase tracking-widest', s.text)}>
      <StatusDot status={status} />
      {s.label}
    </span>
  )
}

const runStyle: Record<string, string> = {
  running: 'text-amber',
  queued: 'text-muted',
  succeeded: 'text-ok',
  failed: 'text-fail',
  partial: 'text-warn',
  skipped: 'text-muted',
  missed: 'text-down',
}

export function RunStatus({ status }: { status: string }) {
  return (
    <span className={cx('inline-flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-widest', runStyle[status] ?? 'text-muted')}>
      {status === 'running' && <span className="size-1.5 animate-blink rounded-full bg-amber" />}
      {status}
    </span>
  )
}

export function Severity({ severity }: { severity: string }) {
  const c = severity === 'critical' ? 'bg-fail text-bg' : severity === 'warning' ? 'bg-warn text-bg' : 'bg-line-2 text-ink-2'
  return <span className={cx('rounded-sm px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-wider', c)}>{severity}</span>
}

// ---- Meters ---------------------------------------------------------------------

/** A horizontal gauge with tick marks; turns amber and red near the thresholds. */
export function Meter({ value, warn = 85, fail = 95, className }: { value: number | undefined; warn?: number; fail?: number; className?: string }) {
  const v = Math.max(0, Math.min(100, value ?? 0))
  const color = v >= fail ? 'bg-fail' : v >= warn ? 'bg-warn' : 'bg-ink-2'
  return (
    <div className={cx('relative h-1.5 w-full overflow-hidden rounded-[1px] bg-line', className)}>
      <div className={cx('h-full transition-[width] duration-700', color)} style={{ width: `${v}%` }} />
      <div className="pointer-events-none absolute inset-0 bg-[repeating-linear-gradient(90deg,transparent_0_9.5%,var(--color-bg)_9.5%_10%)]" />
    </div>
  )
}

export function Stat({ label, value, sub, tone }: { label: string; value: ReactNode; sub?: ReactNode; tone?: 'ok' | 'warn' | 'fail' | 'down' }) {
  const t = tone === 'ok' ? 'text-ok' : tone === 'warn' ? 'text-warn' : tone === 'fail' ? 'text-fail' : tone === 'down' ? 'text-down' : 'text-ink'
  return (
    <div>
      <div className="label">{label}</div>
      <div className={cx('mt-1 font-display text-4xl font-bold leading-none tracking-tight', t)}>{value}</div>
      {sub && <div className="mt-1 font-mono text-[11px] text-muted">{sub}</div>}
    </div>
  )
}

// ---- Controls --------------------------------------------------------------------

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: 'primary' | 'ghost' | 'danger' | 'outline'
  size?: 'sm' | 'md'
  busy?: boolean
}

export function Button({ variant = 'outline', size = 'md', busy, className, children, disabled, ...rest }: ButtonProps) {
  const v = {
    primary: 'bg-amber text-bg hover:bg-amber-2 border-amber font-semibold',
    outline: 'border-line-2 text-ink hover:border-ink-2 hover:bg-panel-2',
    ghost: 'border-transparent text-ink-2 hover:text-ink hover:bg-panel-2',
    danger: 'border-fail/60 text-fail hover:bg-fail hover:text-bg',
  }[variant]
  const s = size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3.5 py-2 text-sm'
  return (
    <button
      {...rest}
      disabled={disabled || busy}
      className={cx(
        'relative inline-flex items-center justify-center gap-2 overflow-hidden rounded border transition-colors disabled:cursor-not-allowed disabled:opacity-50',
        v,
        s,
        className,
      )}
    >
      {busy && <span className="absolute inset-0 animate-sweep bg-gradient-to-r from-transparent via-white/20 to-transparent" />}
      {children}
    </button>
  )
}

export function Field({ label, hint, error, children }: { label: string; hint?: ReactNode; error?: string; children: ReactNode }) {
  return (
    <label className="block">
      <span className="label">{label}</span>
      <div className="mt-1.5">{children}</div>
      {error ? <p className="mt-1 text-xs text-fail">{error}</p> : hint ? <p className="mt-1 text-xs text-muted">{hint}</p> : null}
    </label>
  )
}

export function Toggle({ checked, onChange, label, hint }: { checked: boolean; onChange: (v: boolean) => void; label: string; hint?: string }) {
  return (
    <label className="flex cursor-pointer items-start gap-3 py-1.5">
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        onClick={() => onChange(!checked)}
        className={cx('relative mt-0.5 h-5 w-9 shrink-0 rounded-full border transition-colors', checked ? 'border-amber bg-amber/25' : 'border-line-2 bg-bg-2')}
      >
        <span className={cx('absolute top-0.5 size-3.5 rounded-full transition-all', checked ? 'left-[18px] bg-amber' : 'left-0.5 bg-muted')} />
      </button>
      <span>
        <span className="text-sm text-ink">{label}</span>
        {hint && <span className="mt-0.5 block text-xs text-muted">{hint}</span>}
      </span>
    </label>
  )
}

// ---- Feedback ---------------------------------------------------------------------

export function ErrorBanner({ error }: { error: unknown }) {
  if (!error) return null
  const msg = error instanceof Error ? error.message : String(error)
  return (
    <div role="alert" className="flex gap-3 rounded border border-fail/40 bg-fail/10 px-4 py-3 text-sm text-fail">
      <span className="font-mono font-bold">!</span>
      <span>{msg}</span>
    </div>
  )
}

export function Notice({ children, tone = 'info' }: { children: ReactNode; tone?: 'info' | 'ok' | 'warn' }) {
  const c = tone === 'ok' ? 'border-ok/40 bg-ok/10 text-ok' : tone === 'warn' ? 'border-warn/40 bg-warn/10 text-warn' : 'border-line-2 bg-panel-2 text-ink-2'
  return <div className={cx('rounded border px-4 py-3 text-sm', c)}>{children}</div>
}

export function Loading({ label = 'Reading from the server' }: { label?: string }) {
  return (
    <div className="flex items-center gap-3 py-10 font-mono text-xs text-muted">
      <span className="relative h-px w-24 overflow-hidden bg-line">
        <span className="absolute inset-y-0 w-1/2 animate-sweep bg-amber" />
      </span>
      {label}…
    </div>
  )
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="hatch rounded-md border border-dashed border-line-2 px-6 py-12 text-center">
      <p className="font-display text-2xl font-semibold uppercase tracking-wide text-ink">{title}</p>
      {children && <div className="mx-auto mt-2 max-w-md text-sm text-muted">{children}</div>}
    </div>
  )
}

export function Panel({ title, actions, children, className }: { title?: ReactNode; actions?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={cx('panel', className)}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-4 border-b border-line px-4 py-2.5">
          <h2 className="label !text-ink-2">{title}</h2>
          <div className="flex items-center gap-2">{actions}</div>
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  )
}

export function PageHeader({ kicker, title, children }: { kicker?: ReactNode; title: ReactNode; children?: ReactNode }) {
  return (
    <div className="mb-6 flex flex-wrap items-end justify-between gap-4 animate-rise">
      <div>
        {kicker && <div className="label mb-1">{kicker}</div>}
        <h1 className="font-display text-5xl font-bold uppercase leading-none tracking-tight">{title}</h1>
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  )
}

// ---- Confirmation -------------------------------------------------------------------

/**
 * A dialog that asks the person to type a word (usually the target's name)
 * before a risky action runs.
 */
export function ConfirmDialog({
  open,
  title,
  body,
  confirmWord,
  actionLabel,
  danger,
  busy,
  error,
  onConfirm,
  onClose,
}: {
  open: boolean
  title: string
  body: ReactNode
  confirmWord?: string
  actionLabel: string
  danger?: boolean
  busy?: boolean
  error?: unknown
  onConfirm: () => void
  onClose: () => void
}) {
  const [typed, setTyped] = useState('')
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const d = ref.current
    if (!d) return
    if (open && !d.open) {
      setTyped('')
      d.showModal()
    }
    if (!open && d.open) d.close()
  }, [open])
  const ok = !confirmWord || typed === confirmWord
  return (
    <dialog
      ref={ref}
      onClose={onClose}
      className="m-auto w-full max-w-md rounded-md border border-line-2 bg-panel p-0 text-ink backdrop:bg-black/70 backdrop:backdrop-blur-sm"
    >
      <form
        method="dialog"
        onSubmit={(e) => {
          e.preventDefault()
          if (ok) onConfirm()
        }}
        className="space-y-4 p-5"
      >
        <h2 className="font-display text-2xl font-bold uppercase">{title}</h2>
        <div className="text-sm text-ink-2">{body}</div>
        {confirmWord && (
          <Field label={`Type ${confirmWord} to confirm`}>
            <input className="input font-mono" autoFocus value={typed} onChange={(e) => setTyped(e.target.value)} spellCheck={false} />
          </Field>
        )}
        <ErrorBanner error={error} />
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant={danger ? 'danger' : 'primary'} disabled={!ok} busy={busy}>
            {actionLabel}
          </Button>
        </div>
      </form>
    </dialog>
  )
}

export function CopyBlock({ value, className }: { value: string; className?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <div className={cx('group relative rounded border border-line-2 bg-bg-2', className)}>
      <pre className="overflow-x-auto p-3 pr-20 font-mono text-xs leading-relaxed text-amber-2">{value}</pre>
      <button
        type="button"
        onClick={() => {
          void navigator.clipboard.writeText(value).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          })
        }}
        className="absolute right-2 top-2 rounded border border-line-2 bg-panel px-2 py-1 font-mono text-[10px] uppercase tracking-wider text-ink-2 hover:text-ink"
      >
        {copied ? 'Copied' : 'Copy'}
      </button>
    </div>
  )
}
