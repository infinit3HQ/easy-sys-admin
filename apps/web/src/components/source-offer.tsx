import { useLoaderData } from '@tanstack/react-router'

/** "Source code" plus the version and commit of this running build. */
export function SourceOffer({ className }: { className?: string }) {
  const { source } = useLoaderData({ from: '__root__' })
  const commit =
    source.commit === 'none'
      ? 'commit not set'
      : `commit ${source.commit.slice(0, 12)}`
  return (
    <p className={className}>
      <a
        href={source.href}
        className="underline-offset-2 hover:text-ink hover:underline"
      >
        Source code
      </a>
      <span className="text-faint"> · </span>
      <span>
        version {source.version}, {commit}
      </span>
    </p>
  )
}
