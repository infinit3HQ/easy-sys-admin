# Spaces, teams, and shared groups

## Model

- **Account** is a space that owns servers, groups, runs, alerts, channels, certificates, tokens and an SSH key. Nothing changed there.
- **Personal space.** Every user gets exactly one, created at first sign-in (`accounts.personal_of = user id`). It is private: no invites, no other members, it cannot be deleted, and it cannot receive shared groups. It can share its own groups out.
- **Team.** An account with `personal_of = NULL`. Any user can create one and becomes its owner. A user can belong to many teams. The session works in one account at a time (`PUT /me/account`).
- **Roles** stay `owner`, `admin`, `viewer`:
  - owner: everything, plus members, roles, invites for any role, rename, delete, notification channels.
  - admin: servers, groups, runs, actions, logs, alert rules, certificates; invites for admin or viewer; sharing groups.
  - viewer: read only, no logs.
  - A team always keeps at least one owner. The last owner can neither leave nor be demoted.
- **Invite.** A one-time link `{ESA_PUBLIC_URL}/invite/{token}`, valid 7 days. Only the SHA-256 of the token is stored. An optional email restricts who can accept (compared lowercase with the signed-in user's email). Email and password accounts cannot accept email-restricted invites, because their address is never verified; they need an open invite link. Accepting adds the membership, deletes the invite, and switches the session to the team. Already a member: the invite is not used up and the response says so (`alreadyMember`). Accepting needs a browser session.
- **Group share.** A team shares one group, with the servers that are in it at the time of each request, with another team. Access is `view` or `operate`:
  - The owning account keeps the SSH key, the group's settings and schedules, server settings, enrollment, host keys, reinstall, delete, alerts and notifications.
  - The receiving team sees the group and its servers on its board, groups list, server pages, metrics, run history and alerts for those servers.
  - `operate` also lets the receiving team's admins (and owners) start runs on the group or its servers, act on services and containers, check updates, and read logs. Viewers of the receiving team stay read only.
  - Effective permission on a shared resource = the lower of the caller's role in their own team and the share access, where view ≈ viewer and operate ≈ admin limited to the actions above. Token scopes still apply on top.
  - Sharing management (links, access changes, stop sharing) is for admins of the owning account. Admins of the receiving team can remove a shared group from their team.
- **Share link.** A one-time link `{ESA_PUBLIC_URL}/share/{token}`, valid 7 days, created by an admin of the owning account with an access level. The person accepting must be an owner or admin of the receiving team and picks the team. The receiving team cannot be the owner or a personal space, and the group must not already be shared with it.

## Rules that keep sharing safe

- One visibility helper decides what an actor can see: rows of their own account, plus groups shared with their account and servers whose `group_id` is one of those groups. Moving a server out of a shared group ends the access immediately.
- Writes that change ownership-level data (`UpdateServer`, `DeleteServer`, `AcceptHostKey`, `ReinstallHelper`, `NewEnrollToken`, `UpdateGroup`, `DeleteGroup`, assigning a server to a group) stay limited to the owning account. Assigning a server to a group checks the group is owned by the server's account, never a shared one.
- A run's servers must all belong to one owning account. `Run.AccountID` is that owner, `RequestedBy` is the user, and the audit entry goes to the owner with `via_account_id`, plus a copy in the caller's account. A run on a mix of own and shared servers is refused.
- The receiving team sees runs that touch servers visible to it. It does not see the owner's runs on other servers.
- Server and group names are unique per account only, so lists for the receiving team carry `ownerTeam`, and the MCP server resolves names to own servers first and reports ambiguous names.
- Invites and share links never outlive their creator's authority. Removing or demoting a member deletes the invites and share links they created in that team, and accepting checks that the creator is still an admin (an owner, for owner invites) of the owning team. Links whose creator was deleted do not work. API tokens cannot create or accept either kind of link.
- Caps: 20 teams created per user, 100 members per team, 50 pending invites per team, 50 shares per group. Creating invites, share links and teams goes through `limitRate`.
- Deleting a team deletes everything it owns, including shares of its groups and shares it received. Sessions in it fall back to another membership (existing behaviour). The esa key stays on its servers until someone removes the helper.

## API

See `packages/api/openapi.yaml`, tags `teams` and `sharing`, plus `personal` on `AccountMembership`, and `access`, `ownerTeam`, `shareId` on `Group` and `access`, `ownerTeam` on `Server`.
