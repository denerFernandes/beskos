#!/usr/bin/env python3
"""Configura a página de estado (status.beskos.com) — sondas e página pública.

O Uptime Kuma só se configura pela interface web, e dez sondas feitas à mão são dez oportunidades
de erro que não ficam registadas em lado nenhum. Este script é a fonte de verdade: correr de novo
não duplica nada (salta o que já existe) e serve de documentação do que medimos e porquê.

    pip install "python-socketio[client]"
    python3 deploy/status/kuma-setup.py

Lê STATUS_ADMIN_USER/STATUS_ADMIN_PASSWORD e AI_API_KEY de deploy/secrets.env.
Na primeira execução cria a conta de administração; nas seguintes só entra.
"""

import base64
import pathlib
import re
import sys
import time

import socketio

URL = "https://status.beskos.com"
ROOT = pathlib.Path(__file__).resolve().parents[2]
SLUG = "beskos"


def load_env() -> dict[str, str]:
    text = (ROOT / "deploy" / "secrets.env").read_text()
    return {k: v.strip() for k, v in re.findall(r"^([A-Z_0-9]+)=(.*)$", text, re.M)}


def monitor(name: str, **kw) -> dict:
    """Sonda com os valores por omissão. Nomes voltados para quem lê a página, que é pública:
    "Correio — envio", não "smtp-465"."""
    base = {
        "type": "http", "name": name, "interval": 60, "retryInterval": 60, "resendInterval": 0,
        "maxretries": 2, "timeout": 30, "method": "GET", "accepted_statuscodes": ["200-299"],
        "active": True, "ignoreTls": False, "upsideDown": False, "maxredirects": 10,
        "expiryNotification": False, "notificationIDList": {}, "packetSize": 56,
    }
    return {**base, **kw}


def monitors(ai_key: str) -> list[dict]:
    return [
        monitor("Aplicação", url="https://app.beskos.com/",
                description="A aplicação web: correio, ficheiros, calendário e o resto."),
        # /api/readyz e não /healthz: o primeiro toca na base de dados. E tem de ser dentro de
        # /api — o proxy público só encaminha esse prefixo para a API; na raiz mediríamos o
        # servidor de ficheiros estáticos e diríamos "no ar" com a base em baixo.
        monitor("API", url="https://app.beskos.com/api/readyz",
                description="A nossa API, incluindo a ligação à base de dados."),
        monitor("Correio — envio (SMTP)", type="port", hostname="mail.beskos.com", port=465,
                description="Servidor de envio."),
        monitor("Correio — leitura (IMAP)", type="port", hostname="mail.beskos.com", port=993,
                description="Acesso às caixas por IMAP."),
        # 401 é a resposta CERTA de um endpoint que exige autenticação: prova que o servidor de
        # correio respondeu. Aceitar só 2xx deixaria isto vermelho para sempre.
        monitor("Correio — leitura (JMAP)", url="https://app.beskos.com/.well-known/jmap",
                accepted_statuscodes=["401"], description="Sessão JMAP, que é o que o webmail usa."),
        monitor("Documentos", type="keyword", keyword="true",
                url="https://docs.beskos.com/healthcheck",
                description="Edição de Word, Excel e PowerPoint."),
        monitor("Reuniões", type="keyword", keyword="OK", url="https://meet.beskos.com/",
                description="Servidor de videochamada."),
        monitor("Assistente (IA)", url="https://api.tensorx.ai/v1/models",
                headers=f'{{"Authorization": "Bearer {ai_key}"}}',
                description="Modelos servidos na UE, usados pelo assistente."),
        monitor("Consola do operador", url="https://manager.beskos.com/login",
                description="Painel interno de gestão."),
        monitor("Site", url="https://beskos.com/", description="beskos.com"),
    ]


def main() -> int:
    env = load_env()
    user, password = env["STATUS_ADMIN_USER"], env["STATUS_ADMIN_PASSWORD"]

    sio = socketio.Client(request_timeout=30)
    known: dict[str, dict] = {}
    sio.on("monitorList", lambda d: known.update(d or {}))
    sio.connect(URL, transports=["websocket"], wait_timeout=30)
    call = lambda ev, *a: sio.call(ev, a if len(a) != 1 else a[0], timeout=60)

    r = call("setup", user, password)
    print("conta:", (r or {}).get("msg", "já existia"))
    r = call("login", {"username": user, "password": password, "token": ""})
    if not (r or {}).get("ok"):
        print("login falhou:", r, file=sys.stderr)
        return 1
    time.sleep(2)

    existing = {m["name"] for m in known.values()}
    for m in monitors(env.get("AI_API_KEY", "")):
        if m["name"] in existing:
            print(f'{m["name"]:28} já existe')
            continue
        print(f'{m["name"]:28} {call("add", m).get("msg")}')

    # Logo: o Kuma só aceita PNG (rejeita SVG com "Only allowed PNG logo").
    #   rsvg-convert -w 256 -h 256 frontend/public/beskos-mark.svg -o /tmp/mark.png
    png = ROOT / "deploy" / "status" / "mark.png"
    icon = ("data:image/png;base64," + base64.b64encode(png.read_bytes()).decode()) if png.exists() else "/icon.svg"

    call("addStatusPage", "Estado do Beskos", SLUG)
    mons = [{"id": int(i), "name": m["name"]} for i, m in sorted(known.items(), key=lambda x: int(x[0]))]
    config = {
        "slug": SLUG, "title": "Estado do Beskos",
        "description": "Estado dos serviços do Beskos, medido de fora, a cada minuto. "
                       "Os incidentes são publicados aqui.",
        "icon": "/icon.svg", "theme": "auto", "published": True, "showTags": False,
        "domainNameList": ["status.beskos.com"], "customCSS": "",
        "footerText": "Beskos · Open Solutions, Poznań", "showPoweredBy": False,
        "googleAnalyticsId": None, "showCertificateExpiry": False,
    }
    # imgDataUrl TEM de ser string: com None o servidor rebenta em `startsWith`.
    r = call("saveStatusPage", SLUG, config, icon, [{"name": "Serviços", "weight": 1, "monitorList": mons}])
    print("página pública:", "ok" if (r or {}).get("ok") else r)

    # A raiz mostra a página pública, não o ecrã de entrada: quem escreve status.beskos.com
    # quer ver o estado.
    print("entrada:", call("setSettings", {"entryPage": f"statusPage-{SLUG}", "checkUpdate": False,
                                           "checkBeta": False, "searchEngineIndex": True,
                                           "trustProxy": True}, password))
    sio.disconnect()
    return 0


if __name__ == "__main__":
    sys.exit(main())
