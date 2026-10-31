import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BrushCleaningIcon, GaugeIcon } from 'lucide-react'
import { api, call } from '#/lib/api'
import { q } from '#/lib/queries'
import { RunsTable } from '#/components/runs-table'
import { ErrorBanner, Loading, Panel } from '#/components/kit'
import { Button } from '#/components/ui/button'
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
        <Panel title="Cleanup">
          <div className="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:gap-3">
            <Button variant="outline" onClick={() => start.mutate('measure')} busy={start.isPending && start.variables === 'measure'}>
              <GaugeIcon /> Measure reclaimable space
            </Button>
            <Button onClick={() => start.mutate('clean')} busy={start.isPending && start.variables === 'clean'}>
              <BrushCleaningIcon /> Clean now
            </Button>
          </div>
          <p className="mt-3 text-xs text-muted-foreground">
            Clean prunes idle build cache and old VS Code server copies. Volumes and images in use are never touched.
          </p>
        </Panel>
      )}
      <ErrorBanner error={start.error} />
      {runs.isLoading ? <Loading /> : <RunsTable runs={runs.data?.items ?? []} />}
    </div>
  )
}
