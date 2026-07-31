#!/usr/bin/env python3
"""Publica no CapRover sem passar pelo GitHub Actions.

Envia o código para o CapRover, que constrói no servidor. Serve como via alternativa quando o
Actions está indisponível — foi escrito quando a conta do GitHub ficou bloqueada por faturação e
seis builds foram cancelados de uma vez — e como forma de publicar sem esperar pela fila.

    python3 deploy/deploy.py landing            # constrói NO SERVIDOR (recomendado)
    python3 deploy/deploy.py api worker         # várias
    python3 deploy/deploy.py --todas
    python3 deploy/deploy.py --local landing    # constrói NESTA máquina e envia a imagem
    python3 deploy/deploy.py --em vps3 landing  # constrói NOUTRO VPS e envia a imagem

Onde construir:

  **No servidor** (por omissão) — envia o código, o CapRover constrói. Rápido, porque é a mesma
  arquitetura de destino (amd64), e não gasta a sua ligação. O custo é ocupar o VPS-1 durante o
  build, que é a máquina que também corre o correio.

  **Nesta máquina** (`--local`) — poupa o servidor, mas num Mac com Apple Silicon a imagem tem de
  ser cruzada para amd64 por emulação, o que é várias vezes mais lento, e depois há uma imagem
  inteira a subir pela sua ligação. Vale a pena quando não se quer carga no servidor.

  **Noutro VPS** (`--em vps2|vps3|<ip>`) — o melhor dos dois: arquitetura nativa, ligação de
  centro de dados, e o VPS-1 fica livre. É o Docker do outro VPS que constrói, por SSH.

  Porquê isto e não uma opção do CapRover: o CapRover constrói SEMPRE no líder (VPS-1), porque é lá
  que corre o contentor do captain, o único com acesso ao Docker. Os outros nós só executam.
  Construir noutro sítio é falar com o Docker desse nó diretamente.

  Requer que a chave esteja autorizada nesse VPS:
      ssh-copy-id -i ~/.ssh/beskos_deploy.pub root@<ip>

As apps que partilham a MESMA imagem (api, worker, manager-api) são construídas uma só vez: a
primeira constrói, as outras recebem a imagem que saiu dela. Construir três vezes o mesmo código
seria desperdiçar minutos do servidor que corre o correio dos clientes.

Credenciais em deploy/secrets.env (CAPROVER_URL, CAPROVER_PASSWORD).
"""

import json
import os
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
    "swprobe": "backend",
    "blobgw": "backend",
    "stalwart-cfg": "backend",
    "app": "frontend",
    "manager": "manager",
    "landing": "landing",
    "docs": "deploy/onlyoffice",
    "mail": "deploy/mailweb",
    "backup": "deploy/backup",
}

# What does NOT travel to the server. Build artefacts, and the LOCAL environment files: this tar is
# of the working directory, not of the git tree, so a gitignored `.env.local` rides along anyway --
# and Vite reads it in EVERY mode, production included. That is how the dev
# VITE_PUBLIC_BASE=http://localhost:5173 ended up inside the production bundle, pointing every
# shareable link at the machine that ran the deploy. `.env.example` stays: it holds no values.
EXCLUIR = ["node_modules", ".next", ".git", "dist", "target", "tsconfig.tsbuildinfo", ".turbo",
           ".env", ".env.local", ".env.tauri", ".env.development", ".env.production",
           ".env.development.local", ".env.production.local"]

# Máquinas de build alternativas. O VPS-1 é o líder e corre o correio e a base de dados — é o que
# se quer poupar. O VPS-3 (média) é o mais folgado: só trabalha durante reuniões.
MAQUINAS = {"vps2": "185.232.71.20", "vps3": "185.232.69.0"}


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


def construir_fora(nome: str, contexto: str, etiqueta: str, em: str | None) -> str:
    """Constrói e envia para o registo. Devolve o nome da imagem.

    `em` vazio = nesta máquina; caso contrário, o Docker do VPS indicado, por SSH.

    --platform linux/amd64 é OBRIGATÓRIO quando se constrói num Mac com Apple Silicon: os
    servidores são amd64 e uma imagem arm64 rebenta no arranque com "exec format error". Num VPS
    a arquitetura já é a certa, mas indicá-la não custa nada e evita surpresas.
    """
    imagem = f"ghcr.io/denerfernandes/{nome}:{etiqueta}"
    ambiente = dict(os.environ)
    onde = "nesta máquina, cruzada para amd64"
    if em:
        anfitriao = MAQUINAS.get(em, em)
        # Confirmar o SSH primeiro: sem isto, a falha aparece como um erro do `docker login`, que
        # não diz nada a quem o lê às duas da manhã.
        alcance = subprocess.run(["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15",
                                  f"root@{anfitriao}", "docker version --format {{.Server.Version}}"],
                                 capture_output=True, text=True)
        if alcance.returncode != 0:
            raise SystemExit(
                f"\n  Não consigo falar com o Docker de {anfitriao}:\n"
                f"    {alcance.stderr.strip().splitlines()[-1] if alcance.stderr.strip() else 'sem resposta'}\n\n"
                f"  Autorize a chave nesse VPS:\n"
                f"    ssh-copy-id -i ~/.ssh/beskos_deploy.pub root@{anfitriao}\n")
        ambiente["DOCKER_HOST"] = f"ssh://root@{anfitriao}"
        onde = f"em {anfitriao} (Docker {alcance.stdout.strip()}), nativa"

    token = subprocess.run(["gh", "auth", "token"], capture_output=True, text=True, check=True).stdout.strip()
    subprocess.run(["docker", "login", "ghcr.io", "-u", "denerFernandes", "--password-stdin"],
                   input=token, text=True, check=True, capture_output=True, env=ambiente)
    print(f"  a construir {imagem} ({onde})")
    subprocess.run(["docker", "buildx", "build", "--platform", "linux/amd64",
                    "-t", imagem, "--push", str(RAIZ / contexto)], check=True, env=ambiente)
    return imagem


def empacotar(contexto: str) -> pathlib.Path:
    destino = pathlib.Path(tempfile.mkdtemp()) / "fonte.tar.gz"
    cmd = ["tar"] + [f"--exclude={p}" for p in EXCLUIR] + ["-czf", str(destino), "-C", str(RAIZ / contexto), "."]
    subprocess.run(cmd, check=True)
    return destino


def publicar(apps: list[str], local: bool = False, em: str | None = None) -> int:
    cap = Captain()
    feitos: dict[str, str] = {}          # contexto → imagem já construída
    erros = 0
    etiqueta = subprocess.run(["git", "rev-parse", "--short", "HEAD"], capture_output=True,
                              text=True, cwd=RAIZ).stdout.strip() or "manual"
    for app in apps:
        contexto = APPS[app]
        print(f"\n{app} ({contexto})")
        if contexto in feitos:
            cap.enviar_imagem(app, feitos[contexto])
            if not cap.esperar(app):
                erros += 1
            continue
        if local or em:
            nome = "beskos-" + ("api" if contexto == "backend" else contexto.split("/")[-1])
            imagem = construir_fora(nome, contexto, etiqueta, em)
            cap.enviar_imagem(app, imagem)
            if cap.esperar(app):
                feitos[contexto] = imagem
            else:
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
    local = "--local" in args
    em = None
    if "--em" in args:
        i = args.index("--em")
        if i + 1 >= len(args):
            print("--em precisa da máquina:", ", ".join(MAQUINAS), "ou um endereço"); raise SystemExit(2)
        em = args[i + 1]
        args = args[:i] + args[i + 2:]
    args = [a for a in args if a != "--local"]
    if not args:
        print("indique as apps, ou --todas"); raise SystemExit(2)
    escolhidas = list(APPS) if args[0] == "--todas" else args
    desconhecidas = [a for a in escolhidas if a not in APPS]
    if desconhecidas:
        print("app desconhecida:", ", ".join(desconhecidas), "\napps:", ", ".join(APPS))
        raise SystemExit(2)
    raise SystemExit(1 if publicar(escolhidas, local=local, em=em) else 0)
