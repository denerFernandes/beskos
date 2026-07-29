#!/usr/bin/env python3
"""Publica no CapRover sem passar pelo GitHub Actions.

Envia o código para o CapRover, que constrói no servidor. Serve como via alternativa quando o
Actions está indisponível — foi escrito quando a conta do GitHub ficou bloqueada por faturação e
seis builds foram cancelados de uma vez — e como forma de publicar sem esperar pela fila.

    python3 deploy/deploy.py landing          # uma app
    python3 deploy/deploy.py api worker       # várias
    python3 deploy/deploy.py --todas

As apps que partilham a MESMA imagem (api, worker, manager-api) são construídas uma só vez: a
primeira constrói, as outras recebem a imagem que saiu dela. Construir três vezes o mesmo código
seria desperdiçar minutos do servidor que corre o correio dos clientes.

Credenciais em deploy/secrets.env (CAPROVER_URL, CAPROVER_PASSWORD).
"""

import json
import pathlib
import re
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid

RAIZ = pathlib.Path(__file__).resolve().parents[1]

# app → pasta que é o contexto de build. As três primeiras partilham a imagem do backend.
APPS = {
    "api": "backend",
    "worker": "backend",
    "manager-api": "backend",
    "app": "frontend",
    "manager": "manager",
    "landing": "landing",
    "docs": "deploy/onlyoffice",
    "mail": "deploy/mailweb",
    "backup": "deploy/backup",
}

EXCLUIR = ["node_modules", ".next", ".git", "dist", "target", "tsconfig.tsbuildinfo", ".turbo"]


def env() -> dict[str, str]:
    texto = (RAIZ / "deploy" / "secrets.env").read_text()
    return {k: v.strip() for k, v in re.findall(r"^([A-Z_0-9]+)=(.*)$", texto, re.M)}


class Captain:
    def __init__(self) -> None:
        e = env()
        self.base = e["CAPROVER_URL"].rstrip("/")
        self.token = self._pedir("/api/v2/login", {"password": e["CAPROVER_PASSWORD"]})["data"]["token"]

    def _pedir(self, caminho, corpo=None, method=None, timeout=120):
        req = urllib.request.Request(
            self.base + caminho,
            method=method or ("POST" if corpo else "GET"),
            data=json.dumps(corpo).encode() if corpo else None,
            headers={
                "Content-Type": "application/json",
                "x-namespace": "captain",
                **({"x-captain-auth": self.token} if hasattr(self, "token") else {}),
            },
        )
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return json.loads(r.read())

    def enviar_fonte(self, app: str, tar: pathlib.Path) -> None:
        """Envia o tarball; o CapRover constrói a partir do captain-definition."""
        fronteira = "----beskos" + uuid.uuid4().hex
        corpo = (
            f'--{fronteira}\r\nContent-Disposition: form-data; name="sourceFile"; '
            f'filename="{app}.tar.gz"\r\nContent-Type: application/gzip\r\n\r\n'
        ).encode() + tar.read_bytes() + f"\r\n--{fronteira}--\r\n".encode()
        req = urllib.request.Request(
            f"{self.base}/api/v2/user/apps/appData/{app}?detached=1",
            data=corpo, method="POST",
            headers={"Content-Type": f"multipart/form-data; boundary={fronteira}",
                     "x-namespace": "captain", "x-captain-auth": self.token},
        )
        with urllib.request.urlopen(req, timeout=600) as r:
            print(f"  {app}: {json.loads(r.read()).get('description')}")

    def enviar_imagem(self, app: str, imagem: str) -> None:
        definicao = json.dumps({"schemaVersion": 2, "imageName": imagem})
        self._pedir(f"/api/v2/user/apps/appData/{app}", {"captainDefinitionContent": definicao, "gitHash": ""})
        print(f"  {app}: a receber {imagem}")

    def esperar(self, app: str, minutos: int = 15) -> bool:
        """Espera pelo fim do build. Devolve False se falhou — para não continuar às cegas."""
        for _ in range(minutos * 3):
            time.sleep(20)
            try:
                d = self._pedir(f"/api/v2/user/apps/appData/{app}")["data"]
            except Exception:
                continue
            if not d.get("isAppBuilding"):
                if d.get("isBuildFailed"):
                    linhas = (d.get("logs") or {}).get("lines") or []
                    print(f"  {app}: FALHOU\n" + "\n".join(l.strip() for l in linhas[-12:]))
                    return False
                print(f"  {app}: construído")
                return True
        print(f"  {app}: sem resposta ao fim de {minutos} min")
        return False

    def imagem_atual(self, app: str) -> str | None:
        apps = self._pedir("/api/v2/user/apps/appDefinitions")["data"]["appDefinitions"]
        a = next((x for x in apps if x["appName"] == app), None)
        versoes = (a or {}).get("versions") or []
        return versoes[-1].get("deployedImageName") if versoes else None


def empacotar(contexto: str) -> pathlib.Path:
    destino = pathlib.Path(tempfile.mkdtemp()) / "fonte.tar.gz"
    cmd = ["tar"] + [f"--exclude={p}" for p in EXCLUIR] + ["-czf", str(destino), "-C", str(RAIZ / contexto), "."]
    subprocess.run(cmd, check=True)
    return destino


def publicar(apps: list[str]) -> int:
    cap = Captain()
    feitos: dict[str, str] = {}          # contexto → imagem já construída
    erros = 0
    for app in apps:
        contexto = APPS[app]
        print(f"\n{app} ({contexto})")
        if contexto in feitos:
            cap.enviar_imagem(app, feitos[contexto])
            if not cap.esperar(app):
                erros += 1
            continue
        cap.enviar_fonte(app, empacotar(contexto))
        if not cap.esperar(app):
            erros += 1
            continue
        imagem = cap.imagem_atual(app)
        if imagem:
            feitos[contexto] = imagem
    return erros


if __name__ == "__main__":
    args = sys.argv[1:]
    if not args or args[0] in ("-h", "--help"):
        print(__doc__)
        print("apps:", ", ".join(APPS))
        raise SystemExit(0)
    escolhidas = list(APPS) if args[0] == "--todas" else args
    desconhecidas = [a for a in escolhidas if a not in APPS]
    if desconhecidas:
        print("app desconhecida:", ", ".join(desconhecidas), "\napps:", ", ".join(APPS))
        raise SystemExit(2)
    raise SystemExit(1 if publicar(escolhidas) else 0)
