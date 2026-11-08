import { createServerFn } from '@tanstack/react-start'
import { sourceRepoUrl } from '@esa/plans'

export type SourceOffer = {
  /** Repository or archive URL, without a commit suffix. */
  url: string
  /** Link shown in the UI. A GitHub repo URL gains /tree/<commit> when we know it. */
  href: string
  version: string
  commit: string
}

/**
 * AGPL section 13 source offer. ESA_SOURCE_URL overrides the upstream
 * repository. ESA_VERSION and ESA_COMMIT are the build that is running.
 */
export function sourceOfferFromEnv(
  env: Record<string, string | undefined>,
): SourceOffer {
  const raw = (env.ESA_SOURCE_URL ?? '').trim() || sourceRepoUrl
  const url = raw.replace(/\/+$/, '')
  const version = (env.ESA_VERSION ?? '').trim() || 'dev'
  const commit = (env.ESA_COMMIT ?? '').trim() || 'none'
  const githubRepo = /^https:\/\/github\.com\/[^/]+\/[^/]+$/.test(url)
  const sha = /^[0-9a-f]{7,40}$/i.test(commit)
  const href = githubRepo && sha ? `${url}/tree/${commit}` : url
  return { url, href, version, commit }
}

/** Repository, this build, the license, and issues. A non-GitHub archive is one link. */
export function sourceLinks(
  source: SourceOffer,
): Array<{ label: string; href: string }> {
  const github = /^https:\/\/github\.com\/[^/]+\/[^/]+$/.test(source.url)
  const sha = /^[0-9a-f]{7,40}$/i.test(source.commit)
  const links = [{ label: 'Repository', href: source.url }]
  if (github && sha) links.push({ label: 'This build', href: source.href })
  if (github) {
    links.push(
      { label: 'License', href: `${source.url}/blob/HEAD/LICENSE` },
      { label: 'Issues', href: `${source.url}/issues` },
    )
  }
  return links
}

export const loadSourceOffer = createServerFn({ method: 'GET' }).handler(() =>
  sourceOfferFromEnv(process.env),
)
