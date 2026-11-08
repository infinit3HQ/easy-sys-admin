# syntax=docker/dockerfile:1
# Static docs image. Build from the repository root:
#   docker build -f apps/docs/Dockerfile -t esa-docs .
#   docker run --rm -p 8088:80 esa-docs
FROM node:26-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS build
WORKDIR /repo
RUN npm install -g pnpm@12.15.0
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/docs/package.json apps/docs/
COPY apps/web/package.json apps/web/
COPY apps/esa/package.json apps/esa/
COPY packages/api/package.json packages/api/
COPY packages/plans/package.json packages/plans/
COPY packages/tsconfig/package.json packages/tsconfig/
COPY packages/eslint-config/package.json packages/eslint-config/
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter @esa/docs...
COPY packages/ packages/
COPY apps/docs/ apps/docs/
RUN pnpm --filter @esa/docs generate && pnpm --filter @esa/docs build

FROM nginx:1.29-alpine@sha256:5616878291a2eed594aee8db4dade5878cf7edcb475e59193904b198d9b830de
COPY apps/docs/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /repo/apps/docs/dist /usr/share/nginx/html
EXPOSE 80
