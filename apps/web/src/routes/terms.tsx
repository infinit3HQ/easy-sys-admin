import { createFileRoute } from '@tanstack/react-router'
import terms from '../../../../TERMS.md?raw'
import { LegalDocument } from '#/components/legal-doc'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

const description =
  'Terms of Service for the hosted Easy Sys Admin beta at easysysadmin.app. Draft, pending lawyer review.'

export const Route = createFileRoute('/terms')({
  loader: () => loadSeo(),
  head: ({ loaderData }) => {
    const origin = siteOrigin()
    const seo = loaderData === true
    return {
      meta: [
        { title: 'Terms of Service · easy-sys-admin' },
        { name: 'description', content: description },
        {
          name: 'robots',
          content: seo ? 'index, follow' : 'noindex, nofollow',
        },
      ],
      links:
        seo && origin ? [{ rel: 'canonical', href: `${origin}/terms` }] : [],
    }
  },
  component: () => <LegalDocument source={terms} />,
})
