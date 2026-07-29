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
    "Mail — sending", não "smtp-465"."""
    base = {
        "type": "http", "name": name, "interval": 60, "retryInterval": 60, "resendInterval": 0,
        "maxretries": 2, "timeout": 30, "method": "GET", "accepted_statuscodes": ["200-299"],
        "active": True, "ignoreTls": False, "upsideDown": False, "maxredirects": 10,
        "expiryNotification": False, "notificationIDList": {}, "packetSize": 56,
    }
    return {**base, **kw}


# Nomes e textos da página em INGLÊS: é uma página pública, lida por clientes de vários países e
# citada em conversas com auditores. (A moldura do Kuma — "All Systems Operational", "Last
# Updated" — segue o idioma do browser de quem visita; o 1.x não tem como fixá-la do servidor.)
def monitors(ai_key: str) -> list[dict]:
    return [
        monitor("Application", url="https://app.beskos.com/",
                description="The web application: mail, files, calendar and the rest."),
        # /api/readyz e não /healthz: o primeiro toca na base de dados. E tem de ser dentro de
        # /api — o proxy público só encaminha esse prefixo para a API; na raiz mediríamos o
        # servidor de ficheiros estáticos e diríamos "no ar" com a base em baixo.
        monitor("API", url="https://app.beskos.com/api/readyz",
                description="Our API, including the database connection."),
        monitor("Mail — sending (SMTP)", type="port", hostname="mail.beskos.com", port=465,
                description="Outbound mail server."),
        monitor("Mail — access (IMAP)", type="port", hostname="mail.beskos.com", port=993,
                description="Mailbox access over IMAP."),
        # 401 é a resposta CERTA de um endpoint que exige autenticação: prova que o servidor de
        # correio respondeu. Aceitar só 2xx deixaria isto vermelho para sempre.
        monitor("Mail — access (JMAP)", url="https://app.beskos.com/.well-known/jmap",
                accepted_statuscodes=["401"], description="JMAP session, which is what the webmail uses."),
        monitor("Documents", type="keyword", keyword="true",
                url="https://docs.beskos.com/healthcheck",
                description="Word, Excel and PowerPoint editing."),
        monitor("Meetings", type="keyword", keyword="OK", url="https://meet.beskos.com/",
                description="Video call server."),
        monitor("Assistant (AI)", url="https://api.tensorx.ai/v1/models",
                headers=f'{{"Authorization": "Bearer {ai_key}"}}',
                description="Models served in the EU, used by the assistant."),
        monitor("Operator console (internal)", url="https://manager.beskos.com/login",
                description="Internal management panel."),
        monitor("Website", url="https://beskos.com/", description="beskos.com"),
    ]


# Medidos, mas fora da página pública: listar um painel interno é dizer ao mundo que existe e onde.
PRIVATE = {"Operator console (internal)"}


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

    call("addStatusPage", "Beskos Status", SLUG)
    mons = [{"id": int(i), "name": m["name"]} for i, m in sorted(known.items(), key=lambda x: int(x[0]))
            if m["name"] not in PRIVATE]
    config = {
        "slug": SLUG, "title": "Beskos Status",
        "description": "Status of the Beskos services, measured from the outside, every minute. "
                       "Incidents are published here.",
        "icon": "/icon.svg", "theme": "auto", "published": True, "showTags": False,
        "domainNameList": ["status.beskos.com"], "customCSS": "",
        "footerText": "Beskos · Open Solutions", "showPoweredBy": False,
        "googleAnalyticsId": None, "showCertificateExpiry": False,
    }
    # imgDataUrl TEM de ser string: com None o servidor rebenta em `startsWith`.
    r = call("saveStatusPage", SLUG, config, icon, [{"name": "Services", "weight": 1, "monitorList": mons}])
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
