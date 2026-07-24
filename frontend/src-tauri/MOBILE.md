# Apps móveis (Tauri 2 — Android + iOS)

Os projetos nativos já foram inicializados (`tauri android init` / `tauri ios init`) e vivem em
`src-tauri/gen/android` e `src-tauri/gen/apple`. O ponto de entrada partilhado é
`src-tauri/src/lib.rs` (`#[cfg_attr(mobile, tauri::mobile_entry_point)] pub fn run()`).

## Pré-requisitos

- Rust com os alvos móveis: `rustup target add aarch64-linux-android armv7-linux-androideabi \
  i686-linux-android x86_64-linux-android aarch64-apple-ios aarch64-apple-ios-sim x86_64-apple-ios`
- Android: Android Studio + SDK + NDK. Exporte antes de correr o CLI:
  ```sh
  export ANDROID_HOME="$HOME/Library/Android/sdk"
  export NDK_HOME="$ANDROID_HOME/android-ndk-<versão>"
  ```
- iOS: Xcode + `xcodegen` (o `tauri ios init` instala-o via Homebrew).

## Correr / compilar

```sh
cd frontend
./node_modules/.bin/tauri android dev      # emulador/dispositivo Android
./node_modules/.bin/tauri android build    # APK/AAB
./node_modules/.bin/tauri ios dev          # simulador/dispositivo iOS
./node_modules/.bin/tauri ios build        # IPA (precisa de conta Apple Developer para assinar)
```

## Já configurado

- **Permissões do Meet (WebRTC):** câmara e microfone declaradas —
  `gen/android/app/src/main/AndroidManifest.xml` (CAMERA/RECORD_AUDIO/MODIFY_AUDIO_SETTINGS) e
  `gen/apple/mailin_iOS/Info.plist` (NSCameraUsageDescription/NSMicrophoneUsageDescription). Sem as
  descrições de uso, a App Store recusa o app.
- `POST_NOTIFICATIONS` (Android 13+) para as notificações.
- A UI já é responsiva a 380–430px (ver `webmail.css`), então cabe no ecrã do telemóvel.

## Ainda por fazer (precisa de contas/serviços externos)

1. **Push nativo (app fechado):** o `tauri-plugin-notification` só faz notificação **local**. Push
   real exige **FCM** (Android) e **APNs** (iOS):
   - adicionar um plugin de push (ex.: `tauri-plugin-push` ou integração FCM/APNs nativa);
   - registar o token do dispositivo na nossa API e o backend (`internal/notify`) ganhar um canal
     FCM/APNs além do Web Push atual;
   - Firebase (google-services.json) e um certificado/*key* APNs — precisam das contas.
2. **Spike do WebRTC no WKWebView (iOS)** — decisão registada: **validar antes** de comprometer o
   Meet mobile ao Tauri. O `getUserMedia`/WebRTC no WKWebView do iOS é historicamente frágil; se não
   funcionar, o plano B é abrir o Meet no Safari via `tauri-plugin-opener`. Como testar: `tauri ios
   dev`, entrar numa sala de Meet, confirmar câmara+micro e a publicação de mídia.
3. **Deep links** (`tauri-plugin-deep-link`) para abrir convites de reunião/e-mail no app.
4. **Assinatura e lojas:** conta Apple Developer (iOS) e keystore de release (Android); política de
   privacidade (obrigatória nas lojas — ver §6 LGPD da avaliação).

## Notas de manutenção

- `gen/android` e `gen/apple` são projetos gerados; os `.gitignore` dentro deles já excluem os
  artefactos de build (`.gradle`, `build/`, `xcuserdata/`). Os ficheiros de projeto (manifest,
  Info.plist, Gradle) são versionados e podem ser editados — reexecutar `init` preserva-os.
