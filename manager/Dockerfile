# SaaS console (Next.js) in standalone mode. Serves the UI and forwards /api/manager/* to
# manager-api (via next.config rewrites, host in MANAGER_API_URL).
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --legacy-peer-deps || npm install --legacy-peer-deps
COPY . .
RUN npm run build

FROM node:22-alpine AS run
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
# Next standalone output: server.js + minimal node_modules + static files.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
# MANAGER_API_URL set at deploy time (e.g.: http://srv-captain--manager-api:8091).
EXPOSE 3000
CMD ["node", "server.js"]
