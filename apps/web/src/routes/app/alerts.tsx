import { Link, createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient, useSuspenseQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { PlusIcon, SendIcon, Trash2Icon, XIcon } from 'lucide-react'
import type { AlertRule, ChannelConfig, ChannelKind } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago, When } from '#/components/time'
import { Choice, Empty, ErrorBanner, Field, Notice, NumberInput, PageHeader, Panel, Severity, Toggle, cn } from '#/components/kit'
import { Badge } from '#/components/ui/badge'
import { Button } from '#/components/ui/button'
import { Card } from '#/components/ui/card'
import { Input } from '#/components/ui/input'
import { Separator } from '#/components/ui/separator'
import { useRole } from './route'

export const Route = createFileRoute('/app/alerts')({
  loader: ({ context }) => context.queryClient.ensureQueryData(q.alerts(false)),
  head: () => ({ meta: [{ title: 'Alerts · easy-sys-admin' }] }),
  component: Alerts,
})

function Alerts() {
  const [all, setAll] = useState(false)
  const alerts = useQuery(q.alerts(all))
  const items = alerts.data?.items ?? []
  return (
    <div className="space-y-8">
      <div>
        <PageHeader kicker="Signals" title="Alerts">
          <Toggle checked={all} onChange={setAll} label="Include resolved" />
        </PageHeader>
        {items.length === 0 ? (
          <Empty title={all ? 'No alerts recorded' : 'Nothing is wrong'}>Rules and channels are below.</Empty>
        ) : (
          <Card>
            <ul className="divide-y divide-line">
              {items.map((a) => (
                <li key={a.id} className={cn('flex flex-col gap-2 px-4 py-3 sm:flex-row sm:items-center sm:gap-4', a.resolvedAt && 'opacity-50')}>
                  <div className="flex min-w-0 flex-1 items-start gap-3">
                    <span className="mt-0.5 shrink-0">
                      <Severity severity={a.severity} />
                    </span>
                    <div className="min-w-0">
                      <div className="text-sm break-words text-ink">{a.message}</div>
                      <div className="font-mono text-[10px] tracking-wider text-faint uppercase">
                        {a.kind}
                        {a.serverId && (
                          <>
                            {' · '}
                            <Link to="/app/servers/$serverId" params={{ serverId: a.serverId }} className="hover:text-amber">
                              {a.serverName}
                            </Link>
                          </>
                        )}
                      </div>
                    </div>
                  </div>
                  <div className="font-mono text-[11px] text-muted-foreground sm:shrink-0 sm:text-right">
                    {a.resolvedAt ? (
                      <>
                        resolved <Ago iso={a.resolvedAt} />
                      </>
                    ) : a.firedAt ? (
                      <>
                        firing since <Ago iso={a.firedAt} />
                      </>
                    ) : (
                      <>
                        pending since <Ago iso={a.pendingSince} />
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          </Card>
        )}
      </div>
      <div className="grid gap-6 xl:grid-cols-2">
        <Rules />
        <div className="min-w-0 space-y-6">
          <Channels />
          <Certs />
        </div>
      </div>
    </div>
  )
}

const ruleText: Record<AlertRule['kind'], { title: string; unit?: string }> = {
  disk: { title: 'Disk use above', unit: '%' },
  inode: { title: 'Inode use above', unit: '%' },
  memory: { title: 'Memory use above', unit: '%' },
  unreachable: { title: 'Server not answering' },
  failed_unit: { title: 'A systemd unit failed' },
  container: { title: 'Container unhealthy or restarting' },
  reboot_required: { title: 'Reboot required' },
  cert_expiry: { title: 'Certificate expires within', unit: 'days' },
  run_failed: { title: 'A maintenance run failed' },
  host_key: { title: 'Host key changed' },
}

function Rules() {
  const rules = useSuspenseQuery(q.alertRules())
  return (
    <Panel title="Rules" flush>
      <ul className="divide-y divide-line">
        {rules.data.items.map((r) => (
          <RuleRow key={r.id} rule={r} />
        ))}
      </ul>
    </Panel>
  )
}

function RuleRow({ rule }: { rule: AlertRule }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const [r, setR] = useState(rule)
  const dirty = r.enabled !== rule.enabled || r.threshold !== rule.threshold || r.forMinutes !== rule.forMinutes
  const save = useMutation({
    mutationFn: () =>
      call(
        api().PUT('/alerts/rules/{kind}', {
          params: { path: { kind: r.kind } },
          body: {
            enabled: r.enabled,
            threshold: r.threshold,
            forMinutes: r.forMinutes,
          },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['alert-rules'] }),
  })
  const t = ruleText[r.kind]
  return (
    <li className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 py-3">
      <Toggle className="min-w-0 flex-1 basis-56 py-0" checked={r.enabled} disabled={!canAct} onChange={(enabled) => setR({ ...r, enabled })} label={t.title} />
      <div className="flex items-center gap-2 font-mono text-[11px] text-muted-foreground">
        {t.unit && (
          <>
            <NumberInput
              aria-label={`${t.title} (${t.unit})`}
              className="h-8 w-20"
              value={r.threshold}
              disabled={!canAct}
              onChange={(threshold) => setR({ ...r, threshold })}
            />
            <span className="w-8">{t.unit}</span>
          </>
        )}
        <span>for</span>
        <NumberInput
          aria-label="Minutes before it fires"
          className="h-8 w-20"
          value={r.forMinutes}
          disabled={!canAct}
          onChange={(forMinutes) => setR({ ...r, forMinutes })}
        />
        <span>min</span>
      </div>
      {canAct && dirty && (
        <Button size="sm" onClick={() => save.mutate()} busy={save.isPending}>
          Save
        </Button>
      )}
      {save.error && (
        <div className="w-full">
          <ErrorBanner error={save.error} />
        </div>
      )}
    </li>
  )
}

const kindFields: Record<
  ChannelKind,
  Array<{
    key: keyof ChannelConfig
    label: string
    placeholder?: string
    secret?: boolean
  }>
> = {
  ntfy: [
    {
      key: 'url',
      label: 'Topic URL',
      placeholder: 'https://ntfy.sh/my-servers',
    },
    { key: 'token', label: 'Access token (optional)', secret: true },
  ],
  webhook: [
    { key: 'url', label: 'URL', placeholder: 'https://hooks.example.com/esa' },
    { key: 'secret', label: 'Signing secret (optional)', secret: true },
  ],
  discord: [
    {
      key: 'url',
      label: 'Webhook URL',
      placeholder: 'https://discord.com/api/webhooks/…',
      secret: true,
    },
  ],
  slack: [
    {
      key: 'url',
      label: 'Webhook URL',
      placeholder: 'https://hooks.slack.com/services/…',
      secret: true,
    },
  ],
  email: [
    { key: 'smtpHost', label: 'SMTP host', placeholder: 'smtp.example.com' },
    { key: 'smtpPort', label: 'SMTP port', placeholder: '587' },
    { key: 'username', label: 'Username' },
    { key: 'password', label: 'Password', secret: true },
    { key: 'from', label: 'From', placeholder: 'esa@example.com' },
    {
      key: 'to',
      label: 'To (comma-separated)',
      placeholder: 'you@example.com',
    },
  ],
}

function Channels() {
  const qc = useQueryClient()
  const { isOwner, canAct } = useRole()
  const channels = useQuery(q.channels())
  const [kind, setKind] = useState<ChannelKind>('ntfy')
  const [name, setName] = useState('')
  const [cfg, setCfg] = useState<Record<string, string>>({})
  const [tested, setTested] = useState<Record<number, string>>({})

  const create = useMutation({
    mutationFn: () => {
      const config: ChannelConfig = {
        ...cfg,
        smtpPort: cfg.smtpPort ? Number(cfg.smtpPort) : undefined,
        to: cfg.to ? cfg.to.split(',').map((t) => t.trim()) : undefined,
      }
      return call(api().POST('/channels', { body: { name, kind, config } }))
    },
    onSuccess: () => {
      setName('')
      setCfg({})
      return qc.invalidateQueries({ queryKey: ['channels'] })
    },
  })
  const test = useMutation({
    mutationFn: (id: number) =>
      call(
        api().POST('/channels/{channelId}/test', {
          params: { path: { channelId: id } },
        }),
      ),
    onSuccess: (_d, id) => setTested((t) => ({ ...t, [id]: 'sent' })),
    onError: (e, id) => setTested((t) => ({ ...t, [id]: e.message })),
  })
  const remove = useMutation({
    mutationFn: (id: number) =>
      call(
        api().DELETE('/channels/{channelId}', {
          params: { path: { channelId: id } },
        }),
      ),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['channels'] }),
  })
  const list = channels.data?.items ?? []

  return (
    <Panel title="Notification channels">
      <div className="space-y-2">
        {list.length === 0 && <p className="text-sm text-muted-foreground">No channels. Alerts show here only.</p>}
        {list.map((c) => (
          <div key={c.id} className="rounded-md border bg-bg-2/60 px-3 py-2.5">
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
              <Badge variant="amber">{c.kind}</Badge>
              <span className="text-sm text-ink">{c.name}</span>
              <span className="min-w-0 truncate font-mono text-[11px] text-muted-foreground">{c.target}</span>
              <span className="ml-auto flex items-center gap-1">
                {canAct && (
                  <Button size="xs" variant="ghost" onClick={() => test.mutate(c.id)} busy={test.isPending && test.variables === c.id}>
                    <SendIcon /> test
                  </Button>
                )}
                {isOwner && (
                  <Button
                    size="xs"
                    variant="ghost"
                    className="hover:text-fail"
                    onClick={() => remove.mutate(c.id)}
                    busy={remove.isPending && remove.variables === c.id}
                  >
                    <Trash2Icon /> remove
                  </Button>
                )}
              </span>
            </div>
            {(c.lastError || c.lastSentAt) && (
              <div className="mt-1 font-mono text-[10px] text-faint">
                {c.lastError ? (
                  <span className="text-fail">{c.lastError}</span>
                ) : (
                  <>
                    sent <When iso={c.lastSentAt} />
                  </>
                )}
              </div>
            )}
            {tested[c.id] && <div className={cn('mt-1 font-mono text-[11px]', tested[c.id] === 'sent' ? 'text-ok' : 'text-fail')}>{tested[c.id]}</div>}
          </div>
        ))}
      </div>
      <Separator className="my-5" />
      {isOwner ? (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate()
          }}
        >
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Kind">
              <Choice
                value={kind}
                onChange={(v) => {
                  setKind(v as ChannelKind)
                  setCfg({})
                }}
                options={(Object.keys(kindFields) as Array<ChannelKind>).map((k) => ({ value: k, label: k }))}
              />
            </Field>
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="phone" required />
            </Field>
            {kindFields[kind].map((f) => (
              <Field key={f.key} label={f.label}>
                <Input
                  className="font-mono"
                  type={f.secret ? 'password' : 'text'}
                  autoComplete="off"
                  autoCapitalize="off"
                  spellCheck={false}
                  placeholder={f.placeholder}
                  value={cfg[f.key] ?? ''}
                  onChange={(e) => setCfg({ ...cfg, [f.key]: e.target.value })}
                />
              </Field>
            ))}
          </div>
          <ErrorBanner error={create.error} />
          <Button type="submit" busy={create.isPending} className="w-full sm:w-auto">
            <PlusIcon /> Add channel
          </Button>
        </form>
      ) : (
        <p className="text-xs text-muted-foreground">Only account owners can add channels, because they hold credentials.</p>
      )}
    </Panel>
  )
}

function Certs() {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const certs = useQuery(q.certs())
  const [host, setHost] = useState('')
  const add = useMutation({
    mutationFn: () => call(api().POST('/certs', { body: { hostname: host, port: 443 } })),
    onSuccess: () => {
      setHost('')
      return qc.invalidateQueries({ queryKey: ['certs'] })
    },
  })
  const del = useMutation({
    mutationFn: (id: number) => call(api().DELETE('/certs/{certId}', { params: { path: { certId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['certs'] }),
  })
  const list = certs.data?.items ?? []
  return (
    <Panel title="TLS certificates">
      {list.length === 0 ? (
        <p className="text-sm text-muted-foreground">Add the hostnames your servers serve. esa checks them twice a day.</p>
      ) : (
        <ul className="divide-y divide-line/60">
          {list.map((c) => (
            <li key={c.id} className="flex items-start gap-3 py-2 font-mono text-xs first:pt-0 last:pb-0">
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-3">
                  <span className="min-w-0 truncate text-ink">
                    {c.hostname}
                    {c.port !== 443 && `:${c.port}`}
                  </span>
                  {!c.lastError && <span className={cn('shrink-0', (c.daysLeft ?? 99) <= 14 ? 'text-warn' : 'text-ok')}>{c.daysLeft} days</span>}
                </div>
                {c.lastError ? (
                  <div className="mt-0.5 text-[11px] break-words text-fail">{c.lastError}</div>
                ) : (
                  <div className="truncate text-[10px] text-faint">{c.issuer}</div>
                )}
              </div>
              {canAct && (
                <Button size="icon-xs" variant="ghost" className="-mt-1 hover:text-fail" onClick={() => del.mutate(c.id)} aria-label={`Stop checking ${c.hostname}`}>
                  <XIcon />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canAct && (
        <form
          className="mt-4 flex gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            add.mutate()
          }}
        >
          <Input
            className="font-mono"
            aria-label="Hostname"
            inputMode="url"
            autoCapitalize="off"
            spellCheck={false}
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder="app.example.com"
          />
          <Button type="submit" variant="outline" busy={add.isPending} disabled={!host}>
            Add
          </Button>
        </form>
      )}
      <ErrorBanner error={add.error} className="mt-3" />
      {add.isSuccess && (
        <Notice tone="ok" className="mt-3">
          Checked {add.data.hostname}.
        </Notice>
      )}
    </Panel>
  )
}
