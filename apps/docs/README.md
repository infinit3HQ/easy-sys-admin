# Documentation site

Public docs for Easy Sys Admin, built with [Astro Starlight](https://starlight.astro.build/). The intended hostname is `https://docs.easysysadmin.app`. The build is a static site: search (Pagefind), a sidebar, and light and dark themes. Nothing here talks to the control plane.

## Run it

From the repository root:

```sh
pnpm install
pnpm --filter @esa/docs generate   # refresh the API page from packages/api/openapi.yaml
pnpm --filter @esa/docs dev        # http://127.0.0.1:4321
pnpm --filter @esa/docs build      # writes apps/docs/dist
pnpm --filter @esa/docs preview
```

`pnpm turbo run build` builds this package with the others. The build fails if a docs page links to a missing page or heading.

Plan names and the beta caps come from `packages/plans`. Change them there, not in the markdown.

## Deploy

Any static host can serve `apps/docs/dist`. Point `docs.easysysadmin.app` at it and turn on HTTPS.

The image in this directory is nginx serving that directory:

```sh
docker build -f apps/docs/Dockerfile -t esa-docs .
docker run --rm -p 8088:80 esa-docs
```

To put it behind the same Traefik as the app, run the image on the `proxy` network and add a router for `docs.easysysadmin.app`. It is not part of `deploy/compose.yaml`, and the production deploy workflow does not publish it. CI does build it (`pnpm turbo run build`).

A typical Traefik label set, for a compose service named `docs` that you add yourself:

```yaml
labels:
  - traefik.enable=true
  - traefik.docker.network=proxy
  - traefik.http.routers.esa-docs.rule=Host(`docs.easysysadmin.app`)
  - traefik.http.routers.esa-docs.entrypoints=websecure
  - traefik.http.routers.esa-docs.tls.certresolver=cloudflare
  - traefik.http.services.esa-docs.loadbalancer.server.port=80
```

GitHub Pages works too: upload `apps/docs/dist` and set the custom domain. The site uses extensionless URLs (`/quickstart` is `quickstart.html`), so the host must map those. The nginx config in this directory does.

## API page

`src/content/docs/api.md` is generated. Do not edit it. After changing `packages/api/openapi.yaml`, run `pnpm --filter @esa/docs generate` and commit the result. CI checks that the file matches a fresh generate.
