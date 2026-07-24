# Webmail (Vite/React) → estático servido por nginx, que faz o proxy SAME-ORIGIN para a API Go.
# A API trata /api (control plane) e faz o brokering do JMAP em /jmap,/auth,/.well-known (H11), por
# isso o nginx NUNCA fala direto com a Stalwart — vai tudo para o serviço `api`.

FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci --legacy-peer-deps || npm install --legacy-peer-deps
COPY . .
# build de produção web (mesma origem). `build` = tsc -b && vite build → ./dist
RUN npm run build

FROM nginx:1.27-alpine
# O host da API é configurável (env API_UPSTREAM) e resolvido em runtime (o serviço pode subir depois).
ENV API_UPSTREAM=srv-captain--api:8000
COPY nginx.conf /etc/nginx/templates/default.conf.template
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
