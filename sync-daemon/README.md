# mailin-sync — motor de sincronização de ficheiros

Biblioteca Rust com o motor de sincronização do Beskos. **Não é uma aplicação**: quem o hospeda é a
app desktop (`Beskos.app`), que lhe dá a janela, o ícone na barra e a UI. Assim há só uma app.

## Modelo: pares de pastas (não espelha o Drive inteiro)

O utilizador escolhe uma **pasta local**; é criada a pasta correspondente no **Drive**; a partir daí
só o que está lá dentro sincroniza, nos dois sentidos. Podem existir **várias** pastas assim.
Ficheiros noutras zonas do Drive são simplesmente ignorados.

```
~/Projetos   ⇄   Drive/Projetos      (par 1)
~/Fotos      ⇄   Drive/Fotos         (par 2)
outros ficheiros do Drive            → não sincronizam
```

Na app: **Definições › Sincronização** (adicionar/parar pastas, estado, pausa, limite de banda) e o
botão **"Sincronizar pasta…"** no próprio Drive, onde as pastas sincronizadas aparecem destacadas.

## Como funciona cada passagem

**Push antes de pull** (ordem deliberada): uma edição local sobe antes de o download a poder
sobrescrever.

1. **push** (por par): sobe alterados com `If-Match` (o hash base); cria os novos **dentro da pasta
   do par**, criando as subpastas em falta na nuvem. Se o servidor mudou entretanto → **412** e
   guarda-se uma **cópia de conflito** local (nada se perde).
2. **pull**: aplica o feed de mudanças; cada ficheiro é roteado para o par a que pertence (ou
   ignorado). Se só mudou de pasta, **move** o ficheiro local em vez de o rebaixar.

## Segurança e robustez

- **Anti-traversal**: nomes vindos do servidor são sanitizados e o destino é verificado dentro da
  pasta do par (com testes).
- **Nunca apaga o que não conhece**: um *tombstone* só remove ficheiros que este motor baixou.
- **Apagar local → LIXEIRA do servidor** (recuperável), nunca *hard delete*. **Mover local → move**
  no servidor (detetado por conteúdo: mesmo hash noutro caminho ⇒ é um move, não apagar+criar).
- **Guarda anti-apagão**: se >50 ficheiros sumirem de uma vez, assume pasta indisponível e **não
  propaga nada**.
- **Escrita atómica** (`.part` + rename) e **HTTPS obrigatório** fora de localhost; token em ficheiro `0600`.

## Desempenho

- **Watcher** do sistema de ficheiros (FSEvents/inotify) por pasta, com eventos coalescidos; o
  intervalo é só rede de segurança.
- **Pré-filtro mtime+tamanho**: só lê/hasheia quando o carimbo muda.
- **Transferências em paralelo** (`workers`, default 4) e **limite de banda** global (`bandwidth_kbps`).
- **Upload delta**: em ficheiros >8 MiB envia só os blocos alterados (o servidor reconstrói); se
  mudou >70% ou o servidor não suportar, envia inteiro.
- **Reconciliação horária** por varredura, para apanhar purgas (hard delete) que o feed não reporta.

## Config

`~/Library/Application Support/mailin-sync` (macOS) ou `~/.config/mailin-sync` (Linux):
`config.json` (servidor + pares + workers + banda), `token` (0600), `index.db` (cache SQLite).

## Por fazer

- **M3 mobile**: depende da app mobile (bloqueada em push FCM/APNs e no spike do WebRTC iOS); no
  iOS um espelho contínuo exigiria uma File Provider Extension nativa.
