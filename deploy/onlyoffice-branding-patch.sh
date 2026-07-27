#!/bin/sh
# Patch de branding do Document Server (Beskos).
#
# PORQUÊ: no Docs Community o servidor reporta a licença com `branding: false` e
# `customization: false`, e o cliente descarta `editorConfig.customization.logo` e
# `.customer` sem avisar. O patch liga os dois interruptores NO CLIENTE, para o cabeçalho
# do editor mostrar a marca Beskos em vez da do ONLYOFFICE.
#
# ONDE: web-apps/apps/<editor>/main/app/controller/Main.js — a partir do Docs 9.4 o
# web-apps é distribuído SEM minificação, por isso são duas linhas legíveis por editor:
#
#   this.appOptions.canBrandingExt = params.asc_getCanBranding() && (...)   -> layout/loaderLogo
#   this.appOptions.canBranding    = params.asc_getCustomization()          -> logo + "Sobre"
#
# LICENÇA (AGPL v3, secção 5 + política de marcas da Ascensio System SIA):
#   - a modificação é permitida e está registada em deploy/DIVERGENCES.md, com data;
#   - o Beskos serve este editor pela rede, logo o §13 obriga a oferecer o fonte modificado:
#     é o que este ficheiro é, e o "Sobre" liga para ele;
#   - a atribuição NÃO é removida. O diálogo "Sobre" continua a mostrar o bloco
#     "Powered by / Ascensio System SIA / onlyoffice.com" (id-about-licensor-short, que o
#     próprio setLicInfo revela quando enviamos `customer`), e o backend acrescenta a nota
#     de versão modificada em customization.customer.info;
#   - o que retiramos é a MARCA deles do nosso cabeçalho — que é exatamente o que a política
#     de marcas exige de quem publica uma versão modificada: identidade própria, sem imitar.
#
# Falha SEMPRE de forma segura: se algo correr mal, o Document Server arranca à mesma, apenas
# sem branding. Idempotente — reaplicar não duplica nada.

WEBAPPS=/var/www/onlyoffice/documentserver/web-apps/apps
MARKER=beskos-branding-patch
PATCHED=0
SKIPPED=0

for app in documenteditor spreadsheeteditor presentationeditor pdfeditor visioeditor; do
	f="$WEBAPPS/$app/main/app/controller/Main.js"
	[ -f "$f" ] || continue

	if grep -q "$MARKER" "$f" 2>/dev/null; then
		SKIPPED=$((SKIPPED + 1))
		continue
	fi

	# Só o valor de arranque muda; o resto da expressão (o `&& customization é objeto`) fica,
	# para o branding continuar a depender de nós enviarmos mesmo um objeto de customização.
	sed -i \
		-e "s|\(this\.appOptions\.canBrandingExt *= *\)params\.asc_getCanBranding()|\1true /* $MARKER */|" \
		-e "s|\(this\.appOptions\.canBranding *= *\)params\.asc_getCustomization()|\1true /* $MARKER */|" \
		"$f" 2>/dev/null || continue

	if ! grep -q "$MARKER" "$f" 2>/dev/null; then
		echo "branding patch: $app — padrão não encontrado (versão nova do DS?), ignorado"
		continue
	fi

	# O nginx do DS serve com gzip_static: sem regenerar o .gz, o browser recebe o original.
	if [ -f "$f.gz" ]; then
		gzip -9 -c "$f" > "$f.gz" 2>/dev/null || echo "branding patch: $app — falhou a regerar o .gz"
	fi
	PATCHED=$((PATCHED + 1))
done

echo "branding patch: $PATCHED editor(es) modificado(s), $SKIPPED já estava(m)"
