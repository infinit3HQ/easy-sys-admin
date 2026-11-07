/**
 * App-level building blocks for the board, made from the shadcn/ui
 * primitives in components/ui. Pages use these so a panel, a field, or a
 * status looks the same everywhere.
 */
import {
  Children,
  cloneElement,
  isValidElement,
  useEffect,
  useId,
  useState,
} from 'react'
import type { ComponentProps, ReactElement, ReactNode } from 'react'
import {
  AlertTriangleIcon,
  CheckIcon,
  CircleAlertIcon,
  CopyIcon,
  InfoIcon,
} from 'lucide-react'
import type { ServerStatus } from '@esa/api'
import { cn } from '#/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '#/components/ui/alert'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardHeader,
  CardTitle,
} from '#/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '#/components/ui/dialog'
import { Input } from '#/components/ui/input'
import { Label } from '#/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '#/components/ui/select'
import { Switch } from '#/components/ui/switch'
import { ToggleGroup, ToggleGroupItem } from '#/components/ui/toggle-group'

export { cn }

// ---- Status --------------------------------------------------------------------

const statusStyle: Record<
  ServerStatus,
  { dot: string; text: string; label: string }
> = {
  ok: {
    dot: 'bg-ok shadow-[0_0_10px_var(--color-ok)]',
    text: 'text-ok',
    label: 'OK',
  },
  warning: {
    dot: 'bg-warn shadow-[0_0_10px_var(--color-warn)]',
    text: 'text-warn',
    label: 'Warning',
  },
  failing: {
    dot: 'bg-fail shadow-[0_0_12px_var(--color-fail)] animate-blink',
    text: 'text-fail',
    label: 'Failing',
  },
  unreachable: { dot: 'bg-down', text: 'text-down', label: 'Unreachable' },
  pending: { dot: 'bg-pending', text: 'text-pending', label: 'Pending' },
}

export function StatusDot({
  status,
  className,
}: {
  status: ServerStatus
  className?: string
}) {
  return (
    <span
      aria-hidden
      className={cn(
        'inline-block size-2 shrink-0 rounded-full',
        statusStyle[status].dot,
        className,
      )}
    />
  )
}

export function StatusBadge({
  status,
  className,
}: {
  status: ServerStatus
  className?: string
}) {
  const s = statusStyle[status]
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 font-mono text-[11px] tracking-widest uppercase',
        s.text,
        className,
      )}
    >
      <StatusDot status={status} />
      {s.label}
    </span>
  )
}

const runStyle: Record<string, string> = {
  running: 'text-amber',
  queued: 'text-muted-foreground',
  succeeded: 'text-ok',
  failed: 'text-fail',
  partial: 'text-warn',
  skipped: 'text-muted-foreground',
  missed: 'text-down',
}

export function RunStatus({ status }: { status: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 font-mono text-[11px] tracking-widest uppercase',
        runStyle[status] ?? 'text-muted-foreground',
      )}
    >
      {status === 'running' && (
        <span className="size-1.5 animate-blink rounded-full bg-amber" />
      )}
      {status}
    </span>
  )
}

export function Severity({ severity }: { severity: string }) {
  return (
    <Badge
      variant={
        severity === 'critical'
          ? 'destructive'
          : severity === 'warning'
            ? 'warn'
            : 'secondary'
      }
    >
      {severity}
    </Badge>
  )
}

// ---- Meters ---------------------------------------------------------------------

/** A horizontal gauge with tick marks; turns amber and red near the thresholds. */
export function Meter({
  value,
  warn = 85,
  fail = 95,
  className,
}: {
  value: number | undefined
  warn?: number
  fail?: number
  className?: string
}) {
  const v = Math.max(0, Math.min(100, value ?? 0))
  const color = v >= fail ? 'bg-fail' : v >= warn ? 'bg-warn' : 'bg-ink-2'
  return (
    <div
      role="meter"
      aria-valuenow={Math.round(v)}
      aria-valuemin={0}
      aria-valuemax={100}
      className={cn(
        'relative h-1.5 w-full overflow-hidden rounded-[1px] bg-line',
        className,
      )}
    >
      <div
        className={cn('h-full transition-[width] duration-700', color)}
        style={{ width: `${v}%` }}
      />
      <div className="pointer-events-none absolute inset-0 bg-[repeating-linear-gradient(90deg,transparent_0_9.5%,var(--color-bg)_9.5%_10%)]" />
    </div>
  )
}

type Tone = 'ok' | 'warn' | 'fail' | 'down'
const toneText: Record<Tone, string> = {
  ok: 'text-ok',
  warn: 'text-warn',
  fail: 'text-fail',
  down: 'text-down',
}

/** A label over a big number. `size="md"` is for dense panels. */
export function Stat({
  label,
  value,
  sub,
  tone,
  size = 'lg',
  word,
}: {
  label: string
  value: ReactNode
  sub?: ReactNode
  tone?: Tone
  size?: 'lg' | 'md'
  /** The value is a word (synced, none), not a number: set it in mono so it does not pose as a figure. */
  word?: boolean
}) {
  return (
    <div className="min-w-0">
      <div className="label">{label}</div>
      {/* min-h, not h: the row keeps one baseline for numbers and words, and
          nothing is clipped when a value is taller or wraps. */}
      <div
        className={cn(
          'mt-1.5 flex items-end leading-[1.1] break-words',
          size === 'lg' ? 'min-h-9 sm:min-h-10' : 'min-h-8',
          word
            ? 'pb-1 font-mono text-sm tracking-wider uppercase'
            : 'font-display tracking-tight tabular-nums',
          !word &&
            (size === 'lg'
              ? 'text-3xl font-bold sm:text-4xl'
              : 'text-[1.75rem] font-semibold'),
          tone ? toneText[tone] : 'text-ink',
        )}
      >
        <span className="min-w-0">
          {word ? value : <Figure value={value} />}
        </span>
      </div>
      {sub && (
        <div className="mt-1.5 font-mono text-[11px] leading-snug text-pretty text-muted-foreground">
          {sub}
        </div>
      )}
    </div>
  )
}

/** Sets "595 MiB" or "12.5%" with the unit smaller than the number, so the figure leads. */
function Figure({ value }: { value: ReactNode }) {
  if (typeof value !== 'string') return value
  const m = /^(-?[\d.,]+)\s*([A-Za-z%]+)$/.exec(value)
  if (!m) return value
  return (
    <>
      {m[1]}
      <span className="ml-0.5 text-[0.55em] font-medium tracking-normal text-ink-2">
        {m[2]}
      </span>
    </>
  )
}

/** A key on the left, a value on the right. Used in every facts panel. */
export function KeyValue({
  k,
  v,
  tone,
  wrap,
}: {
  k: ReactNode
  v: ReactNode
  tone?: Tone
  wrap?: boolean
}) {
  return (
    <div className="flex items-baseline justify-between gap-4 font-mono text-xs">
      <dt className="shrink-0 text-muted-foreground">{k}</dt>
      <dd
        className={cn(
          'min-w-0 text-right',
          wrap ? 'break-all' : 'truncate',
          tone ? toneText[tone] : 'text-ink-2',
        )}
      >
        {v}
      </dd>
    </div>
  )
}

// ---- Controls --------------------------------------------------------------------

/**
 * A labelled form control. The label is tied to the single child control by
 * id, so screen readers announce it and tapping the label focuses the control.
 */
export function Field({
  label,
  hint,
  error,
  children,
  className,
}: {
  label: string
  hint?: ReactNode
  error?: string
  children: ReactNode
  className?: string
}) {
  const auto = useId()
  const only =
    Children.count(children) === 1 && isValidElement(children)
      ? (children as ReactElement<{ id?: string }>)
      : null
  const id = only?.props.id ?? auto
  return (
    <div className={cn('grid min-w-0 content-start gap-1.5', className)}>
      <Label
        htmlFor={only ? id : undefined}
        className="font-mono text-[10px] font-normal tracking-[0.14em] text-muted-foreground uppercase"
      >
        {label}
      </Label>
      {only ? cloneElement(only, { id }) : children}
      {error ? (
        <p className="text-xs text-fail">{error}</p>
      ) : hint ? (
        <p className="text-xs text-muted-foreground">{hint}</p>
      ) : null}
    </div>
  )
}

/** A switch with a label and an optional line of help under it. */
export function Toggle({
  checked,
  onChange,
  label,
  hint,
  disabled,
  className,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  label: ReactNode
  hint?: ReactNode
  disabled?: boolean
  className?: string
}) {
  const id = useId()
  return (
    <div className={cn('flex items-start gap-3 py-1.5', className)}>
      <Switch
        id={id}
        checked={checked}
        onCheckedChange={onChange}
        disabled={disabled}
        className="mt-0.5"
      />
      <div className="min-w-0">
        <Label
          htmlFor={id}
          className="cursor-pointer text-sm leading-snug font-normal text-ink"
        >
          {label}
        </Label>
        {hint && <p className="mt-0.5 text-xs text-muted-foreground">{hint}</p>}
      </div>
    </div>
  )
}

/** A single-choice button row, for a handful of options. */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  size = 'default',
  className,
  'aria-label': ariaLabel,
}: {
  value: T
  onChange: (v: T) => void
  options: ReadonlyArray<{ value: T; label: ReactNode }>
  size?: 'sm' | 'default'
  className?: string
  'aria-label'?: string
}) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size={size}
      value={value}
      aria-label={ariaLabel}
      onValueChange={(v) => v && onChange(v as T)}
      className={cn('flex-wrap', className)}
    >
      {options.map((o) => (
        <ToggleGroupItem
          key={o.value}
          value={o.value}
          className={
            size === 'sm'
              ? 'font-mono text-[10px] tracking-wider uppercase'
              : undefined
          }
        >
          {o.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

/**
 * A shadcn select over a list of options. Radix select cannot hold an empty
 * value, so "nothing picked" is the string "none".
 */
export function Choice({
  id,
  value,
  onChange,
  options,
  placeholder,
  disabled,
  size,
  className,
  'aria-label': ariaLabel,
}: {
  id?: string
  value: string
  onChange: (v: string) => void
  options: ReadonlyArray<{ value: string; label: ReactNode }>
  placeholder?: string
  disabled?: boolean
  size?: 'sm' | 'default'
  className?: string
  'aria-label'?: string
}) {
  return (
    <Select value={value} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        id={id}
        size={size}
        aria-label={ariaLabel}
        className={cn('w-full', className)}
      >
        <SelectValue placeholder={placeholder} />
      </SelectTrigger>
      <SelectContent position="popper">
        {options.map((o) => (
          <SelectItem key={o.value} value={o.value}>
            {o.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

/** A number input that keeps the field editable while it is briefly empty. */
export function NumberInput({
  value,
  onChange,
  className,
  ...rest
}: Omit<ComponentProps<typeof Input>, 'value' | 'onChange' | 'type'> & {
  value: number
  onChange: (v: number) => void
}) {
  return (
    <Input
      {...rest}
      type="number"
      inputMode="numeric"
      className={cn('font-mono', className)}
      value={value}
      onChange={(e) => onChange(Number(e.target.value))}
    />
  )
}

// ---- Feedback ---------------------------------------------------------------------

export function ErrorBanner({
  error,
  className,
}: {
  error: unknown
  className?: string
}) {
  if (!error) return null
  const msg = error instanceof Error ? error.message : String(error)
  return (
    <Alert
      variant="destructive"
      className={cn('rounded-md border-fail/40 bg-fail/10', className)}
    >
      <CircleAlertIcon />
      <AlertDescription className="text-fail">{msg}</AlertDescription>
    </Alert>
  )
}

export function Notice({
  children,
  tone = 'info',
  title,
  className,
}: {
  children: ReactNode
  tone?: 'info' | 'ok' | 'warn'
  title?: ReactNode
  className?: string
}) {
  const c =
    tone === 'ok'
      ? 'border-ok/40 bg-ok/10 text-ok'
      : tone === 'warn'
        ? 'border-warn/40 bg-warn/10 text-warn'
        : 'border-line-2 bg-panel-2 text-ink-2'
  const Icon =
    tone === 'ok' ? CheckIcon : tone === 'warn' ? AlertTriangleIcon : InfoIcon
  return (
    <Alert className={cn('rounded-md', c, className)}>
      <Icon />
      {title && <AlertTitle>{title}</AlertTitle>}
      <AlertDescription className="text-current/90">
        {children}
      </AlertDescription>
    </Alert>
  )
}

export function Loading({
  label = 'Reading from the server',
}: {
  label?: string
}) {
  return (
    <div
      role="status"
      className="flex items-center gap-3 py-10 font-mono text-xs text-muted-foreground"
    >
      <span className="relative h-px w-24 overflow-hidden bg-line">
        <span className="absolute inset-y-0 w-1/2 animate-sweep bg-amber" />
      </span>
      {label}…
    </div>
  )
}

export function Empty({
  title,
  children,
  action,
}: {
  title: string
  children?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="hatch rounded-md border border-dashed border-line-2 px-6 py-12 text-center">
      <p className="font-display text-2xl font-semibold tracking-wide text-ink uppercase">
        {title}
      </p>
      {children && (
        <div className="mx-auto mt-2 max-w-md text-sm text-muted-foreground">
          {children}
        </div>
      )}
      {action && <div className="mt-5 flex justify-center">{action}</div>}
    </div>
  )
}

// ---- Layout --------------------------------------------------------------------------

/** A titled card. Pass `flush` when the body is a table that brings its own padding. */
export function Panel({
  title,
  actions,
  children,
  className,
  flush,
}: {
  title?: ReactNode
  actions?: ReactNode
  children: ReactNode
  className?: string
  flush?: boolean
}) {
  return (
    <Card className={className}>
      {(title || actions) && (
        <CardHeader>
          {title && <CardTitle>{title}</CardTitle>}
          {actions && <CardAction>{actions}</CardAction>}
        </CardHeader>
      )}
      <CardContent className={cn(flush && 'p-0')}>{children}</CardContent>
    </Card>
  )
}

export function PageHeader({
  kicker,
  title,
  description,
  children,
}: {
  kicker?: ReactNode
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
}) {
  return (
    <div className="mb-6 flex animate-rise flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-end sm:justify-between">
      <div className="min-w-0">
        {kicker && (
          <div className="label mb-1.5 flex flex-wrap items-center gap-1.5">
            {kicker}
          </div>
        )}
        <h1 className="font-display text-4xl leading-[0.9] font-bold tracking-tight break-words uppercase sm:text-5xl">
          {title}
        </h1>
        {description && (
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            {description}
          </p>
        )}
      </div>
      {children && (
        <div className="flex flex-wrap items-center gap-2">{children}</div>
      )}
    </div>
  )
}

/** Breadcrumb separator for PageHeader kickers. */
export function Crumb() {
  return <span className="text-faint">/</span>
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
  const confirmId = useId()
  useEffect(() => {
    if (open) setTyped('')
  }, [open])
  const ok = !confirmWord || typed === confirmWord
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (ok) onConfirm()
          }}
          className="grid gap-4"
        >
          <DialogHeader>
            <DialogTitle className="font-sans text-lg leading-snug font-semibold tracking-normal break-words normal-case">
              {title}
            </DialogTitle>
            <DialogDescription className="text-ink-2">{body}</DialogDescription>
          </DialogHeader>
          {confirmWord && (
            <div className="grid gap-1.5">
              <label htmlFor={confirmId} className="text-sm text-ink-2">
                Type{' '}
                <span className="font-mono text-sm tracking-normal text-ink normal-case">
                  {confirmWord}
                </span>{' '}
                to confirm
              </label>
              <Input
                id={confirmId}
                className="border-input font-mono text-ink focus-visible:border-line-2 focus-visible:ring-line-2/60"
                autoFocus
                autoComplete="off"
                autoCapitalize="off"
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                spellCheck={false}
              />
            </div>
          )}
          <ErrorBanner error={error} />
          <DialogFooter className="pt-2">
            <Button type="button" variant="ghost" onClick={onClose}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={danger ? 'destructive' : 'default'}
              disabled={!ok}
              busy={busy}
              className="capitalize"
            >
              {actionLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function CopyBlock({
  value,
  className,
}: {
  value: string
  className?: string
}) {
  const [copied, setCopied] = useState(false)
  return (
    // The button sits in its own column so it can never cover the code.
    <div
      className={cn(
        'flex min-w-0 items-start rounded-md border border-line-2 bg-bg-2',
        className,
      )}
    >
      <pre className="min-w-0 flex-1 overflow-x-auto p-3 font-mono text-xs leading-relaxed text-amber-2">
        {value}
      </pre>
      <Button
        type="button"
        variant="outline"
        size="xs"
        aria-label={copied ? 'Copied' : 'Copy to clipboard'}
        onClick={() => {
          void navigator.clipboard.writeText(value).then(() => {
            setCopied(true)
            setTimeout(() => setCopied(false), 1500)
          })
        }}
        className="m-2 shrink-0 bg-panel"
      >
        {copied ? <CheckIcon className="text-ok" /> : <CopyIcon />}
        <span className="hidden sm:inline">{copied ? 'Copied' : 'Copy'}</span>
      </Button>
    </div>
  )
}
