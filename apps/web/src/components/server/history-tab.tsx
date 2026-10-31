import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { RunsTable } from '#/components/runs-table'
import { Button, ErrorBanner, Loading } from '#/components/ui'
import { useRole } from '#/routes/app/route'

export function HistoryTab({ serverId }: { serverId: number }) {
  const qc = useQueryClient()
  const { canAct } = useRole()
  const runs = useQuery(q.runs({ serverId, limit: 50 }))
  const start = useMutation({
    mutationFn: (kind: 'measure' | 'clean') => call(api().POST('/runs', { body: { kind, serverIds: [serverId] } })),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['runs'] }),
  })
  return (
    <div className="space-y-4">
      {canAct && (
        <div className="flex flex-wrap items-center gap-3">
          <Button onClick={() => start.mutate('measure')} busy={start.isPending && start.variables === 'measure'}>
            Measure reclaimable space
          </Button>
          <Button variant="primary" onClick={() => start.mutate('clean')} busy={start.isPending && start.variables === 'clean'}>
            Clean now
          </Button>
          <span className="text-xs text-muted">Clean prunes idle build cache and old VS Code server copies. Volumes and images in use are never touched.</span>
        </div>
      )}
      <ErrorBanner error={start.error} />
      {runs.isLoading ? <Loading /> : <RunsTable runs={runs.data?.items ?? []} />}
    </div>
  )
}
