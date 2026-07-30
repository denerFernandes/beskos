#!/usr/bin/env python3
"""Põe variáveis de ambiente numa app do CapRover, a partir do deploy/secrets.env.

    python3 deploy/setenv.py api FILE_ENC_KEYS
    python3 deploy/setenv.py api worker -- FILE_ENC_KEYS
    python3 deploy/setenv.py --ver backup            # mostra o que lá está (valores tapados)

FUNDE, não substitui. A API do CapRover recebe a lista COMPLETA de variáveis da app e escreve-a por
cima da que lá estava: mandar só a variável nova apagaria a base de dados, as credenciais do
armazenamento e tudo o resto. Por isso lê-se a definição da app, altera-se o que se pediu, e
devolve-se o conjunto inteiro.

Só passa variáveis que existam no secrets.env, e uma de cada vez por nome — nunca o ficheiro todo.
O que fica na sua máquina fica na sua máquina: a chave privada das cópias de segurança
(BACKUP_AGE_IDENTITY) está lá e não tem nada que ir para o servidor.
"""

import json
import sys

from deploy import Captain, env  # mesmo diretório: reaproveita o cliente e a leitura de segredos

# Nunca sobem, mesmo que sejam pedidas pelo nome. A privada do age é o único meio de restaurar uma
# cópia: pô-la no mesmo servidor que escreve as cópias anula a razão de a cifra ser assimétrica.
NUNCA = {"BACKUP_AGE_IDENTITY", "CAPROVER_PASSWORD", "GITHUB_TOKEN"}


def tapar(valor: str) -> str:
    return valor[:6] + "…" if len(valor) > 8 else "…"


def definicao(cap: Captain, app: str) -> dict:
    for a in cap._pedir("/api/v2/user/apps/appDefinitions")["data"]["appDefinitions"]:
        if a["appName"] == app:
            return a
    raise SystemExit(f"app desconhecida: {app}")


def ver(cap: Captain, app: str) -> None:
    print(f"\n{app}")
    for v in definicao(cap, app).get("envVars", []):
        print(f"  {v['key']}={tapar(v['value'])}")


def por(cap: Captain, app: str, nomes: list[str], segredos: dict[str, str]) -> None:
    d = definicao(cap, app)
    atuais = {v["key"]: v["value"] for v in d.get("envVars", [])}

    mudou = []
    for nome in nomes:
        if nome in NUNCA:
            raise SystemExit(f"{nome} não sobe para o servidor — ver a lista NUNCA neste ficheiro")
        if nome not in segredos:
            raise SystemExit(f"{nome} não está no deploy/secrets.env")
        if atuais.get(nome) == segredos[nome]:
            print(f"  {app}: {nome} já está igual")
            continue
        atuais[nome] = segredos[nome]
        mudou.append(nome)

    if not mudou:
        return

    # A definição vai inteira: o CapRover substitui o que lá está pelo que receber.
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
    print(f"  {app}: {', '.join(mudou)} — a app reinicia")


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
        apps, nomes = args[:i], args[i + 1:]
    else:
        apps, nomes = args[:1], args[1:]

    segredos = env()
    for app in apps:
        por(cap, app, nomes, segredos)
    return 0


if __name__ == "__main__":
    sys.exit(main())
