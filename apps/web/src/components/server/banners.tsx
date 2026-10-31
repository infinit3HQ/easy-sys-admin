import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import type { Enrollment, Server } from '@esa/api'
import { api, call } from '#/lib/api'
import { Button, CopyBlock, ErrorBanner } from '#/components/ui'

export function HostKeyAlert({ server }: { server: Server }) {
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () =>
      call(api().POST('/servers/{serverId}/host-key', { params: { path: { serverId: server.id } }, body: { fingerprint: server.presentedFingerprint } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['server', server.id] }),
  })
  return (
    <div className="mb-6 rounded-md border border-fail bg-fail/10 p-5">
      <h2 className="font-display text-2xl font-bold uppercase text-fail">Host key changed · all work paused</h2>
      <p className="mt-2 text-sm text-ink-2">
        The server presented a different SSH host key than the one you accepted. That happens after a reinstall, and also when someone is
        intercepting the connection. Nothing runs on this server until you accept the new key.
      </p>
      <dl className="mt-4 grid gap-2 font-mono text-xs sm:grid-cols-[120px_1fr]">
        <dt className="text-muted">Accepted</dt>
        <dd className="break-all text-ink-2">{server.hostKeyFingerprint}</dd>
        <dt className="text-muted">Presented</dt>
        <dd className="break-all text-fail">{server.presentedFingerprint}</dd>
      </dl>
      <p className="mt-4 text-sm text-ink-2">Check the new key on the server console before you accept it:</p>
      <CopyBlock className="mt-2" value="ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub" />
      <div className="mt-4 flex items-center gap-3">
        <Button variant="danger" onClick={() => m.mutate()} busy={m.isPending}>
          I checked it, accept the new key
        </Button>
      </div>
      <div className="mt-3">
        <ErrorBanner error={m.error} />
      </div>
    </div>
  )
}

export function PendingEnrollment({ server }: { server: Server }) {
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const m = useMutation({
    mutationFn: () => call(api().POST('/servers/{serverId}/enrollment', { params: { path: { serverId: server.id } } })),
    onSuccess: setEnrollment,
  })
  return (
    <div className="mb-6 rounded-md border border-amber/50 bg-amber/5 p-5">
      <h2 className="font-display text-2xl font-bold uppercase text-amber">Waiting for enrollment</h2>
      <p className="mt-2 text-sm text-ink-2">Run the enroll command on the server. If it expired, get a new one.</p>
      {enrollment ? (
        <CopyBlock className="mt-4" value={enrollment.command} />
      ) : (
        <Button className="mt-4" onClick={() => m.mutate()} busy={m.isPending}>
          New enroll command
        </Button>
      )}
      <div className="mt-3">
        <ErrorBanner error={m.error} />
      </div>
    </div>
  )
}
