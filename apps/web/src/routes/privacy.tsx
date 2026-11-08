import { createFileRoute } from '@tanstack/react-router'
import privacy from '../../../../PRIVACY.md?raw'
import { LegalDocument } from '#/components/legal-doc'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

const description =
  'Privacy Policy for the hosted Easy Sys Admin beta at easysysadmin.app. Draft, pending lawyer review.'

export const Route = createFileRoute('/privacy')({
  loader: () => loadSeo(),
  head: ({ loaderData }) => {
    const origin = siteOrigin()
    const seo = loaderData === true
    return {
      meta: [
        { title: 'Privacy Policy · easy-sys-admin' },
        { name: 'description', content: description },
        {
          name: 'robots',
          content: seo ? 'index, follow' : 'noindex, nofollow',
        },
      ],
      links:
        seo && origin ? [{ rel: 'canonical', href: `${origin}/privacy` }] : [],
    }
  },
  component: () => <LegalDocument source={privacy} />,
})
