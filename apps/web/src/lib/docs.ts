export type DocNavItem = { slug: string; label: string }

export const docSections: readonly {
  label: string
  items: readonly DocNavItem[]
}[] = [
  {
    label: 'Start',
    items: [
      { slug: '', label: 'Introduction' },
      { slug: 'quickstart', label: 'Quickstart' },
      { slug: 'how-it-works', label: 'How it works' },
    ],
  },
  {
    label: 'Use it',
    items: [
      { slug: 'features', label: 'Features' },
      { slug: 'cli', label: 'CLI' },
      { slug: 'mcp', label: 'MCP' },
      { slug: 'api', label: 'API' },
    ],
  },
  {
    label: 'Run it',
    items: [
      { slug: 'self-hosting', label: 'Self-hosting' },
      { slug: 'remove-a-server', label: 'Remove a server' },
    ],
  },
  {
    label: 'Project',
    items: [
      { slug: 'pricing', label: 'Pricing and plans' },
      { slug: 'license', label: 'License' },
      { slug: 'legal', label: 'Legal' },
      { slug: 'troubleshooting', label: 'Troubleshooting' },
      { slug: 'security', label: 'Security' },
      { slug: 'changelog', label: 'Changelog' },
    ],
  },
]

export function docHref(slug: string): string {
  return slug ? `/docs/${slug}` : '/docs'
}

export const docPathnames = docSections.flatMap((section) =>
  section.items.map((item) => docHref(item.slug)),
)

const docSlugs = new Set(
  docSections.flatMap((section) =>
    section.items.map((item) => item.slug).filter((slug) => slug !== ''),
  ),
)

/** Point a docs-relative link at /docs on this site. Other links stay as written. */
export function rewriteDocHref(href: string): string {
  const hashAt = href.indexOf('#')
  const path = hashAt === -1 ? href : href.slice(0, hashAt)
  const hash = hashAt === -1 ? '' : href.slice(hashAt)
  if (!path.startsWith('/') || path.startsWith('//')) return href
  const slug = path.replace(/^\/+|\/+$/g, '')
  if (!docSlugs.has(slug)) return href
  return docHref(slug) + hash
}

export type DocPage = {
  slug: string
  title: string
  description: string
  body: string
}

const files = import.meta.glob<string>('../content/docs/*.md', {
  query: '?raw',
  import: 'default',
  eager: true,
})

function sourceFor(slug: string): string | undefined {
  const name = slug === '' ? 'index' : slug
  const entry = Object.entries(files).find(([path]) =>
    path.endsWith(`/${name}.md`),
  )
  return entry?.[1]
}

function frontmatter(source: string): {
  title: string
  description: string
  body: string
} {
  const match = /^---\r?\n([\s\S]*?)\r?\n---\r?\n?/.exec(source)
  const raw = match?.[1] ?? ''
  const body = match ? source.slice(match[0].length) : source
  let title = ''
  let description = ''
  for (const line of raw.split('\n')) {
    const split = line.indexOf(':')
    if (split === -1) continue
    const key = line.slice(0, split).trim()
    const value = line.slice(split + 1).trim()
    if (key === 'title') title = value
    if (key === 'description') description = value
  }
  return { title, description, body }
}

export function getDoc(slug: string): DocPage | undefined {
  const known = docSections.some((section) =>
    section.items.some((item) => item.slug === slug),
  )
  if (!known) return undefined
  const source = sourceFor(slug)
  if (!source) return undefined
  const page = frontmatter(source)
  return { slug, ...page }
}
