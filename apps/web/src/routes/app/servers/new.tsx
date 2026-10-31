import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { ArrowRightIcon, KeyRoundIcon, ScanSearchIcon, TerminalIcon } from 'lucide-react'
import type { Enrollment, ProbeResult } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Choice, CopyBlock, ErrorBanner, Field, Notice, NumberInput, PageHeader, Panel, Segmented, Toggle, cn } from '#/components/kit'
import { Button } from '#/components/ui/button'
import { Input } from '#/components/ui/input'
import { Textarea } from '#/components/ui/textarea'
import { useRole } from '../route'

export const Route = createFileRoute('/app/servers/new')({
  head: () => ({ meta: [{ title: 'Add server · easy-sys-admin' }] }),
  component: AddServer,
})

type Method = 'login' | 'command'

const methods = [
  {
    value: 'login',
    title: 'One-time sudo login',
    body: 'Give esa a root or sudo login once. It installs the helper, then forgets the login.',
    icon: KeyRoundIcon,
  },
  {
    value: 'command',
    title: 'Paste a command',
    body: 'Get a one-time command to run on the server yourself. No login leaves your hands.',
    icon: TerminalIcon,
  },
] as const

function AddServer() {
  const [method, setMethod] = useState<Method>('login')
  return (
    <div className="max-w-3xl">
      <PageHeader
        kicker={
          <Link to="/app" className="hover:text-ink">
            Board
          </Link>
        }
        title="Add a server"
      />
      <div role="radiogroup" aria-label="How to add it" className="mb-6 grid gap-3 sm:grid-cols-2">
        {methods.map((m) => {
          const on = method === m.value
          return (
            <button
              key={m.value}
              type="button"
              role="radio"
              aria-checked={on}
              onClick={() => setMethod(m.value)}
              className={cn(
                'panel flex cursor-pointer gap-3 p-4 text-left transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
                on ? 'border-amber/70 bg-amber/5' : 'hover:border-line-2',
              )}
            >
              <m.icon className={cn('mt-0.5 size-5 shrink-0', on ? 'text-amber' : 'text-muted-foreground')} aria-hidden />
              <span>
                <span className={cn('block font-display text-xl leading-tight font-semibold uppercase', on ? 'text-amber' : 'text-ink')}>{m.title}</span>
                <span className="mt-1 block text-xs text-muted-foreground">{m.body}</span>
              </span>
            </button>
          )
        })}
      </div>
      {method === 'login' ? <LoginFlow /> : <CommandFlow />}
    </div>
  )
}

function useGroups() {
  return useQuery(q.groups()).data?.items ?? []
}

function BasicFields({ v, set }: { v: Basic; set: (b: Basic) => void }) {
  const groups = useGroups()
  return (
    <div className="grid gap-4 sm:grid-cols-[1fr_2fr_100px]">
      <Field label="Name">
        <Input value={v.name} onChange={(e) => set({ ...v, name: e.target.value })} placeholder="media-01" autoCapitalize="off" required />
      </Field>
      <Field label="IP or hostname" className="sm:col-span-1">
        <Input
          className="font-mono"
          inputMode="url"
          autoCapitalize="off"
          spellCheck={false}
          value={v.host}
          onChange={(e) => set({ ...v, host: e.target.value })}
          placeholder="203.0.113.10"
          required
        />
      </Field>
      <Field label="SSH port">
        <NumberInput value={v.port} onChange={(port) => set({ ...v, port })} />
      </Field>
      <Field label="Group">
        <Choice
          value={v.groupId === null ? 'none' : String(v.groupId)}
          onChange={(g) => set({ ...v, groupId: g === 'none' ? null : Number(g) })}
          options={[{ value: 'none', label: 'No group' }, ...groups.map((g) => ({ value: String(g.id), label: g.name }))]}
        />
      </Field>
      <div className="sm:col-span-2 sm:self-end">
        <Toggle
          checked={v.hostsControlPlane}
          onChange={(c) => set({ ...v, hostsControlPlane: c })}
          label="esa itself runs on this server"
          hint="It is then never rebooted by a schedule."
        />
      </div>
    </div>
  )
}

interface Basic {
  name: string
  host: string
  port: number
  groupId: number | null
  hostsControlPlane: boolean
}

const emptyBasic: Basic = {
  name: '',
  host: '',
  port: 22,
  groupId: null,
  hostsControlPlane: false,
}

function Step({ n, children }: { n: number; children: string }) {
  return (
    <span className="flex items-center gap-2">
      <span className="grid size-5 place-items-center rounded-sm bg-amber/15 text-amber">{n}</span>
      {children}
    </span>
  )
}

function LoginFlow() {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const { isOwner } = useRole()
  const [basic, setBasic] = useState<Basic>(emptyBasic)
  const [probe, setProbe] = useState<ProbeResult | null>(null)
  const [accepted, setAccepted] = useState(false)
  const [username, setUsername] = useState('root')
  const [authKind, setAuthKind] = useState<'key' | 'password'>('key')
  const [secret, setSecret] = useState('')
  const [passphrase, setPassphrase] = useState('')
  const [keepLogin, setKeepLogin] = useState(false)

  const probeM = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/probe', {
          body: { host: basic.host, port: basic.port },
        }),
      ),
    onSuccess: (r) => {
      setProbe(r)
      setAccepted(false)
    },
  })
  const addM = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers', {
          body: {
            name: basic.name,
            host: basic.host,
            port: basic.port,
            groupId: basic.groupId,
            hostsControlPlane: basic.hostsControlPlane,
            hostKey: probe?.hostKey ?? '',
            keepLogin,
            login:
              authKind === 'key'
                ? {
                    username,
                    privateKey: secret,
                    passphrase: passphrase || undefined,
                  }
                : { username, password: secret },
          },
        }),
      ),
    onSuccess: async (s) => {
      setSecret('')
      await qc.invalidateQueries({ queryKey: ['overview'] })
      await navigate({
        to: '/app/servers/$serverId',
        params: { serverId: s.id },
      })
    },
  })

  return (
    <div className="space-y-6">
      <Panel title={<Step n={1}>Where is it</Step>}>
        <BasicFields v={basic} set={(b) => (setBasic(b), setProbe(null))} />
        <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
          <Button variant="outline" onClick={() => probeM.mutate()} busy={probeM.isPending} disabled={!basic.host}>
            <ScanSearchIcon /> Check host key
          </Button>
          <span className="text-xs text-muted-foreground">Reads the server&apos;s SSH host key. Nothing is sent to the server yet.</span>
        </div>
        <ErrorBanner error={probeM.error} className="mt-3" />
      </Panel>

      {probe && (
        <Panel title={<Step n={2}>Is this your server</Step>}>
          <p className="text-sm text-ink-2">The server presented this {probe.keyType} key. Compare it with the output of this command on the server:</p>
          <CopyBlock className="mt-3" value="ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub" />
          <div className="mt-4 rounded-md border border-amber/40 bg-amber/5 p-4 font-mono text-sm break-all text-amber-2">{probe.fingerprint}</div>
          <Toggle
            className="mt-3"
            checked={accepted}
            onChange={setAccepted}
            label="The fingerprints match"
            hint="esa will refuse to connect if this key ever changes."
          />
        </Panel>
      )}

      {probe && accepted && (
        <Panel title={<Step n={3}>One-time login</Step>}>
          <div className="grid gap-4 sm:grid-cols-[1fr_2fr]">
            <Field label="User" hint="root, or a user that can use sudo">
              <Input className="font-mono" autoCapitalize="off" spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} />
            </Field>
            <Field label="Log in with">
              <Segmented
                value={authKind}
                onChange={setAuthKind}
                options={[
                  { value: 'key', label: 'Private key' },
                  { value: 'password', label: 'Password' },
                ]}
              />
            </Field>
          </div>
          <div className="mt-4 space-y-4">
            {authKind === 'key' ? (
              <>
                <Field label="Private key" hint="Paste the whole key, including the BEGIN and END lines.">
                  <Textarea
                    className="h-32 font-mono text-xs"
                    value={secret}
                    onChange={(e) => setSecret(e.target.value)}
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  />
                </Field>
                <Field label="Passphrase" hint="Only if the key is encrypted.">
                  <Input type="password" value={passphrase} onChange={(e) => setPassphrase(e.target.value)} autoComplete="off" />
                </Field>
              </>
            ) : (
              <Field label="Password" hint="Also used once for sudo when the user is not root.">
                <Input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} autoComplete="off" />
              </Field>
            )}
            {isOwner && (
              <Toggle
                checked={keepLogin}
                onChange={setKeepLogin}
                label="Keep this login (encrypted) for helper updates"
                hint="Off is safer: esa then holds only its own key, which can run nothing but the helper."
              />
            )}
          </div>
          <div className="mt-5 flex flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
            <Button onClick={() => addM.mutate()} busy={addM.isPending} disabled={!basic.name || !secret}>
              Install and add
            </Button>
            <span className="text-xs text-muted-foreground">Creates the esa user, installs the helper, allows one sudo command.</span>
          </div>
          <ErrorBanner error={addM.error} className="mt-3" />
        </Panel>
      )}
    </div>
  )
}

function CommandFlow() {
  const qc = useQueryClient()
  const [basic, setBasic] = useState<Basic>(emptyBasic)
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const m = useMutation({
    mutationFn: () =>
      call(
        api().POST('/servers/enrollments', {
          body: {
            name: basic.name,
            host: basic.host,
            port: basic.port,
            groupId: basic.groupId,
            hostsControlPlane: basic.hostsControlPlane,
          },
        }),
      ),
    onSuccess: async (e) => {
      setEnrollment(e)
      await qc.invalidateQueries({ queryKey: ['overview'] })
    },
  })

  if (enrollment) {
    return (
      <Panel title="Run this on the server">
        <p className="text-sm text-ink-2">
          As a user with sudo, on <span className="font-mono text-ink">{enrollment.server.host}</span>. The command works once and expires at{' '}
          <span suppressHydrationWarning>{new Date(enrollment.expiresAt).toLocaleTimeString()}</span>.
        </p>
        <CopyBlock className="mt-4" value={enrollment.command} />
        <Notice className="mt-4">
          The script creates a system user <span className="font-mono">esa</span>, installs the helper, allows exactly one sudo command, and reports the
          server&apos;s host key back over HTTPS. Read it first with <span className="font-mono break-all">curl -fsSL {enrollment.scriptUrl}</span>.
        </Notice>
        <Button asChild variant="outline" className="mt-4">
          <Link to="/app/servers/$serverId" params={{ serverId: enrollment.server.id }}>
            Open the server page <ArrowRightIcon />
          </Link>
        </Button>
      </Panel>
    )
  }

  return (
    <Panel title="Where is it">
      <BasicFields v={basic} set={setBasic} />
      <Button className="mt-5 w-full sm:w-auto" onClick={() => m.mutate()} busy={m.isPending} disabled={!basic.name || !basic.host}>
        <TerminalIcon /> Get the command
      </Button>
      <ErrorBanner error={m.error} className="mt-3" />
    </Panel>
  )
}
