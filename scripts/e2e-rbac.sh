#!/usr/bin/env bash
# E2E de segurança do RBAC: papéis, permissões, convites, último-owner, isolamento entre tenants.
set -euo pipefail
PG=rbac-pg; PGPORT=55437; APIPORT=8204
BACKEND="$(cd "$(dirname "$0")/../backend" && pwd)"
cleanup(){ [[ -n "${API_PID:-}" ]]&&kill $API_PID 2>/dev/null||true; docker rm -f $PG >/dev/null 2>&1||true; }
trap cleanup EXIT

docker rm -f $PG >/dev/null 2>&1||true
docker run -d --name $PG -e POSTGRES_USER=mailin -e POSTGRES_PASSWORD=r -e POSTGRES_DB=mailin -p $PGPORT:5432 postgres:16-alpine >/dev/null
for i in $(seq 1 30); do docker exec $PG pg_isready -U mailin >/dev/null 2>&1 && break; sleep 1; done

export DATABASE_URL="postgres://mailin:r@localhost:$PGPORT/mailin?sslmode=disable"
cd "$BACKEND"; HTTP_ADDR=":$APIPORT" go run ./cmd/api >/tmp/rbac-api.log 2>&1 & API_PID=$!
for i in $(seq 1 20); do curl -fsS localhost:$APIPORT/healthz >/dev/null 2>&1 && break; sleep 1; done
API="localhost:$APIPORT/api"; PW=correct-horse-battery-9273
J(){ python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }
code(){ curl -s -o /dev/null -w "%{http_code}" "$@"; }
pass(){ echo "   ✓ $1"; }
fail(){ echo "   ✗ $1"; exit 1; }

echo "== owner (signup) =="
curl -s -c /tmp/owner -X POST $API/auth/signup -d '{"org_name":"Org A","email":"owner@a.com","password":"'$PW'"}' >/dev/null
ROLE=$(curl -s -b /tmp/owner $API/me | J "['role']"); [[ $ROLE == owner ]]&&pass "signup vira owner"||fail "role=$ROLE"
NPERM=$(curl -s -b /tmp/owner $API/me | J "['permissions'].__len__()"); [[ $NPERM -ge 8 ]]&&pass "owner tem todas as permissões ($NPERM)"||fail "perms=$NPERM"

echo "== convite + aceite =="
TOKEN=$(curl -s -b /tmp/owner -X POST $API/invites -d '{"email":"member@a.com","role":"member"}' | J "['token']")
[[ -n $TOKEN ]]&&pass "owner cria convite"||fail "sem token"
EMAIL=$(curl -s $API/invites/$TOKEN | J "['email']"); [[ $EMAIL == member@a.com ]]&&pass "convite público resolvível"||fail "get invite"
curl -s -c /tmp/member -X POST $API/auth/accept 2>/dev/null >/dev/null || true
curl -s -c /tmp/member -X POST $API/invites/accept -d '{"token":"'$TOKEN'","name":"Member","password":"'$PW'"}' >/dev/null
MROLE=$(curl -s -b /tmp/member $API/me | J "['role']"); [[ $MROLE == member ]]&&pass "aceite cria member logado"||fail "role=$MROLE"

echo "== permissões (member é leitura) =="
[[ $(code -b /tmp/member -X POST $API/domains -d '{"dominio":"x.com"}') == 403 ]]&&pass "member NÃO cria domínio (403)"||fail "member criou domínio!"
[[ $(code -b /tmp/member $API/domains) == 200 ]]&&pass "member vê domínios (200)"||fail "member não vê domínios"
[[ $(code -b /tmp/member $API/members) == 403 ]]&&pass "member NÃO lista membros (403)"||fail "member listou membros!"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"x@a.com","role":"member"}') == 403 ]]&&pass "member NÃO convida (403)"||fail "member convidou!"

echo "== escalonamento de privilégio =="
MID=$(curl -s -b /tmp/owner $API/members | python3 -c "import sys,json;print([m['id'] for m in json.load(sys.stdin) if m['role']=='member'][0])")
curl -s -b /tmp/owner -X PATCH $API/members/$MID/role -d '{"role":"admin"}' >/dev/null
[[ $(curl -s -b /tmp/member $API/me | J "['role']") == admin ]]&&pass "owner promove member→admin"||fail "promoção falhou"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"y@a.com","role":"owner"}') == 403 ]]&&pass "admin NÃO convida como owner (anti-escalonamento)"||fail "admin criou owner!"
[[ $(code -b /tmp/member -X POST $API/invites -d '{"email":"y@a.com","role":"member"}') == 201 ]]&&pass "admin convida member (201)"||fail "admin não convidou"
# internal_admin não é atribuível
[[ $(code -b /tmp/owner -X POST $API/invites -d '{"email":"z@a.com","role":"internal_admin"}') == 400 ]]&&pass "internal_admin não é atribuível (400)"||fail "internal_admin atribuído!"

echo "== último owner protegido =="
OID=$(curl -s -b /tmp/owner $API/members | python3 -c "import sys,json;print([m['id'] for m in json.load(sys.stdin) if m['role']=='owner'][0])")
[[ $(code -b /tmp/owner -X PATCH $API/members/$OID/role -d '{"role":"admin"}') == 409 ]]&&pass "não rebaixa o único owner (409)"||fail "rebaixou último owner!"
[[ $(code -b /tmp/owner -X DELETE $API/members/$OID) == 409 ]]&&pass "não remove o único owner (409)"||fail "removeu último owner!"

echo "== isolamento entre tenants =="
curl -s -c /tmp/owner2 -X POST $API/auth/signup -d '{"org_name":"Org B","email":"owner@b.com","password":"'$PW'"}' >/dev/null
# owner A tenta remover membro de A que não existe em B: usar MID (de A) a partir de B
[[ $(code -b /tmp/owner2 -X DELETE $API/members/$MID) == 404 ]]&&pass "tenant B não enxerga membro de A (404)"||fail "vazou entre tenants!"
[[ $(code -b /tmp/owner2 -X PATCH $API/members/$MID/role -d '{"role":"member"}') == 404 ]]&&pass "tenant B não altera membro de A (404)"||fail "alterou entre tenants!"

echo; echo "✅ E2E RBAC PASSOU — papéis, permissões, convites, anti-escalonamento, último-owner, isolamento."
