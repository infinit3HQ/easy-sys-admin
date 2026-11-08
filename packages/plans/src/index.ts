/** Public docs site. The web app links here. The docs site uses the paths. */
export const docsSiteUrl = 'https://docs.easysysadmin.app'

/** Upstream Git repository. Self-hosters override the link with ESA_SOURCE_URL. */
export const sourceRepoUrl = 'https://github.com/infinit3HQ/easy-sys-admin'

export const supportEmail = 'support@easysysadmin.app'

/**
 * Caps of the hosted public beta (ESA_BETA_LIMITS). A self-hosted install
 * leaves that switch off and uses ESA_MAX_SERVERS_PER_ACCOUNT instead.
 */
export const betaLimits = {
  serversPerSpace: 10,
  serversTotal: 20,
  organizationsBesidesPersonal: 1,
} as const

export type PlanId = 'beta' | 'self-hosted' | 'managed'

export type PlanCta =
  | { kind: 'current'; label: string }
  | { kind: 'docs'; label: string; path: string }
  | { kind: 'mail'; label: string; href: string }

export type Plan = {
  id: PlanId
  name: string
  price: string
  priceDetail: string
  /** current is the plan in force today. coming-soon is not on sale. */
  state: 'current' | 'coming-soon'
  stateLabel: string
  summary: string
  features: readonly string[]
  cta: PlanCta
}

const waitlist = `mailto:${supportEmail}?subject=${encodeURIComponent('Managed Cloud waitlist')}`

/** The only place prices and the beta caps are written down for the UI. */
export const plans: readonly Plan[] = [
  {
    id: 'beta',
    name: 'Free Beta',
    price: '$0',
    priceDetail: 'during the public beta',
    state: 'current',
    stateLabel: 'Current plan',
    summary:
      'The hosted board you can use today. We run it. Sign in with Google or email. Nothing is charged.',
    features: [
      `${betaLimits.serversPerSpace} servers in each space, ${betaLimits.serversTotal} in total`,
      'One personal space and one organization',
      'Hosted by us',
      'Status, updates, cleanups, schedules, alerts, the CLI, and MCP',
    ],
    cta: { kind: 'current', label: 'Current plan' },
  },
  {
    id: 'self-hosted',
    name: 'Self-Hosted',
    price: 'Free',
    priceDetail: 'forever',
    state: 'coming-soon',
    stateLabel: 'Coming soon',
    summary:
      'The same software, with no license fee and no usage cap in the license, on infrastructure you run.',
    features: [
      'Open source under AGPL-3.0-only',
      'Free for any use, including commercial use',
      'Unlimited servers',
      'Every feature',
      'You run it, and you keep the database and the master key',
      'Community support',
    ],
    cta: {
      kind: 'docs',
      label: 'Read the self-hosting guide',
      path: '/self-hosting',
    },
  },
  {
    id: 'managed',
    name: 'Managed Cloud',
    price: 'TBA',
    priceDetail: 'Coming soon',
    state: 'coming-soon',
    stateLabel: 'Coming soon',
    summary:
      'We host, update, back up, and monitor the control plane for you, with priority support.',
    features: [
      'We host it',
      'We update it',
      'Backups and monitoring included',
      'Priority support',
      'Price not set yet',
      'No payment is taken',
    ],
    cta: { kind: 'mail', label: 'Join the waitlist', href: waitlist },
  },
]

/** Absolute docs URL for a path such as `/self-hosting`. */
export function docsHref(path: string): string {
  const base = docsSiteUrl.endsWith('/') ? docsSiteUrl : `${docsSiteUrl}/`
  return new URL(path.replace(/^\//, ''), base).href
}
