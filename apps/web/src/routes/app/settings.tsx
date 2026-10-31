import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import type { Scope } from '@esa/api'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { Ago } from '#/components/time'
import { Button, CopyBlock, ErrorBanner, Field, Notice, PageHeader, Panel, Toggle } from '#/components/ui'
import { useRole } from './route'

export const Route = createFileRoute('/app/settings')({
  head: () => ({ meta: [{ title: 'Settings · easy-sys-admin' }] }),
  component: Settings,
})

const scopeText: Record<Scope, string> = {
  read: 'Read the board, servers, runs, and alerts',
  'logs:read': 'Read server and container logs',
  actions: 'Start runs and service or container actions',
  admin: 'Change servers, groups, rules, and certificates',
}

function Settings() {
  // The server render cannot know the browser's origin; fill it in after hydration.
  const [origin, setOrigin] = useState('https://esa.example.com')
  useEffect(() => setOrigin(window.location.origin), [])
  return (
    <div className="space-y-6">
      <PageHeader kicker="Account" title="Settings" />
      <div className="grid gap-6 xl:grid-cols-2">
        <Tokens origin={origin} />
        <div className="space-y-6">
          <SshKey />
          <Panel title="CLI">
            <CopyBlock value={`esa login ${origin} --token esa_…\nesa status\nesa servers show <name>\nesa runs start measure --group <group>`} />
          </Panel>
        </div>
      </div>
    </div>
  )
}

function Tokens({ origin }: { origin: string }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const tokens = useQuery(q.tokens())
  const [name, setName] = useState('')
  const [scopes, setScopes] = useState<Array<Scope>>(['read'])
  const [days, setDays] = useState(90)
  const [secret, setSecret] = useState<string | null>(null)
  const create = useMutation({
    mutationFn: () => call(api().POST('/tokens', { body: { name, scopes, expiresInDays: days } })),
    onSuccess: (r) => {
      setSecret(r.secret)
      setName('')
      return qc.invalidateQueries({ queryKey: ['tokens'] })
    },
  })
  const revoke = useMutation({
    mutationFn: (id: number) => call(api().DELETE('/tokens/{tokenId}', { params: { path: { tokenId: id } } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['tokens'] }),
  })

  const mcp = JSON.stringify({ mcpServers: { esa: { type: 'http', url: `${origin}/mcp`, headers: { Authorization: `Bearer ${secret ?? 'esa_…'}` } } } }, null, 2)

  return (
    <Panel title="API tokens · CLI and MCP">
      {secret && (
        <div className="mb-5 space-y-3">
          <Notice tone="warn">Copy the token now. esa stores only a hash and cannot show it again.</Notice>
          <CopyBlock value={secret} />
          <div className="label">MCP client config</div>
          <CopyBlock value={mcp} />
        </div>
      )}
      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault()
          create.mutate()
        }}
      >
        <div className="grid gap-3 sm:grid-cols-[2fr_1fr]">
          <Field label="Name">
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="claude on laptop" required />
          </Field>
          <Field label="Expires after (days)" hint="0 never expires">
            <input className="input font-mono" type="number" min={0} max={3650} value={days} onChange={(e) => setDays(Number(e.target.value))} />
          </Field>
        </div>
        <div>
          {(Object.keys(scopeText) as Array<Scope>).map((s) => (
            <Toggle
              key={s}
              checked={scopes.includes(s)}
              onChange={(on) => setScopes(on ? [...scopes, s] : scopes.filter((x) => x !== s || s === 'read'))}
              label={s}
              hint={scopeText[s] + (s !== 'read' && !canAct ? ' (needs the admin role)' : '')}
            />
          ))}
        </div>
        <ErrorBanner error={create.error} />
        <Button type="submit" variant="primary" busy={create.isPending}>
          Create token
        </Button>
      </form>
      <div className="mt-6 space-y-2 border-t border-line pt-4">
        {(tokens.data?.items ?? []).map((t) => (
          <div key={t.id} className="flex flex-wrap items-center gap-3 font-mono text-xs">
            <span className={t.revokedAt ? 'text-faint line-through' : 'text-ink'}>{t.name}</span>
            <span className="text-faint">{t.prefix}…</span>
            <span className="text-muted">{t.scopes.join(' ')}</span>
            <span className="ml-auto text-faint">{t.revokedAt ? 'revoked' : t.lastUsedAt ? <>used <Ago iso={t.lastUsedAt} /></> : 'never used'}</span>
            {!t.revokedAt && (
              <Button size="sm" variant="ghost" onClick={() => revoke.mutate(t.id)}>
                revoke
              </Button>
            )}
          </div>
        ))}
      </div>
    </Panel>
  )
}

function SshKey() {
  const key = useQuery(q.sshKey())
  return (
    <Panel title="Control plane SSH key">
      <p className="mb-3 text-xs text-muted">
        esa logs in to servers as the esa user with this key. It may run only esa-helper there. To enroll a server by hand, put it in{' '}
        <span className="font-mono">~esa/.ssh/authorized_keys</span> with the <span className="font-mono">restrict</span> option.
      </p>
      {key.data && (
        <>
          <CopyBlock value={key.data.publicKey} />
          <p className="mt-2 font-mono text-[11px] text-muted">{key.data.fingerprint}</p>
        </>
      )}
    </Panel>
  )
}
