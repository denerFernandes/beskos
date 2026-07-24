# Fase 1 · Provisionar as 3 VPS — runbook

Automação idempotente. Ver a arquitetura em [../../docs/DEPLOY.md](../../docs/DEPLOY.md).

## Ordem de operações

### Passo 0 — chave SSH de deploy (na tua máquina, uma vez)
```bash
ssh-keygen -t ed25519 -f ~/.ssh/beskos_deploy -C "beskos-deploy" -N ""
cat ~/.ssh/beskos_deploy.pub      # → é esta a SSH_PUBKEY dos scripts
```

### Passo 1 — formatar/reinstalar o SO das 3 VPS (painel Netcup)
Faz-se pelo **painel SCP**, não pela API (a API SOAP não reinstala; a REST está em beta; o "hack" por
scraping só serve imagens custom — não compensa). Para cada VPS:
**SCP → Media → Images → Ubuntu 24.04 LTS → Install** (apaga tudo, deixa limpo).
A Netcup mostra a **password root inicial** no painel — guarda-a (é o acesso da 1ª ligação).
Anota o **IPv4 público** de cada uma → VPS-1 (dados), VPS-2 (app), VPS-3 (média).

> Nota: a Netcup **não cria VPS por API** (não há API de provisionamento como no Hetzner Cloud);
> reinstala-se as 3 existentes. Reinstalar = formatar.

### Passo 2 — hardening + Docker (cada nó)
IPs reais (a chave SSH foi injetada na instalação → 1ª ligação já é por chave, como root):

| IP | ROLE | Nó |
|----|------|----|
| 185.232.71.70 ⭐ | `data` | dados + CapRover leader + mail (recebe os domínios) |
| 185.232.71.20 | `app` | app + réplica Postgres |
| 185.232.69.0 | `media` | OnlyOffice + LiveKit + Egress |

```bash
KEY=~/.ssh/beskos_deploy                 # ajustar ao caminho da tua chave privada
PUB="$(cat $KEY.pub)"
IPS="185.232.71.70 185.232.71.20 185.232.69.0"

ssh -i $KEY root@185.232.71.70 "ROLE=data  ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
ssh -i $KEY root@185.232.71.20 "ROLE=app   ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
ssh -i $KEY root@185.232.69.0  "ROLE=media ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
```
No fim: root e password ficam **desligados**. A partir daqui liga-se por `ssh -i $KEY deploy@<IP>`.

### Passo 3 — CapRover no leader (VPS-1) — já é Fase 2
```bash
scp -i ~/.ssh/beskos_deploy caprover-leader.sh deploy@<VPS1_IP>:~
ssh -i ~/.ssh/beskos_deploy deploy@<VPS1_IP> "LEADER_IP=<VPS1_IP> bash caprover-leader.sh"
```
Segue as instruções que o script imprime (painel por túnel, root domain, HTTPS, juntar nós, labels, rede overlay).

## O que os scripts fazem
- **`setup-node.sh`** — utilizador sudo + chave · SSH só-chave (sem root/password) · unattended-upgrades ·
  fail2ban · Docker CE + limites de log · sysctl · **ufw por papel** (data/app/media).
- **`caprover-leader.sh`** — instala o CapRover (manager do Swarm) no VPS-1.

## O que preciso de ti para EU executar (senão executas tu, é copy-paste)
1. **IPv4** das 3 VPS (e qual é qual: dados/app/média).
2. Confirmar que a **porta 25** está desbloqueada na Netcup (senão abrir pedido — só afeta a Fase 6, mail).
3. Acesso: ou (a) pões a minha chave / uma chave nas VPS e dás-me os IPs para eu correr por SSH daqui,
   ou (b) corres tu os 3 comandos do Passo 2 e dizes-me quando estiver.
4. *(Opcional)* credenciais da **API Netcup** se quiseres que eu automatize a reinstalação + PTR.

> ⚠️ O Passo 1 **apaga** o conteúdo das VPS. Só avanço com isso após confirmares explicitamente.
