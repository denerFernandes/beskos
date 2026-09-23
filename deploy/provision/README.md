# Provisioning the three VPS — runbook

Idempotent automation. See the architecture in [../../docs/DEPLOY.md](../../docs/DEPLOY.md).

## Order of operations

### Step 0 — deploy SSH key (on the operator's machine, once)
```bash
ssh-keygen -t ed25519 -f ~/.ssh/beskos_deploy -C "beskos-deploy" -N ""
cat ~/.ssh/beskos_deploy.pub      # → this is the scripts' SSH_PUBKEY
```

### Step 1 — format/reinstall the OS on the three VPS (Netcup panel)
This is done through the **SCP panel**, not the API (the SOAP API cannot reinstall; the REST API is
in beta; the scraping "hack" only works for custom images — not worth it). For each VPS:
**SCP → Media → Images → Ubuntu 24.04 LTS → Install** (wipes everything, leaves it clean).
Netcup shows the **initial root password** in the panel — keep it (it is the access for the first
connection). Record each machine's **public IPv4** → VPS-1 (data), VPS-2 (app), VPS-3 (media).

> Note: Netcup **does not create VPS through an API** (there is no provisioning API like Hetzner
> Cloud's); the three existing machines are reinstalled. Reinstall = format.

### Step 2 — hardening + Docker (each node)
Actual IPs (the SSH key was injected at install time → the first connection is already key-based,
as root):

| IP | ROLE | Node |
|----|------|----|
| 185.232.71.70 ⭐ | `data` | data + CapRover leader + mail (receives the domains) |
| 185.232.71.20 | `app` | app + Postgres replica |
| 185.232.69.0 | `media` | OnlyOffice + LiveKit + Egress |

```bash
KEY=~/.ssh/beskos_deploy                 # adjust to the path of the private key
PUB="$(cat $KEY.pub)"
IPS="185.232.71.70 185.232.71.20 185.232.69.0"

ssh -i $KEY root@185.232.71.70 "ROLE=data  ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
ssh -i $KEY root@185.232.71.20 "ROLE=app   ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
ssh -i $KEY root@185.232.69.0  "ROLE=media ADMIN_USER=deploy NODE_IPS='$IPS' SSH_PUBKEY='$PUB' bash -s" < setup-node.sh
```
At the end, root and password login are **disabled**. From here on, connect with
`ssh -i $KEY deploy@<IP>`.

### Step 3 — CapRover on the leader (VPS-1)
```bash
scp -i ~/.ssh/beskos_deploy caprover-leader.sh deploy@<VPS1_IP>:~
ssh -i ~/.ssh/beskos_deploy deploy@<VPS1_IP> "LEADER_IP=<VPS1_IP> bash caprover-leader.sh"
```
Follow the instructions the script prints (panel via tunnel, root domain, HTTPS, joining nodes,
labels, overlay network).

## What the scripts do
- **`setup-node.sh`** — sudo user + key · key-only SSH (no root/password) · unattended-upgrades ·
  fail2ban · Docker CE + log limits · sysctl · **ufw per role** (data/app/media).
- **`caprover-leader.sh`** — installs CapRover (Swarm manager) on VPS-1.

## Inputs required
1. The **IPv4** of the three VPS (and which is which: data/app/media).
2. Confirmation that **port 25** is unblocked at Netcup (otherwise open a request — it only affects
   mail).
3. Access: either (a) a deploy key is placed on the VPS so the Step 2 commands can be run over SSH
   remotely, or (b) the three Step 2 commands are run directly on each machine.
4. *(Optional)* **Netcup API** credentials, to automate the reinstall + PTR.

> ⚠️ Step 1 **wipes** the contents of the VPS. Proceed only after explicit confirmation.
