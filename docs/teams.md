# Organizations, teams, and shared groups

## Model

An account owns servers, groups, runs, alerts, channels, certificates, tokens, and an SSH key. There are two kinds.

**Personal space.** Every user gets exactly one, created at first sign-in (`accounts.personal_of` is the user id). It is private. It has no invites and no other members, it cannot be deleted, and it cannot receive a shared group. It can share its own groups out.

**Organization.** An account with `personal_of` NULL. Any user can create one and becomes its only owner. The create call itself leaves the session where it is. The web app then switches to the new organization. A user can belong to many organizations. The session works in one account at a time (`PUT /me/account`).

**Roles** are `owner`, `admin`, and `viewer`. A new member joins as a viewer and can read the whole organization. Logs, runs, and changes stay closed until their role changes or an extra team grants them a group.

- An owner can do everything in the organization. Only an owner can delete it, rename it, or make another member an owner. An organization always keeps at least one owner. The last owner cannot leave or be demoted.
- An admin manages members, extra teams, servers, groups, runs, actions, logs, alert rules, channels, and certificates. An admin invites admins and viewers, and shares groups. An admin cannot delete the organization, rename it, or change an owner's role.
- A viewer can read the organization.

**Built-in teams** are the admin and viewer roles. They start named Admins and Viewers. An owner or admin can rename both. They are not separate lists of people. Changing a member's role moves them from one name to the other. Owners are not in either team.

**Extra teams** give admin rights on chosen server groups. A member of an extra team can, on those groups and their servers, start runs, act on services and containers, read logs, check updates, and edit that group's own settings. They still cannot add, move, or delete servers, create or delete groups, share groups, or change alerts, channels, certificates, members, or teams. An extra team can also hold a group another organization shares in. On an `operate` share that raises the member to admin, and the share still caps the role at admin. On a `view` share it adds nothing. An owner or admin creates extra teams and chooses their members and groups.

**Invite.** A one-time link `{ESA_PUBLIC_URL}/invite/{token}`, valid 7 days. Only the SHA-256 of the token is stored. An optional email restricts who can accept, compared lowercase with the signed-in user's email. A password account can accept one only after it has opened its confirmation link. Without system mail, password accounts stay unconfirmed and need an open invite. Accepting adds the membership, deletes the invite, and switches the session to the organization. If the person is already a member, the invite is not used and the response says so (`alreadyMember`). Accepting needs a browser session. Only an owner can invite an owner. API tokens cannot create or accept invites.

**Group share.** An organization, or a personal space, shares one group with another organization. The share covers the servers in the group at the time of each request. Access is `view` or `operate`.

- The owning account keeps the SSH key, the group's settings and schedules, server settings, enrollment, host keys, reinstall, delete, alerts, and notifications.
- The receiving organization sees the group and its servers on its board, its groups list, server pages, metrics, run history, and alerts for those servers.
- With `operate`, owners and admins of the receiving organization, and members of an extra team that holds the group, can start runs, act on services and containers, check updates, and read logs. Other viewers stay read only. Editing the group's settings stays with the owner.
- With `view`, everyone in the receiving organization is read only, including its owners.
- Owners and admins of the owning account manage links, access changes, and stopping a share. Owners and admins of the receiving organization can remove a shared group.
- A personal space cannot receive a share.

**Share link.** A one-time link `{ESA_PUBLIC_URL}/share/{token}`, valid 7 days, created by an owner or admin of the owning account with an access level. The person accepting must be an owner or admin of the receiving organization and picks that organization. The receiver cannot be the owner or a personal space, and the group must not already be shared with it. API tokens cannot create or accept share links.

## Rules that keep this safe

- One visibility helper decides what an actor can see: rows of their own account, plus groups shared with their account and servers whose `group_id` is one of those groups. Moving a server out of a shared group ends the access immediately. A viewer of an organization sees all of its servers. An extra team does not hide anything. It only adds rights on its groups.
- The effective role on a group is the organization role. It rises from viewer to admin when one of the actor's extra teams holds the group. On a shared group, `view` keeps the role at viewer and `operate` caps it at admin. Token scopes still apply. The check is `roleOn` in `apps/esa/internal/fleet/roles.go`.
- Writes that change ownership stay with owners and admins of the owning account. That covers server settings, delete, host key, helper reinstall, enrollment, assigning a server to a group, deleting a group, and sharing. `UpdateGroup` on an owned group also allows a viewer who is admin of that group through an extra team.
- A run's servers must all belong to one owning account. `Run.AccountID` is that owner. The audit entry goes to the owner with `via_account_id`, plus a copy in the caller's account.
- The receiving organization sees runs that touch servers visible to it. It does not see the owner's runs on other servers. The run's overall status can still reflect servers the receiver cannot see.
- Server and group names are unique per account only. Lists for the receiver carry `ownerTeam`, the owning organization's name. The MCP server resolves names to own servers first and reports ambiguous names. Server and group responses carry `yourRole`. The web app hides controls from it. The API makes the decision.
- Invites and share links never outlive their creator's authority. Removing or demoting a member deletes the invites and share links they created in that organization. Accepting checks that the creator is still an admin, or an owner when the invite is for an owner. Links whose creator was deleted do not work.
- Removing a member deletes their API tokens and extra-team memberships in that organization, and moves their sessions to another account.
- Caps: 20 organizations a user may own, 100 members per organization, 50 pending invites per organization, 100 extra teams per organization, 200 groups per extra team, 50 shares per group, 50 pending share links per group. `ESA_BETA_LIMITS=true` lowers the owned-organization cap to 1 (the personal space is not counted) and the server cap to 10 per account. Creating an organization, an extra team, an invite, or a share link goes through `limitRate`, 20 per minute per account for each of those actions.
- Deleting an organization deletes everything it owns, including its extra teams, its audit log, shares of its groups, and shares it received. Only an owner can delete it, and they must type its exact name. API tokens cannot delete it. Sessions in it move to each user's personal space, or to another membership. The `esa` user stays on its servers until someone removes the helper.

## API

See `packages/api/openapi.yaml`, tags `teams` and `sharing`. `Organization` has `personal`, `adminTeamName`, and `viewerTeamName`. `Group` and `Server` have `access`, `ownerTeam`, and `yourRole`. A group shared in also has `shareId`.
