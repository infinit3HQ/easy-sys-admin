import { createFileRoute } from '@tanstack/react-router'
import { LegalPage, showSelfHost } from '#/components/site-frame'
import { siteOrigin } from '#/lib/origin'
import { loadSeo } from '#/lib/seo'

export const Route = createFileRoute('/terms')({
  loader: () => loadSeo(),
  head: ({ loaderData }) => {
    const origin = siteOrigin()
    const seo = loaderData === true
    const description = showSelfHost
      ? 'Terms for the easy-sys-admin public beta and for running your own install.'
      : 'Terms for the easy-sys-admin public beta.'
    return {
      meta: [
        { title: 'Terms of use · easy-sys-admin' },
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
  component: Terms,
})

function Terms() {
  return (
    <LegalPage
      kicker="7 November 2026"
      title="Terms of use"
      lede={
        showSelfHost
          ? 'easy-sys-admin is software for operating Linux servers. These terms cover the public beta on this website. A copy you run yourself is yours to operate.'
          : 'easy-sys-admin is software for operating Linux servers. These terms cover the public beta on this website.'
      }
    >
      <section>
        <h2>The public beta</h2>
        <p>
          This website is one install of the product, and it is in beta. Pages,
          the board, and the API can change. An account here is for trying the
          product on servers you administer. It is not a paid support contract.
        </p>
        <p>
          Sign-up on this install can be limited to specific email addresses.
          The first account on an empty database can always be created. Closing
          sign-up does not delete accounts that already exist.
        </p>
      </section>
      <section>
        <h2>Your servers</h2>
        <p>
          Add only machines you are allowed to administer. The control plane
          logs in over SSH as a restricted user and runs one helper binary. You
          are responsible for what that helper is asked to do: package updates,
          cleanups, service and container actions, and reboots.
        </p>
        <p>
          The helper refuses some destructive actions. It does not delete Docker
          volumes, remove images a container is using, or prune the Docker
          system. Those limits are how the product behaves. They are not a
          promise that a command cannot disrupt a server.
        </p>
      </section>
      {showSelfHost && (
        <section>
          <h2>Running your own install</h2>
          <p>
            If you deploy easy-sys-admin yourself, you operate that install. You
            hold the database and the master key. These terms still ask you not
            to use the software against machines you do not administer. They do
            not make the operator of this website responsible for your install.
          </p>
        </section>
      )}
      <section>
        <h2>Accounts and access</h2>
        <p>
          Keep your password and API tokens to yourself. A token can do what its
          scopes allow, including through MCP. Organizations have owners,
          admins, and viewers. Only an owner can delete an organization, and
          there is always at least one owner.
        </p>
        <p>
          The operator of this public beta can close an account that attacks
          other accounts, adds machines the person does not administer, or tries
          to break the helper's limits. You can delete an organization you own
          by typing its name.
        </p>
      </section>
      <section>
        <h2>No warranty</h2>
        <p>
          The beta is provided as it runs today. It can lose data, fail a run,
          or be unavailable. Do not point it at a machine you cannot restore.
          {showSelfHost &&
            ' Keep a copy of your master key somewhere other than the host. Without that key, stored server logins cannot be read.'}
        </p>
      </section>
    </LegalPage>
  )
}
