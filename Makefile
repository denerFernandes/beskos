# Beskos — build das apps (desktop, Android, iOS) via Tauri 2.
# A app é o frontend (webmail): src em `frontend/`, projeto nativo em `frontend/src-tauri`.
# Pré-requisitos móveis e assinatura: ver frontend/src-tauri/MOBILE.md.

FRONTEND := frontend
TAURI    := ./node_modules/.bin/tauri
# ARGS repassa flags ao Tauri, ex.: make android ARGS="--apk --target aarch64"
ARGS     ?=

.DEFAULT_GOAL := help
.PHONY: help deps web desktop android ios android-dev ios-dev desktop-dev icons doctor clean

help: ## Mostra os alvos disponíveis
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
	@echo "\n  Ex.: make android ARGS=\"--apk\"   make desktop   make ios"

deps: ## Instala as dependências do frontend (npm --legacy-peer-deps)
	cd $(FRONTEND) && npm install --legacy-peer-deps

web: ## Build só do frontend web (dist/) — sem empacotar
	cd $(FRONTEND) && npm run build

## ─── Desktop (macOS/Windows/Linux, conforme a máquina que corre) ───
desktop: ## Compila o instalador desktop (.dmg/.msi/.deb…)
	cd $(FRONTEND) && $(TAURI) build $(ARGS)

desktop-dev: ## Corre a app desktop em modo dev (hot-reload)
	cd $(FRONTEND) && $(TAURI) dev $(ARGS)

## ─── Android (precisa de ANDROID_HOME + NDK_HOME — ver MOBILE.md) ───
android: ## Compila o APK/AAB Android
	cd $(FRONTEND) && $(TAURI) android build $(ARGS)

android-dev: ## Corre no emulador/dispositivo Android
	cd $(FRONTEND) && $(TAURI) android dev $(ARGS)

## ─── iOS (só macOS; assinatura exige conta Apple Developer) ───
ios: ## Compila o IPA iOS
	cd $(FRONTEND) && $(TAURI) ios build $(ARGS)

ios-dev: ## Corre no simulador/dispositivo iOS
	cd $(FRONTEND) && $(TAURI) ios dev $(ARGS)

icons: ## Regenera os ícones a partir de src-tauri/icons/icon.png
	cd $(FRONTEND) && $(TAURI) icon src-tauri/icons/icon.png

mobile: android ios ## Compila Android e iOS (iOS ignorado fora de macOS)

doctor: ## Verifica pré-requisitos (tauri info)
	cd $(FRONTEND) && $(TAURI) info

clean: ## Limpa artefactos de build (dist + target Rust)
	rm -rf $(FRONTEND)/dist
	cd $(FRONTEND)/src-tauri && cargo clean
