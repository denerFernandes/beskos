# Consola SaaS (Next.js) em standalone. Serve a UI e reencaminha /api/manager/* para o manager-api
# (via next.config rewrites, host em MANAGER_API_URL).
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
# Saída standalone do Next: server.js + node_modules mínimos + estáticos.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
# MANAGER_API_URL definido no deploy (ex.: http://srv-captain--manager-api:8091).
EXPOSE 3000
CMD ["node", "server.js"]
