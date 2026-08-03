# Beskos — build das apps (desktop, Android, iOS) via Tauri 2.
# A app é o frontend (webmail): src em `frontend/`, projeto nativo em `frontend/src-tauri`.
# Pré-requisitos móveis e assinatura: ver frontend/src-tauri/MOBILE.md.

FRONTEND := frontend
TAURI    := ./node_modules/.bin/tauri
# ARGS repassa flags ao Tauri, ex.: make android ARGS="--apk --target aarch64"
ARGS     ?=

# URLs embutidas na app (VITE_*), por modo. Produção = SaaS público; dev = backend local.
# (São embutidas em texto claro no pacote — só URLs públicas, nunca segredos.)
PROD_API := https://app.beskos.com
PROD_ENV := VITE_API_BASE=$(PROD_API) VITE_PUBLIC_BASE=$(PROD_API)
DEV_ENV  := VITE_API_BASE=http://localhost:8000 VITE_PUBLIC_BASE=http://localhost:5173

.DEFAULT_GOAL := help
.PHONY: help deps web web-dev desktop desktop-prod desktop-dev desktop-run android ios android-dev ios-dev icons doctor clean mobile

help: ## Mostra os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'
	@echo "\n  Ex.: make desktop-prod   make desktop-dev   make android ARGS=\"--apk\""

deps: ## Instala as dependências do frontend (npm --legacy-peer-deps)
	cd $(FRONTEND) && npm install --legacy-peer-deps

web: ## Build só do frontend web (dist/) — sem empacotar
	cd $(FRONTEND) && npm run build

web-dev: ## Frontend local contra a API de PRODUÇÃO (localhost:5173, dados reais)
	@echo "  frontend em http://localhost:5173 · API: $(PROD_API)"
	@echo "  ATENÇÃO: dados REAIS. Apagar, remover ou enviar daqui acontece mesmo."
	@echo
	cd $(FRONTEND) && env -u VITE_API_BASE API_URL=$(PROD_API) npm run dev

## ─── Desktop (macOS/Windows/Linux, conforme a máquina que corre) ───
desktop: desktop-prod ## Atalho para desktop-prod (build de produção)

desktop-prod: ## Build desktop de PRODUÇÃO — aponta para app.beskos.com (.dmg/.msi/.deb…)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) build $(ARGS)

desktop-dev: ## Build desktop de DESENVOLVIMENTO — aponta para o backend local (localhost)
	cd $(FRONTEND) && $(DEV_ENV) $(TAURI) build $(ARGS)

desktop-run: ## Corre a app desktop em modo dev (hot-reload, backend local)
	cd $(FRONTEND) && $(TAURI) dev $(ARGS)

## ─── Android (precisa de ANDROID_HOME + NDK_HOME — ver MOBILE.md) ───
android: ## Compila o APK/AAB Android (produção)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) android build $(ARGS)

android-dev: ## Corre no emulador/dispositivo Android (hot-reload)
	cd $(FRONTEND) && $(TAURI) android dev $(ARGS)

## ─── iOS (só macOS; assinatura exige conta Apple Developer) ───
ios: ## Compila o IPA iOS (produção)
	cd $(FRONTEND) && $(PROD_ENV) $(TAURI) ios build $(ARGS)

ios-dev: ## Corre no simulador/dispositivo iOS (hot-reload)
	cd $(FRONTEND) && $(TAURI) ios dev $(ARGS)

icons: ## Regenera os ícones a partir de src-tauri/icons/icon.png
	cd $(FRONTEND) && $(TAURI) icon src-tauri/icons/icon.png

mobile: android ios ## Compila Android e iOS (iOS ignorado fora de macOS)

doctor: ## Verifica pré-requisitos (tauri info)
	cd $(FRONTEND) && $(TAURI) info

clean: ## Limpa artefactos de build (dist + target Rust)
	rm -rf $(FRONTEND)/dist
	cd $(FRONTEND)/src-tauri && cargo clean

check: ## Verifica as leis do CLAUDE.md nas linhas novas (idioma e tamanho de ficheiro)
	@python3 scripts/check_laws.py && echo "leis: ok"
	@python3 scripts/check_docs.py
