import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Switch } from "./Switch";
import { toast } from "./toast";
import {
  notifSupported, notifPermission, requestNotifPermission, notifPrefs, setNotifPref, testNotification, type NotifDomain,
} from "./desktopNotify";
import { soundPrefs, setSoundPref, playSound } from "./sounds";
import { enableWebPush, disableWebPush, pushSupported } from "./webpush";
import { isTauri } from "./tauriNotify";

// Notificações desktop: permissão do browser + escolha por app. Preferências por-dispositivo
// (localStorage) — a permissão é do browser, então activar aqui não afeta outros aparelhos.
const NOTIF_DOMAINS: NotifDomain[] = ["email", "chat", "meet", "cal", "files", "tasks"];
const WEBPUSH_FLAG = "mailin.webpush.on";
export function NotificationsSection() {
  const { t } = useTranslation();
  const [perm, setPerm] = useState(notifPermission());
  const [prefs, setPrefs] = useState(notifPrefs());
  const [closedOn, setClosedOn] = useState(() => { try { return localStorage.getItem(WEBPUSH_FLAG) === "1"; } catch { return false; } });

  // Ao conceder permissão, tenta também subscrever o Web Push (para notificar com o app fechado).
  // Se o servidor não tiver VAPID, a subscrição falha em silêncio e ficam só as notificações
  // com a aba aberta — em ambos os casos a experiência com a aba aberta funciona.
  const ask = async () => {
    const p = await requestNotifPermission();
    setPerm(p);
    if (p === "granted") { const ok = await enableWebPush(); setClosed(ok); }
  };
  const setClosed = (on: boolean) => { setClosedOn(on); try { localStorage.setItem(WEBPUSH_FLAG, on ? "1" : "0"); } catch { /* modo privado */ } };
  const toggleClosed = async (on: boolean) => {
    if (on) { const ok = await enableWebPush(); setClosed(ok); }
    else { await disableWebPush(); setClosed(false); }
  };
  const toggle = (d: NotifDomain, on: boolean) => { setNotifPref(d, on); setPrefs(notifPrefs()); };
  const [sounds, setSounds] = useState(soundPrefs());
  const toggleSound = (k: "notify" | "meet", on: boolean) => {
    setSoundPref(k, on);
    setSounds(soundPrefs());
    if (on) playSound(k === "meet" ? "meetJoin" : "notify"); // ligar dá logo a ouvir o que ficou ligado
  };

  if (!notifSupported()) {
    return (
      <div className="set-page">
        <h2>{t("settings.notifications")}</h2>
        <div className="set-card"><p className="desc">{t("settings.notifUnsupported")}</p></div>
      </div>
    );
  }

  return (
    <div className="set-page">
      <h2>{t("settings.notifications")}</h2>
      <div className="set-card">
        {perm !== "granted" ? (
          <div className="set-field col">
            <span>{t("settings.notifEnable")}</span>
            <p className="desc">{perm === "denied" ? t("settings.notifDenied") : t("settings.notifAskHint")}</p>
            {perm !== "denied" && (
              <button className="send" style={{ alignSelf: "flex-start" }} onClick={() => void ask()}>
                {t("settings.notifAllow")}
              </button>
            )}
          </div>
        ) : (
          <>
            <div className="ios-group-label">{t("settings.notifPerApp")}</div>
            <div className="ios-list">
              {NOTIF_DOMAINS.map((d) => (
                <div key={d} className="ios-row">
                  <span className="ios-row-label">{t(`settings.notifDomain.${d}`)}</span>
                  <Switch checked={prefs[d]} onChange={(on) => toggle(d, on)} label={t(`settings.notifDomain.${d}`)} />
                </div>
              ))}
            </div>
            {/* App nativo (Tauri): notifica pelo SO sempre que está a correr (mesmo minimizado),
                por isso mostra uma nota em vez do Web Push (que o WKWebView não suporta). Na web,
                o toggle só aparece quando o browser suporta Push; senão explica porquê. */}
            {isTauri() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifNativeTitle")}</span>
                    <span className="ios-row-sub">{t("settings.notifNativeHint")}</span>
                  </div>
                </div>
              </div>
            ) : pushSupported() ? (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifTabHint")}</span>
                  </div>
                  <Switch checked={closedOn} onChange={(on) => void toggleClosed(on)} label={t("settings.notifClosed")} />
                </div>
              </div>
            ) : (
              <div className="ios-list">
                <div className="ios-row">
                  <div className="ios-row-text">
                    <span className="ios-row-label">{t("settings.notifClosed")}</span>
                    <span className="ios-row-sub">{t("settings.notifPushUnsupported")}</span>
                  </div>
                </div>
              </div>
            )}
            <button className="chipbtn" style={{ alignSelf: "flex-start" }}
              onClick={() => { void testNotification(t("settings.notifTestTitle"), t("settings.notifTestBody")).then((ok) => { if (!ok) toast(t("settings.notifTestFail")); }); }}>
              {t("settings.notifTest")}
            </button>
          </>
        )}
      </div>

      {/* Sons: FORA do bloco da permissão, de propósito. Tocar um som não precisa de autorização
          nenhuma do sistema — quem recusou as notificações do SO pode na mesma querer ouvir que
          chegou uma mensagem, e escondê-lo aqui deixava essa pessoa sem nada. */}
      <div className="set-card">
        <div className="ios-group-label">{t("settings.soundGroup")}</div>
        <div className="ios-list">
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.soundNotify")}</span>
              <span className="ios-row-sub">{t(isTauri() ? "settings.soundNotifyHintApp" : "settings.soundNotifyHintWeb")}</span>
            </div>
            <Switch checked={sounds.notify} onChange={(on) => toggleSound("notify", on)} label={t("settings.soundNotify")} />
          </div>
          <div className="ios-row">
            <div className="ios-row-text">
              <span className="ios-row-label">{t("settings.soundMeet")}</span>
              <span className="ios-row-sub">{t("settings.soundMeetHint")}</span>
            </div>
            <Switch checked={sounds.meet} onChange={(on) => toggleSound("meet", on)} label={t("settings.soundMeet")} />
          </div>
        </div>
        {/* Ouvir antes de decidir: um interruptor de som sem forma de o experimentar obriga a
            esperar por uma reunião a sério para saber se está bom. */}
        <div className="set-field" style={{ gap: 8 }}>
          <button className="chipbtn" onClick={() => playSound("notify")}>{t("settings.soundTryNotify")}</button>
          <button className="chipbtn" onClick={() => playSound("meetKnock")}>{t("settings.soundTryKnock")}</button>
        </div>
      </div>
    </div>
  );
}
