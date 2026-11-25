# Access: projects, robots and SSO

## Projects and roles

A repository belongs to the project named by its first path segment: `team/api` lives in `team`,
names without a slash in `library`.

| Role    | Pull | Push, delete tags and manifests | Members, robots, webhooks, repository settings |
|---------|------|---------------------------------|------------------------------------------------|
| `read`  | yes  |                                 |                                                |
| `write` | yes  | yes                             |                                                |
| `admin` | yes  | yes                             | yes                                            |

- Members are local users or OIDC groups; a user gets the highest role they hold either way.
- System admins can push anywhere, and a push to a new project creates it. Everyone else needs a membership.
- System users are either `admin` or `user`; users only see what their memberships give them. Accounts with the old global `read` or `write` role became `user` on upgrade and need memberships.
- Quotas (bytes, set by system admins on the project) reject pushes that would exceed them and emit a `quota_exceeded` event.
- [Immutable tags](#immutable-tags) (project settings, project admins) stop pushes from overwriting existing tags.

Local users are created on the Users page or from the shell:

```sh
echo 'secret-password' | doreg user add --role user ci
```

## Public repositories

A project marked public allows anonymous pulls of all its repositories: `docker pull` works without
`docker login`. A single repository can be made public in its settings while the rest of the project
stays private. Project admins set both flags.

- Anonymous clients get a Bearer challenge and fetch a token from `/v2/token`; Basic credentials sent directly keep working. The token URL is built from the request, so the [reverse proxy](reverse-proxy.md) has to pass the original `Host` and `X-Forwarded-Proto`.
- Public only means pull. Pushing and deleting still need a membership.
- The web UI stays behind the login by default. With `DOREG_PUBLIC_BROWSE=true`, visitors who are not signed in see the public repositories (list, tags, manifests) and a sign-in button; everything else stays behind the login. It is off by default because the list reveals repository names that anonymous pulls alone do not.

## Immutable tags

With **Immutable tags** on in a project's settings, a push that would point an existing tag at a
different manifest is rejected with `403 DENIED` (`docker push` prints `denied: tag v1 exists and is
immutable …`).

**Exceptions** keep single tags overwritable: a comma-separated list of tag patterns in the same
settings, where `*` and `?` are wildcards (`latest, *-dev, nightly-*`).

`DOREG_IMMUTABLE_TAGS=true` turns immutability on for every project, with its own exceptions in
`DOREG_MUTABLE_TAGS`. A tag is overwritable only if neither the server nor the project locks it:
project exceptions do not lift the server-wide setting.

- New tags, pushes by digest and re-pushing the image a tag already points at keep working.
- Deleting a tag still works for anyone with delete permission (project `write`/`admin`, robots with delete); a deleted tag can be pushed again. Leave delete off for CI robots that should only add tags.
- Tags like `latest` need an exception if they should keep moving.
- An [import](migrating.md) into an immutable project reports a tag that already exists there with a different image as failed and copies the rest.

## Repository READMEs

Every repository can have a Markdown README, written on its Overview tab by anyone with write access or
imported once from GitHub (the importer rewrites relative links and images to GitHub; set
`DOREG_GITHUB_TOKEN` for private repositories or to avoid rate limits). The repository page and list also
show the image's `org.opencontainers.image.description` and link its `org.opencontainers.image.source`.
README HTML is rendered on the server without raw HTML, so it is safe on public pages.

## Robot accounts

Robots are credentials for CI. They pull by default; push and delete are opt-in, and each robot can have
an expiry date. Robots are named `robot$<project>+<name>` (or `robot$<name>` for system robots, which
work in every project). Their secret is shown once, on creation or rotation. Quote the `$` in shells:

```sh
echo "$ROBOT_SECRET" | docker login reg.example.com -u 'robot$team+ci' --password-stdin
```

## Single sign-on

SSO turns on when `OIDC_ISSUER` is set (authorization code flow with PKCE; works with Authentik,
Keycloak and other OpenID Connect providers). All `OIDC_*` variables are listed in
[configuration](configuration.md#single-sign-on).

Setup:

1. Create a confidential OpenID client at the provider with `https://<your host>/api/auth/oidc/callback` as redirect URI.
2. Make sure the ID token carries groups or roles (many providers need a groups scope or mapper) if you want to add OIDC groups as project members or use `OIDC_ADMIN_ROLE`.
3. Set `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET` and `OIDC_REDIRECT_URI` and restart.

Keycloak's issuer is `https://sso.example.com/realms/<realm>`.

The first SSO login creates the account. If the name is taken by a local account, the SSO user gets a
suffix (`alice-2`); local accounts are never taken over.

### Who becomes admin

1. If `OIDC_ADMIN_ROLE` or `OIDC_ADMIN_EMAILS` is set, they decide at every SSO login: users matching either become admins, everyone else is a normal user. Removing the role at the provider (or the email from the list) demotes at the next login.
2. If neither is set, roles are managed in doreg (Users page). While the instance has no admin at all, the first person who signs in via SSO becomes admin; doreg logs a warning and an audit entry. Only accounts of your identity provider can sign in, so set one of the variables before opening a provider to the public.

`DOREG_ADMIN_PASSWORD` is optional with SSO; keep a local admin only as a fallback for when the provider is down.

### Docker login with SSO

SSO users log docker in with one command:

```sh
doreg login reg.example.com
```

It shows a code, opens the browser, and runs `docker login` once you approve. The token lasts 90 days
and can be revoked on the account page. `--print` hands out username and token for CI instead;
machines without a browser can use a CLI secret from the account page as the password.

Get `doreg` for Linux, macOS or Windows from the [releases](https://github.com/p-arndt/doreg/releases); it is
the same binary that runs the server, with the web UI embedded.
