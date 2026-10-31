# syntax=docker/dockerfile:1
# Web app image (landing page, board, MCP endpoint). Build from the repository root:
#   docker build -f apps/web/Dockerfile .
FROM node:26-alpine AS build
WORKDIR /repo
# Node 25+ no longer ships corepack.
RUN npm install -g pnpm@12.15.0

# Install with only the manifests first, so the dependency layer is cached.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
COPY apps/esa/package.json apps/esa/
COPY packages/api/package.json packages/api/
COPY packages/tsconfig/package.json packages/tsconfig/
COPY packages/eslint-config/package.json packages/eslint-config/
RUN --mount=type=cache,id=pnpm,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter @esa/web...

COPY packages/ packages/
COPY apps/web/ apps/web/
RUN pnpm --filter @esa/web build

FROM node:26-alpine
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOST=0.0.0.0
# Nitro bundles the server and its dependencies into .output.
COPY --from=build --chown=node:node /repo/apps/web/.output ./.output
USER node
EXPOSE 3000
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:3000/ || exit 1
CMD ["node", ".output/server/index.mjs"]
