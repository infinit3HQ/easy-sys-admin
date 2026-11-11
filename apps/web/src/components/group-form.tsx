import { useState } from 'react'
import type { GroupInput } from '@esa/api'
import {
  Choice,
  ErrorBanner,
  Field,
  NumberInput,
  Panel,
  Toggle,
  cn,
} from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { NativeSelect, NativeSelectOption } from '#/components/ui/native-select'
import { ToggleGroup, ToggleGroupItem } from '#/components/ui/toggle-group'

const days = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const
type Day = (typeof days)[number]

export const defaultGroup: GroupInput = {
  name: '',
  timezone:
    typeof Intl !== 'undefined'
      ? Intl.DateTimeFormat().resolvedOptions().timeZone
      : 'UTC',
  clean: {
    enabled: false,
    days: ['sun'],
    time: '03:30',
    buildCache: true,
    vscode: true,
    journal: false,
    journalSize: '200M',
  },
  updates: {
    enabled: false,
    mode: 'security',
    days: ['sun'],
    time: '04:00',
    hold: [],
    rebootIfRequired: false,
    batchSize: 1,
    maxFailures: 1,
  },
  checks: [],
}

function DayPicker({
  id,
  value,
  onChange,
}: {
  id?: string
  value: Array<Day>
  onChange: (d: Array<Day>) => void
}) {
  return (
    <ToggleGroup
      id={id}
      type="multiple"
      variant="outline"
      size="sm"
      spacing={1}
      value={value}
      onValueChange={(v) => onChange(days.filter((d) => v.includes(d)))}
      className="flex-wrap"
    >
      {days.map((d) => (
        <ToggleGroupItem
          key={d}
          value={d}
          aria-label={d}
          className="w-11 font-mono text-[11px] uppercase"
        >
          {d}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

export function GroupForm({
  initial,
  submitLabel,
  busy,
  error,
  disabled,
  onSubmit,
}: {
  initial: GroupInput
  submitLabel: string
  busy?: boolean
  error?: unknown
  disabled?: boolean
  onSubmit: (g: GroupInput) => void
}) {
  const [g, setG] = useState<GroupInput>(initial)
  const [hold, setHold] = useState(initial.updates.hold.join(', '))
  const zones =
    typeof Intl !== 'undefined' && 'supportedValuesOf' in Intl
      ? Intl.supportedValuesOf('timeZone')
      : [g.timezone]
  const setClean = (c: Partial<GroupInput['clean']>) =>
    setG({ ...g, clean: { ...g.clean, ...c } })
  const setUpd = (u: Partial<GroupInput['updates']>) =>
    setG({ ...g, updates: { ...g.updates, ...u } })

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSubmit({
          ...g,
          updates: {
            ...g.updates,
            hold: hold
              .split(',')
              .map((h) => h.trim())
              .filter(Boolean),
          },
        })
      }}
    >
      <fieldset
        disabled={disabled}
        className="min-w-0 space-y-6 disabled:opacity-75"
      >
        <Panel title="Group">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Name">
              <Input
                value={g.name}
                onChange={(e) => setG({ ...g, name: e.target.value })}
                placeholder="production"
                required
              />
            </Field>
            <Field label="Timezone" hint="Schedule times are in this zone.">
              <NativeSelect
                value={g.timezone}
                onChange={(e) => setG({ ...g, timezone: e.target.value })}
              >
                {[...new Set(['UTC', ...zones])].map((z) => (
                  <NativeSelectOption key={z}>{z}</NativeSelectOption>
                ))}
              </NativeSelect>
            </Field>
          </div>
        </Panel>

        <Panel
          title="Cleanups"
          actions={
            <Toggle
              className="py-0"
              checked={g.clean.enabled}
              onChange={(enabled) => setClean({ enabled })}
              label="On a schedule"
              disabled={disabled}
            />
          }
        >
          <div className={cn('space-y-5', !g.clean.enabled && 'opacity-50')}>
            <div className="flex flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-end sm:gap-6">
              <Field label="Days">
                <DayPicker
                  value={g.clean.days}
                  onChange={(d) => setClean({ days: d })}
                />
              </Field>
              <Field label="Time">
                <Input
                  className="w-32 font-mono"
                  type="time"
                  value={g.clean.time}
                  onChange={(e) => setClean({ time: e.target.value })}
                />
              </Field>
            </div>
            <div className="grid gap-x-6 gap-y-1 sm:grid-cols-2">
              <Toggle
                checked={g.clean.buildCache}
                onChange={(buildCache) => setClean({ buildCache })}
                disabled={disabled}
                label="Docker build cache"
                hint="Only when idle. A server running a build is skipped."
              />
              <Toggle
                checked={g.clean.vscode}
                onChange={(vscode) => setClean({ vscode })}
                disabled={disabled}
                label="Old VS Code server copies"
                hint="Keeps the newest copy and any copy in use."
              />
              <Toggle
                checked={g.clean.journal}
                onChange={(journal) => setClean({ journal })}
                disabled={disabled}
                label="Cap the systemd journal"
              />
              <Field label="Journal size">
                <Input
                  className="w-32 font-mono"
                  value={g.clean.journalSize}
                  onChange={(e) => setClean({ journalSize: e.target.value })}
                />
              </Field>
            </div>
          </div>
        </Panel>

        <Panel
          title="Package updates"
          actions={
            <Toggle
              className="py-0"
              checked={g.updates.enabled}
              onChange={(enabled) => setUpd({ enabled })}
              label="On a schedule"
              disabled={disabled}
            />
          }
        >
          <p className="mb-4 text-xs text-muted-foreground">
            Servers are updated in batches, in name order. The server hosting
            esa always goes on its own. Once the failure limit is reached, the
            rest of the group is skipped.
          </p>
          <div className={cn('space-y-5', !g.updates.enabled && 'opacity-50')}>
            <div className="flex flex-col gap-4 sm:flex-row sm:flex-wrap sm:items-end sm:gap-6">
              <Field label="What" className="sm:w-56">
                <Choice
                  disabled={disabled}
                  value={g.updates.mode}
                  onChange={(v) => setUpd({ mode: v as 'security' | 'all' })}
                  options={[
                    { value: 'security', label: 'Security updates only' },
                    { value: 'all', label: 'All updates (apt upgrade)' },
                  ]}
                />
              </Field>
              <Field label="Days">
                <DayPicker
                  value={g.updates.days}
                  onChange={(d) => setUpd({ days: d })}
                />
              </Field>
              <Field label="Time">
                <Input
                  className="w-32 font-mono"
                  type="time"
                  value={g.updates.time}
                  onChange={(e) => setUpd({ time: e.target.value })}
                />
              </Field>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field
                label="Servers at a time"
                hint="Upgraded together. 1 is the safest."
              >
                <NumberInput
                  className="w-24"
                  min={1}
                  max={50}
                  required
                  value={g.updates.batchSize}
                  onChange={(batchSize) => setUpd({ batchSize })}
                />
              </Field>
              <Field
                label="Stop after failures"
                hint="The run stops once this many servers fail."
              >
                <NumberInput
                  className="w-24"
                  min={1}
                  max={1000}
                  required
                  value={g.updates.maxFailures}
                  onChange={(maxFailures) => setUpd({ maxFailures })}
                />
              </Field>
            </div>
            <Field label="Never upgrade" hint="Package names, comma-separated.">
              <Input
                className="font-mono"
                autoCapitalize="off"
                spellCheck={false}
                value={hold}
                onChange={(e) => setHold(e.target.value)}
                placeholder="docker-ce, postgresql-16"
              />
            </Field>
            <Toggle
              checked={g.updates.rebootIfRequired}
              onChange={(rebootIfRequired) => setUpd({ rebootIfRequired })}
              disabled={disabled}
              label="Reboot after updates when a server asks for it"
              hint="Each server reboots after its own update, and its batch waits for it to come back. The server hosting esa is skipped."
            />
          </div>
        </Panel>

        <ChecksEditor
          checks={g.checks ?? []}
          disabled={disabled}
          onChange={(checks) => setG({ ...g, checks })}
        />

        <ErrorBanner error={error} />
        {!disabled && (
          <Button type="submit" busy={busy} className="w-full sm:w-auto">
            {submitLabel}
          </Button>
        )}
      </fieldset>
    </form>
  )
}

type Check = NonNullable<GroupInput['checks']>[number]

function ChecksEditor({
  checks,
  disabled,
  onChange,
}: {
  checks: Check[]
  disabled?: boolean
  onChange: (checks: Check[]) => void
}) {
  const set = (i: number, next: Partial<Check>) =>
    onChange(checks.map((c, n) => (n === i ? { ...c, ...next } : c)))
  return (
    <Panel title="Checks">
      <p className="mb-3 text-xs text-muted-foreground">
        An update runs these before and after apt, on each server's own address.
        A blocking failure stops that server. One failed canary skips the rest
        of the group. Eight checks at most.
      </p>
      <ul className="space-y-3">
        {checks.map((c, i) => (
          <li
            key={i}
            className="grid gap-2 sm:grid-cols-[8rem_7rem_minmax(0,1fr)_auto] sm:items-end"
          >
            <Field label="Name">
              <Input
                value={c.name}
                maxLength={40}
                disabled={disabled}
                onChange={(e) => set(i, { name: e.target.value })}
              />
            </Field>
            <Field label="Kind">
              <NativeSelect
                value={c.kind}
                disabled={disabled}
                onChange={(e) => {
                  const kind = e.target.value as Check['kind']
                  set(i, {
                    kind,
                    path: kind === 'http' ? (c.path ?? '/') : c.path,
                    method: kind === 'http' ? (c.method ?? 'GET') : c.method,
                  })
                }}
              >
                <NativeSelectOption value="tcp">TCP</NativeSelectOption>
                <NativeSelectOption value="http">HTTP</NativeSelectOption>
                <NativeSelectOption value="unit">Service</NativeSelectOption>
                <NativeSelectOption value="container">
                  Container
                </NativeSelectOption>
              </NativeSelect>
            </Field>
            <CheckTarget
              c={c}
              disabled={disabled}
              onChange={(next) => set(i, next)}
            />
            <div className="flex gap-2">
              <Toggle
                checked={c.blocking}
                disabled={disabled}
                onChange={(blocking) => set(i, { blocking })}
                label="Blocking"
              />
              {!disabled && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => onChange(checks.filter((_, n) => n !== i))}
                >
                  Remove
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
      {!disabled && checks.length < 8 && (
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="mt-3"
          onClick={() =>
            onChange([
              ...checks,
              {
                name: `check ${checks.length + 1}`,
                kind: 'tcp',
                blocking: true,
                port: 443,
              },
            ])
          }
        >
          Add a check
        </Button>
      )}
    </Panel>
  )
}

function CheckTarget({
  c,
  disabled,
  onChange,
}: {
  c: Check
  disabled?: boolean
  onChange: (next: Partial<Check>) => void
}) {
  if (c.kind === 'unit') {
    return (
      <Field label="Service">
        <Input
          className="font-mono"
          value={c.unit ?? ''}
          disabled={disabled}
          placeholder="nginx"
          onChange={(e) => onChange({ unit: e.target.value })}
        />
      </Field>
    )
  }
  if (c.kind === 'container') {
    return (
      <Field label="Container">
        <Input
          className="font-mono"
          value={c.container ?? ''}
          disabled={disabled}
          placeholder="web"
          onChange={(e) => onChange({ container: e.target.value })}
        />
      </Field>
    )
  }
  return (
    <div className="flex flex-wrap gap-2">
      <Field label="Port">
        <NumberInput
          className="w-24"
          min={1}
          max={65535}
          value={c.port ?? 80}
          disabled={disabled}
          onChange={(port) => onChange({ port })}
        />
      </Field>
      {c.kind === 'http' && (
        <>
          <Field label="Path">
            <Input
              className="w-36 font-mono"
              value={c.path ?? '/'}
              disabled={disabled}
              onChange={(e) => onChange({ path: e.target.value })}
            />
          </Field>
          <Toggle
            checked={!!c.tls}
            disabled={disabled}
            onChange={(tls) => onChange({ tls, method: c.method ?? 'GET' })}
            label="HTTPS"
          />
        </>
      )}
    </div>
  )
}
