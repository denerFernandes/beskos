# Document Server com a marca Beskos.
#
# Em dev o compose monta os scripts na imagem oficial e o patch corre no arranque. Em produção o
# CapRover faz deploy POR IMAGEM e não monta nada do repositório — por isso a modificação tem de
# estar dentro da imagem. É o que este Dockerfile faz.
#
# O patch é aplicado no BUILD (imutável, e a build falha alto se uma versão futura do Document
# Server mudar os padrões, em vez de o problema só aparecer em produção). O entrypoint volta a
# chamá-lo por segurança: é idempotente e reporta "já estava".
#
# LICENÇA: a imagem que sai daqui é uma versão modificada de ONLYOFFICE® Docs Community Edition,
# da Ascensio System SIA, sob AGPL v3. O fonte correspondente é público:
# https://github.com/denerFernandes/beskos-docs-server — e é para lá que aponta o
# ONLYOFFICE_SOURCE_URL da api, que o editor mostra no "Sobre". Ao mudar de versão base, atualizar
# os commits fixados no README desse repositório.

# Pinado por digest (Docs 9.4.0-129, Ubuntu 24.04) — o patch depende do web-apps NÃO minificado,
# que só existe do 9.4 em diante.
FROM onlyoffice/documentserver@sha256:e3da62a847b9a5d51a11f73cfea1d9c13c3be3809614490d4edddcf01dcf919b

COPY brand/ /scripts/brand/
COPY onlyoffice-branding-patch.sh onlyoffice-ai-init.sh /scripts/

# Aplica a marca e verifica que pegou mesmo: sem esta verificação, um padrão que deixasse de casar
# passaria despercebido e a imagem ia para produção com a marca do ONLYOFFICE.
RUN sh /scripts/onlyoffice-branding-patch.sh \
	&& grep -q beskos-branding-patch /var/www/onlyoffice/documentserver/web-apps/apps/documenteditor/main/app/controller/Main.js \
	&& grep -q beskos-branding-patch /var/www/onlyoffice/documentserver/web-apps/apps/documenteditor/main/resources/css/app.css \
	&& test -f /var/www/onlyoffice/documentserver/web-apps/apps/common/main/resources/img/beskos/wordmark-light.svg

# Injeta a config de IA (precisa das envs, logo só no arranque) e depois arranca o Document Server.
ENTRYPOINT ["/bin/sh", "/scripts/onlyoffice-ai-init.sh"]
