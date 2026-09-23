#!/usr/bin/env python3
"""Configure the status page (status.beskos.com) — monitors and the public page.

Uptime Kuma is configured only through its web interface, and ten monitors made by hand are ten
chances for mistakes that are recorded nowhere. This script is the source of truth: running it
again duplicates nothing (it skips what already exists) and documents what is measured and why.

    pip install "python-socketio[client]"
    python3 deploy/status/kuma-setup.py

Reads STATUS_ADMIN_USER/STATUS_ADMIN_PASSWORD and AI_API_KEY from deploy/secrets.env.
The first run creates the admin account; later runs only log in.
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
    """Monitor with the default values. Names are written for whoever reads the page, which is
    public: "Mail — sending", not "smtp-465"."""
    base = {
        "type": "http", "name": name, "interval": 60, "retryInterval": 60, "resendInterval": 0,
        "maxretries": 2, "timeout": 30, "method": "GET", "accepted_statuscodes": ["200-299"],
        "active": True, "ignoreTls": False, "upsideDown": False, "maxredirects": 10,
        "expiryNotification": False, "notificationIDList": {}, "packetSize": 56,
    }
    return {**base, **kw}


# Page names and texts in ENGLISH: it is a public page, read by customers in many countries and
# cited in conversations with auditors. (Kuma's frame -- "All Systems Operational", "Last
# Updated" -- follows the visitor's browser language; 1.x has no way to pin it from the server.)
def monitors(ai_key: str) -> list[dict]:
    return [
        monitor("Application", url="https://app.beskos.com/",
                description="The web application: mail, files, calendar and the rest."),
        # /api/readyz and not /healthz: the former touches the database. And it has to be under
        # /api -- the public proxy only forwards that prefix to the API; at the root we would
        # measure the static file server and report "up" with the database down.
        monitor("API", url="https://app.beskos.com/api/readyz",
                description="Our API, including the database connection."),
        monitor("Mail — sending (SMTP)", type="port", hostname="mail.beskos.com", port=465,
                description="Outbound mail server."),
        monitor("Mail — access (IMAP)", type="port", hostname="mail.beskos.com", port=993,
                description="Mailbox access over IMAP."),
        # 401 is the CORRECT answer from an endpoint that requires authentication: it proves the
        # mail server responded. Accepting only 2xx would leave this red forever.
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


# Measured, but kept off the public page: listing an internal panel tells the world it exists and
# where.
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
    print("account:", (r or {}).get("msg", "already existed"))
    r = call("login", {"username": user, "password": password, "token": ""})
    if not (r or {}).get("ok"):
        print("login failed:", r, file=sys.stderr)
        return 1
    time.sleep(2)

    existing = {m["name"] for m in known.values()}
    for m in monitors(env.get("AI_API_KEY", "")):
        if m["name"] in existing:
            print(f'{m["name"]:28} already exists')
            continue
        print(f'{m["name"]:28} {call("add", m).get("msg")}')

    # Logo: Kuma only accepts PNG (it rejects SVG with "Only allowed PNG logo").
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
    # imgDataUrl MUST be a string: with None the server crashes on `startsWith`.
    r = call("saveStatusPage", SLUG, config, icon, [{"name": "Services", "weight": 1, "monitorList": mons}])
    print("public page:", "ok" if (r or {}).get("ok") else r)

    # The root shows the public page, not the login screen: whoever types status.beskos.com
    # wants to see the status.
    print("entry page:", call("setSettings", {"entryPage": f"statusPage-{SLUG}", "checkUpdate": False,
                                           "checkBeta": False, "searchEngineIndex": True,
                                           "trustProxy": True}, password))
    sio.disconnect()
    return 0


if __name__ == "__main__":
    sys.exit(main())
