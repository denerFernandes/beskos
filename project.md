Brief de projeto — Plataforma de email + armazenamento (alternativa ao Google Workspace)


Cole este documento inteiro como prompt inicial num agente de código (Claude Code, Cursor, etc.).
Ele descreve o produto, as restrições inegociáveis, a arquitetura e o plano de fases.
Leia a seção "Como você deve trabalhar" antes de escrever qualquer código.




1. O que estamos construindo

Uma plataforma multi-tenant de email e armazenamento para vender como produto (SaaS), self-hosted em VPS próprio. Cada cliente (tenant) é uma organização com seu próprio domínio, seus usuários e suas caixas de email.

O motor de email é o Stalwart — um servidor all-in-one (SMTP/IMAP/JMAP/DKIM/spam/storage) que usamos sem modificar, apenas pela API. Nós construímos por cima dele um control plane (backend Go) e uma interface própria (frontend React). Toda a diferenciação, a experiência do usuário e a lógica de negócio são nossas.

Escopo da primeira versão: email (enviar/receber/ler) + armazenamento de arquivos + o painel de gestão multi-tenant que provisiona e cobra os clientes. Calendário, contatos e colaboração ficam para depois.


2. Restrições inegociáveis (não viole nenhuma)


NUNCA modifique o Stalwart. Não faça fork, não aplique patch no código-fonte, não recompile com alterações, não linke/embuta código dele no nosso binário. Nós rodamos o binário/imagem oficial e só falamos com ele pela rede (JMAP + REST Management API). Editar arquivos de configuração e rodar via Docker é permitido; alterar o código-fonte não é. Isso é uma exigência de licença (AGPL-3.0): tratar o Stalwart como serviço externo mantém todo o nosso código proprietário e fechado.
Multi-tenancy é nossa, no Go. A tenancy formal com admin-por-cliente e branding é recurso Enterprise pago do Stalwart. Nós NÃO usamos isso. Implementamos o conceito de tenant, o admin-por-cliente e o isolamento entre organizações na nossa camada Go, usando o suporte a multi-domínio do Stalwart Community (cada cliente = um ou mais domínios).
A Admin API do Stalwart é admin GLOBAL e nunca pode vazar para o cliente. Toda operação administrativa (criar domínio, mailbox, mudar quota) passa obrigatoriamente pelo nosso backend Go, que primeiro verifica se o cliente autenticado é dono do recurso e só então traduz para a chamada na API. Nenhuma credencial de admin do Stalwart chega ao frontend, nem por proxy.
Isolamento de usuário final é nativo do Stalwart — não reimplemente. Cada mailbox é um principal com autenticação própria; o JMAP só entrega o que a credencial pode ver. Não construa uma camada de autorização por cima disso para leitura de email.
Fixe a versão do Stalwart e gere o cliente da API a partir do OpenAPI daquela versão. O Stalwart é pré-1.0; endpoints e payloads mudam entre versões. Não codifique payloads de memória — gere tipos a partir do openapi.yml da versão exata que rodamos.
Entregabilidade é um problema separado do código e é o maior risco do negócio. rDNS/PTR, warm-up de IP, SPF/DKIM/DMARC, feedback loops e monitoramento de blocklists não se resolvem com código bonito. O sistema deve ter ganchos para isso (limites de envio por tenant, pools de IP de saída), mas não assuma que provisionar bem = entregar bem.



3. Stack técnica


Frontend: React (TypeScript). Webmail + painel do cliente + painel de admin interno + UI de billing e de armazenamento.
Backend (control plane): Go. API REST/JSON, workers, integração com Stalwart e Stripe.
Banco: PostgreSQL (nosso control plane + backend de dados do Stalwart podem compartilhar o Postgres, mas em schemas/instâncias separados).
Armazenamento de blobs: MinIO (S3-compatível, self-hosted) para o nosso serviço de arquivos.
Motor de email: Stalwart (oficial, sem modificação), via Docker.
Pagamentos: Stripe (ou equivalente) para assinaturas por faixa de caixas.
Infra: Docker Compose no início; deixar caminho para múltiplas instâncias Stalwart depois.



4. Arquitetura

React (TypeScript)
 ├─ Webmail            → fala JMAP DIRETO com o Stalwart (token do usuário final)
 ├─ Painel do cliente  → fala com a nossa API Go
 ├─ Painel admin       → fala com a nossa API Go
 └─ UI storage         → fala com a nossa API Go

Go (control plane — o que construímos e vendemos)
 ├─ Auth / contas / tenants / orgs
 ├─ Provisioning: cria domínios, DKIM, mailboxes chamando a Admin API do Stalwart
 ├─ Billing (Stripe): assinaturas, faturamento por uso
 ├─ Storage service: upload/download/share sobre MinIO
 ├─ Quotas de envio por tenant + detecção de abuso
 └─ Roteamento tenant → instância Stalwart (para crescer depois)

Stalwart (engine, NÃO modificado)  → SMTP/IMAP/JMAP/DKIM/spam
MinIO                               → blobs dos arquivos
PostgreSQL                          → control plane + backend do Stalwart

Princípio central: o usuário final lê email falando JMAP direto com o Stalwart; o nosso Go só entra em operações administrativas, de billing e de armazenamento. Isso tira o Go do caminho crítico de leitura (menos latência, menos superfície de risco).


5. Modelo de dados (Postgres — control plane)

Ponto de partida; ajuste conforme necessário. Toda query de dados de cliente DEVE filtrar por tenant_id (padrão multi-tenant em banco compartilhado).

tenants        (id, nome, plano, status, stalwart_instance, outbound_ip_pool, created_at)
domains        (id, tenant_id, dominio, stalwart_id, dkim_status, dns_status, provision_state)
mail_users     (id, tenant_id, email, name, role, stalwart_account_id, quota_bytes)
storage_files  (id, tenant_id, owner_user_id, nome, blob_key, hash, tamanho, versao, created_at)
storage_shares (id, file_id, tenant_id, principal, nivel)   -- owner/editor/leitor/link
usage          (tenant_id, storage_bytes, sent_today, periodo)
subscriptions  (tenant_id, stripe_id, faixa_de_caixas, status)
audit_log      (id, tenant_id, actor, acao, alvo, timestamp)

stalwart_id / stalwart_account_id são o elo entre o nosso mundo e o do Stalwart.


6. Integração com o Stalwart (endpoints reais)

Autenticação: nosso Go usa credencial de admin global (Bearer token ou basic auth). No Stalwart, domínio, usuário e grupo são todos "principals", diferenciados pelo campo type. A maior parte da gestão é via JMAP (POST /jmap/ com a capability urn:stalwart:jmap); os endpoints REST abaixo são helpers convenientes para provisionamento.


Criar domínio: POST /api/principal com {"type":"domain","name":"cliente.com.br","description":"tenant_id=42","quota":0} → guardar o id retornado. (Exige permissão sysDomainCreate no token admin.)
Gerar DKIM: POST /api/dkim com domínio, algoritmo (ex.: ed25519), selector. Confirmar os campos exatos no OpenAPI da versão.
Criar mailbox: POST /api/principal com {"type":"individual","name":"joao","secrets":["senha"],"emails":["joao@cliente.com.br"],"quota":5368709120,"roles":["user"]} → guardar o id. Atenção: name é o login e é único no SERVIDOR inteiro, não por domínio — qualifique quando houver colisão. emails são os endereços.
Ler/editar/remover principal: GET|PATCH|DELETE /api/principal/{id} (PATCH usa ações set/addItem/removeItem).
Login / token (webmail): POST /api/auth autentica e devolve código de autorização; trocar por token (OAuth 2.0 / OIDC suportados).
Contas OIDC pré-criadas: POST /api/principal/deploy (mesmo payload do principal) quando usarmos diretório externo OIDC.
Fila / diagnóstico: GET /api/queue/messages e as ferramentas troubleshoot (simular entrega, checar SPF/DKIM/DMARC).


Regra: gere um client Go tipado a partir do openapi.yml da versão fixada. Não escreva os payloads à mão.


7. Máquina de estados de provisionamento (implementar assim)

Provisionar cliente novo envolve chamadas que podem falhar no meio e um passo (DNS) que depende do cliente e demora. Modele como máquina de estados persistida, não como script linear. Cada transição idempotente e recuperável por um worker.

pending → domain_created → dkim_created → dns_pending → dns_verified → active
                                                     ↘ (falha) → error


pending: signup cria tenant + customer Stripe; enfileira provisionamento; responde ao React na hora.
domain_created: POST /api/principal (domínio); salva stalwart_id.
dkim_created: POST /api/dkim.
dns_pending: devolve ao cliente os registros DNS (MX, SPF, TXT do DKIM, DMARC, opcional TLSA/MTA-STS); trava até o cliente publicar.
dns_verified: worker faz polling (resolução DNS própria e/ou troubleshoot) até os registros baterem.
active: cria as mailboxes (individual), salva stalwart_account_id; cliente pronto.


O worker sempre retoma a partir do último estado salvo, nunca do zero.


8. Autenticação e handoff do webmail


Operações administrativas e de billing: React → nossa API Go (o Go carrega a credencial de admin do Stalwart, nunca o cliente).
Leitura/escrita de email: React → JMAP direto no Stalwart com token do usuário final. Fluxo: React manda credenciais para POST /api/auth → recebe código → troca por token → usa como Bearer em POST /jmap/.
Login unificado (opcional, fase posterior): rodar nosso próprio provedor OIDC e configurar o Stalwart com diretório externo OIDC; pré-criar contas via POST /api/principal/deploy.



9. Fases de entrega (trabalhe nesta ordem, uma por vez)

Fase 0 — Fundação. Docker Compose com Stalwart oficial + Postgres + MinIO subindo localmente. Cliente Go tipado gerado do OpenAPI da versão fixada. Esqueleto do projeto Go (API + workers) e do React. Autenticação básica do nosso painel.

Fase 1 — Provisionamento. Máquina de estados da seção 7 completa: signup, criação de domínio, DKIM, tela de DNS, verificação, criação de mailbox. Idempotente e testada. Entregável: consigo criar um cliente novo de ponta a ponta pelo painel.

Fase 2 — Webmail. React consumindo JMAP direto (listar/ler/enviar/buscar email, pastas, anexos). Handoff de auth da seção 8.

Fase 3 — Armazenamento. Serviço Go sobre MinIO: upload multipart/resumável, dedup por hash, versionamento, compartilhamento (owner/editor/leitor/link). UI React.

Fase 4 — Billing e quotas. Stripe (assinaturas por faixa de caixas), quotas de disco e de envio por tenant, faturamento por uso, painel de uso.

Fase 5 — Operação e conformidade. Limites de envio + detecção de abuso por tenant, ganchos de reputação/pool de IP, abuse@, audit_log, LGPD (política de privacidade, DPA, exportação/exclusão de dados), roteamento tenant → instância para escalar.

Não pule fases. Não comece a Fase N+1 antes da Fase N estar testada e funcionando.


10. Requisitos não-funcionais


Idempotência em todo provisionamento e webhook (Stripe, retries).
Testes em cada fase (unit + integração contra um Stalwart de teste em container).
Segurança: credenciais do Stalwart só no backend; segredos fora do código; checagem de posse do tenant antes de toda operação admin; rate limiting.
LGPD (produto brasileiro): somos operadores de dados; precisamos de base legal, DPA com clientes, tratamento de abuse@, e exportação/exclusão de dados por tenant.
Observabilidade: logs estruturados, métricas de fila e de entrega, alertas de blocklist.
Reputação compartilhada: um tenant que faz spam derruba todos no mesmo IP — limites de envio e detecção de abuso são requisito, não enfeite.



11. Como você deve trabalhar (IA, leia isto)


Comece pela Fase 0 e vá uma fase por vez. Ao terminar cada fase, pare, mostre o que foi feito e como testar, e espere confirmação antes de seguir.
Antes de decisões estruturais (escolha de libs, formato de auth, schema final), proponha as opções com trade-offs e pergunte — não decida sozinho e siga.
Nunca modifique o Stalwart. Se algo parecer exigir alterar o Stalwart, pare e reporte — provavelmente há um caminho pela API, ou é sinal de que precisamos da edição Enterprise.
Fixe a versão do Stalwart logo no início e gere o client a partir do OpenAPI dela. Não invente payloads.
Escreva testes junto com o código de cada fase, não depois.
Provisionamento e webhooks são idempotentes por padrão. Assuma que qualquer chamada de rede pode falhar e ser repetida.
Documente as decisões e o setup num README conforme avança.
Se algo neste brief conflitar com a realidade da versão do Stalwart que você encontrar, confie na versão real e me avise da divergência.



Primeiro passo

Confirme que entendeu o escopo e as restrições, proponha as escolhas concretas da Fase 0 (versão do Stalwart a fixar, libs Go e React, layout de repositório) com trade-offs, e espere minha confirmação antes de gerar código.