import { Link } from '@tanstack/react-router'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { api, call } from '#/lib/api'
import {
  ConfirmDialog,
  CopyBlock,
  ErrorBanner,
  Notice,
  Panel,
} from '#/components/kit'
import { Button } from '#/components/ui/button'

export function AgentSecret({
  serverId,
  token,
  onClose,
}: {
  serverId: number
  token: string
  onClose: () => void
}) {
  return (
    <Panel title="Connect this server">
      <p className="mb-3 text-sm">
        Copy this credential now. esa stores only a hash and cannot show it
        again. It expires after one year. Keep it in a protected file on the
        server.
      </p>
      <CopyBlock value={token} />
      <p className="my-3 text-sm">
        Install esa-agent and the helper using the{' '}
        <Link
          className="text-amber underline"
          to="/docs/$page"
          params={{ page: 'outbound-agent' }}
        >
          outbound agent instructions
        </Link>
        . Use this esa origin and a token file. The agent needs outgoing HTTPS
        only.
      </p>
      <CopyBlock
        value={`ESA_AGENT_URL=${typeof window === 'undefined' ? 'https://your-esa-origin' : window.location.origin}\nESA_AGENT_TOKEN_FILE=/etc/esa/agent.token`}
      />
      <Notice className="my-3">
        Server ID {serverId}. Commands wait for the agent to connect. A missing
        or expired result stays unknown.
      </Notice>
      <Button onClick={onClose}>I saved the credential</Button>
    </Panel>
  )
}

export function AgentConnection({ serverId }: { serverId: number }) {
  const [confirm, setConfirm] = useState<'rotate' | 'revoke' | null>(null)
  const [token, setToken] = useState<string | null>(null)
  const change = useMutation({
    mutationFn: async (action: 'rotate' | 'revoke') => {
      if (action === 'rotate') {
        const data = await call(
          api().POST('/servers/{serverId}/agent-credential', {
            params: { path: { serverId } },
          }),
        )
        setToken(data.token)
      } else
        await call(
          api().DELETE('/servers/{serverId}/agent-credential', {
            params: { path: { serverId } },
          }),
        )
      setConfirm(null)
    },
  })
  return (
    <div className="space-y-4">
      {token ? (
        <AgentSecret
          serverId={serverId}
          token={token}
          onClose={() => setToken(null)}
        />
      ) : (
        <Panel title="Outbound connection">
          <p className="mb-4 text-sm">
            This server connects to esa over HTTPS. Credential changes require a
            sign-in within the last 15 minutes. Rotation and revocation cancel
            pending commands. A command already running may finish on the
            server.
          </p>
          <div className="flex flex-wrap gap-3">
            <Button variant="outline" onClick={() => setConfirm('rotate')}>
              Replace credential
            </Button>
            <Button variant="destructive" onClick={() => setConfirm('revoke')}>
              Disable connection
            </Button>
          </div>
          {change.isSuccess && (
            <Notice className="mt-3">Connection credential updated.</Notice>
          )}
        </Panel>
      )}
      <ConfirmDialog
        open={confirm !== null}
        title={
          confirm === 'rotate'
            ? 'Replace agent credential'
            : 'Disable outbound connection'
        }
        body="Pending commands will be cancelled. Commands already running can finish. Replace the token file and restart the agent after rotation."
        actionLabel={
          confirm === 'rotate' ? 'Replace credential' : 'Disable connection'
        }
        busy={change.isPending}
        error={change.error}
        onClose={() => setConfirm(null)}
        onConfirm={() => {
          if (confirm) change.mutate(confirm)
        }}
      />
      <ErrorBanner error={change.error} />
    </div>
  )
}
