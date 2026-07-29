# Página de estado — status.beskos.com

Pública, sem autenticação: <https://status.beskos.com>. É a medição que sustenta a promessa de
disponibilidade — sem um número que o cliente possa ver, "99,5%" é a nossa palavra contra a memória
dele. Serve também os avisos de subprocessadores (é uma lista de subscrição só).

## O que está montado

**Uptime Kuma** (`louislam/uptime-kuma:1`), app `status` no CapRover:

| Definição | Valor | Porquê |
|---|---|---|
| Porta | 3001 | É a do Kuma; sem isto o CapRover encaminha para a 80 e dá 502 |
| Volume | `status-data` → `/app/data` | O histórico vive num SQLite. Sem volume, cada deploy apaga os 90 dias que a página existe para mostrar |
| WebSocket | ligado | O Kuma fala socket.io; sem isto o handshake dá 400 e nem a interface abre |
| TLS | certificado do subdomínio, `forceSsl` | `beskos.com` é o domínio raiz do CapRover, por isso `status.beskos.com` não é "domínio personalizado" — usa-se `enablebasedomainssl` |
| DNS | A → 185.232.71.70, **sem proxy** | A validação Let's Encrypt do CapRover é HTTP-01 e tem de chegar à origem |

**Subdomínio e não `/status`:** se o ingress principal cair, um `/status` servido do mesmo sítio cai
com ele — que é exatamente quando as pessoas o procuram. `beskos.com/status` redireciona para cá
(307, temporário de propósito: um 308 fica em cache no browser para sempre).

## Sondas

Dez, de minuto a minuto, do lado de fora. Definidas em [`kuma-setup.py`](kuma-setup.py) — correr o
script outra vez não duplica nada.

Duas merecem nota:

- **API** aponta a `/api/readyz`, que toca na base de dados, e não a `/healthz`. E tem de ser dentro
  de `/api`: o proxy público só encaminha esse prefixo para a API — na raiz mediríamos o servidor de
  ficheiros estáticos e diríamos "no ar" com a base em baixo.
- **Correio — leitura (JMAP)** aceita **401**. É a resposta certa de um endpoint que exige
  autenticação, e prova que o Stalwart respondeu; exigir 2xx deixaria a linha vermelha para sempre.

## Correr a configuração

```sh
pip install "python-socketio[client]"
python3 deploy/status/kuma-setup.py
```

Credenciais em `deploy/secrets.env` (`STATUS_ADMIN_USER`, `STATUS_ADMIN_PASSWORD`). O painel de
administração fica em <https://status.beskos.com/dashboard>.

## O que falta

- **Avisos** (e-mail/webhook) quando uma sonda cai — o Kuma suporta, ainda não está configurado.
- **Sondas profundas**: enviar um e-mail de teste de ponta a ponta e medir quanto demora a chegar,
  escrever e ler um objeto no armazenamento. As de hoje provam que o serviço responde, não que o
  trabalho completo funciona.
- **VPS separado.** Por agora a página corre na mesma infraestrutura que mede: numa falha total fica
  inacessível. É menos mau do que uma página a garantir que está tudo bem enquanto não está, mas é
  para trocar.
