#!/usr/bin/env python3
"""Set environment variables on a CapRover app, from deploy/secrets.env.

    python3 deploy/setenv.py api FILE_ENC_KEYS
    python3 deploy/setenv.py api worker -- FILE_ENC_KEYS
    python3 deploy/setenv.py --ver backup            # shows what is there (values masked)

MERGES, does not replace. The CapRover API takes the app's COMPLETE list of variables and writes it
over the one that was there: sending only the new variable would wipe the database, the storage
credentials and everything else. So the app definition is read, the requested change is applied,
and the whole set is sent back.

Only variables that exist in secrets.env are passed, one at a time by name — never the whole file.
What stays on the local machine stays there: the backup private key (BACKUP_AGE_IDENTITY) lives in
that file and has no business going to the server.
"""

import json
import sys

from deploy import Captain, env  # same directory: reuses the client and the secrets reader

# Never uploaded, even when requested by name. The age private key is the only way to restore a
# backup: putting it on the same server that writes the backups defeats the point of asymmetric
# encryption.
NEVER = {"BACKUP_AGE_IDENTITY", "CAPROVER_PASSWORD", "GITHUB_TOKEN"}


def tapar(valor: str) -> str:
    return valor[:6] + "…" if len(valor) > 8 else "…"


def definicao(cap: Captain, app: str) -> dict:
    for a in cap._pedir("/api/v2/user/apps/appDefinitions")["data"]["appDefinitions"]:
        if a["appName"] == app:
            return a
    raise SystemExit(f"unknown app: {app}")


def ver(cap: Captain, app: str) -> None:
    print(f"\n{app}")
    for v in definicao(cap, app).get("envVars", []):
        print(f"  {v['key']}={tapar(v['value'])}")


def put(cap: Captain, app: str, names: list[str], segredos: dict[str, str]) -> None:
    d = definicao(cap, app)
    atuais = {v["key"]: v["value"] for v in d.get("envVars", [])}

    mudou = []
    for name in names:
        if name in NEVER:
            raise SystemExit(f"{name} is never uploaded to the server — see the NEVER list in this file")
        if name not in segredos:
            raise SystemExit(f"{name} is not in deploy/secrets.env")
        if atuais.get(name) == segredos[name]:
            print(f"  {app}: {name} is already up to date")
            continue
        atuais[name] = segredos[name]
        mudou.append(name)

    if not mudou:
        return

    # The whole definition is sent: CapRover replaces what is there with what it receives.
    cap._pedir("/api/v2/user/apps/appDefinitions/update", {
        "appName": app,
        "instanceCount": d.get("instanceCount", 1),
        "captainDefinitionRelativeFilePath": d.get("captainDefinitionRelativeFilePath", "./captain-definition"),
        "notExposeAsWebApp": d.get("notExposeAsWebApp", False),
        "forceSsl": d.get("forceSsl", False),
        "websocketSupport": d.get("websocketSupport", False),
        "containerHttpPort": d.get("containerHttpPort", 80),
        "description": d.get("description", ""),
        "envVars": [{"key": k, "value": v} for k, v in atuais.items()],
        "volumes": d.get("volumes", []),
        "ports": d.get("ports", []),
        "nodeId": d.get("nodeId", ""),
        "appPushWebhook": d.get("appPushWebhook", {}),
        "preDeployFunction": d.get("preDeployFunction", ""),
        "serviceUpdateOverride": d.get("serviceUpdateOverride", ""),
        "tags": d.get("tags", []),
        "redirectDomain": d.get("redirectDomain", ""),
        "httpAuth": d.get("httpAuth", {}),
        "appDeployTokenConfig": d.get("appDeployTokenConfig", {}),
    })
    print(f"  {app}: {', '.join(mudou)} — the app restarts")


def main() -> int:
    args = sys.argv[1:]
    if not args:
        print(__doc__)
        return 1

    cap = Captain()

    if "--ver" in args:
        for app in [a for a in args if a != "--ver"]:
            ver(cap, app)
        return 0

    if "--" in args:
        i = args.index("--")
        apps, names = args[:i], args[i + 1:]
    else:
        apps, names = args[:1], args[1:]

    segredos = env()
    for app in apps:
        put(cap, app, names, segredos)
    return 0


if __name__ == "__main__":
    sys.exit(main())
