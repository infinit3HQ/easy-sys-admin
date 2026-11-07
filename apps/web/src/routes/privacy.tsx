import { createFileRoute } from '@tanstack/react-router'
import { LegalPage, showSelfHost } from '#/components/site-frame'

export const Route = createFileRoute('/privacy')({
  head: () => ({
    meta: [
      { title: 'Privacy policy · easy-sys-admin' },
      {
        name: 'description',
        content:
          'Privacy and product policy for the easy-sys-admin public beta.',
      },
    ],
  }),
  component: Privacy,
})

function Privacy() {
  return (
    <LegalPage
      kicker="7 November 2026"
      title="Privacy policy"
      lede="What this site stores, who the beta is for, and which actions the helper will not take."
    >
      <section>
        <h2>This website</h2>
        <p>
          Opening the landing page, these legal pages, or the sign-in page does
          not set an analytics cookie and does not send the visit to an
          advertising network. The session cookie is set only after you sign in.
          It is HttpOnly, limited to this site, and lasts at most 7 days.
        </p>
      </section>
      <section>
        <h2>An account</h2>
        <p>When you create an account or sign in, the install stores:</p>
        <ul>
          <li>Your email address and the name you give.</li>
          <li>
            A password hash, if you use a password. The password itself is not
            stored.
          </li>
          <li>
            The issuer and subject from Google or another OpenID provider, if
            you use one.
          </li>
          <li>
            A personal space, and any organization you create or join, with your
            role.
          </li>
        </ul>
        <p>
          Sign-up on this public beta can be limited to an allow list of email
          addresses. That list is a setting of the install, not a public
          directory.
        </p>
      </section>
      <section>
        <h2>Servers and secrets</h2>
        <p>
          For each server you add, the install stores the hostname, the SSH host
          key it pinned, status and metrics, run history, and an audit log.
          Logins and notification credentials are encrypted with the install's
          master key. API tokens are stored as hashes. Invite and share links
          are shown once.
        </p>
        <p>
          Metrics are counts and resource levels. The public /metrics endpoint
          of the control plane is not exposed through this website.
        </p>
      </section>
      <section>
        <h2>Where it goes</h2>
        <p>
          The public beta keeps this data in its own database, on the host that
          serves this website. It is not sold, and it is not sent to an ad
          network.
        </p>
        <p>
          Alerts go only to the channels an owner configures, such as ntfy,
          Discord, Slack, a webhook, or email. Those messages can include a
          server name and a link back to the board.
        </p>
        {showSelfHost && (
          <p>
            A self-hosted install never sends that data here. Whoever runs the
            install holds the database and the master key, and is the operator
            of that copy.
          </p>
        )}
      </section>
      <section>
        <h2>How long it stays</h2>
        <p>
          Metric samples are kept for 48 hours, and hourly averages for 90 days,
          unless that install changes the retention settings. The audit log and
          run history stay until the account or the server is removed.
        </p>
        <p>
          Removing a server asks the helper to uninstall, then deletes that
          server and its history from the install. Deleting an organization
          removes its servers, groups, and members from that install. Signing
          out deletes the session cookie in the browser.
        </p>
      </section>
      <section>
        <h2>What the product is</h2>
        <p>
          easy-sys-admin is software. You open a board and reach each server
          over SSH.
          {showSelfHost && ' You can also run the control plane yourself.'}{' '}
          Nothing from easy-sys-admin listens on those servers. An agent can
          read the same board through MCP, using an API token, and only within
          that token's scopes.
        </p>
        <p>
          The copy on this website is a public beta. You can create an account
          here if sign-up allows your email.
          {showSelfHost &&
            ' You can also deploy your own install. Both are the same software.'}{' '}
          It is for people who administer their own Linux servers. It is not a
          remote shell, and the helper has no shell.
        </p>
      </section>
      <section>
        <h2>What the helper will not do</h2>
        <p>
          These limits are enforced in the helper, not only hidden in the board:
        </p>
        <ul>
          <li>It does not delete Docker volumes.</li>
          <li>It does not remove an image a container is using.</li>
          <li>It does not run system prune or image prune.</li>
          <li>
            It does not stop or restart protected units and containers, such as
            the edge proxy, unless you type the name to confirm.
          </li>
          <li>
            It stops talking to a server whose host key changed until a person
            accepts the new key.
          </li>
          <li>
            A schedule does not reboot the server the control plane itself runs
            on.
          </li>
        </ul>
      </section>
      <section>
        <h2>While it is in beta</h2>
        <p>
          Behavior, limits, and the pages on this site can change between
          releases. The public beta can be unavailable while it is upgraded.
          Treat servers you attach here as machines you can restore.
          {showSelfHost &&
            ' Keep the master key of a self-hosted install outside that host.'}
        </p>
        <p>
          There is no separate support desk during the beta. A problem with this
          public website is for the operator of this install.
          {showSelfHost && ' A problem with a copy you deployed is for you.'}
        </p>
      </section>
    </LegalPage>
  )
}
