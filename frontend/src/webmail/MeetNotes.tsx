import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { useQuery, useMutation } from "@tanstack/react-query";
import { backend, type Board } from "./backend";
import { notesApi } from "./notesApi";
import { useChatSocket } from "./useChatSocket";
import { useBoards } from "./BoardPicker";
import { ExtractTasksModal } from "./ExtractTasksModal";
import { TaskModal } from "./TaskModal";
import { Spinner } from "./Spinner";
import { toast } from "./toast";
import { IcColumns, IcSparkle, IcMail } from "../panel/icons";

// Turns the textarea's text into BlockNote paragraphs -- the format the Notes app and the wiki
// editor both read. One block per line, because a blank line between two thoughts is the only
// structure a textarea can express.
function paragraphs(text: string): unknown[] {
  return text.split("\n").map((line) => ({
    type: "paragraph",
    content: line ? [{ type: "text", text: line, styles: {} }] : [],
  }));
}

// Conteúdo do painel de notas (o frame/cabeçalho vem do MeetRoom). Documento compartilhado da
// reunião: autosave + sincronização ao vivo. Daqui as notas viram tarefas no Kanban via IA.
export function NotesPanel({ meetingId, title, isHost }: { meetingId: string; title: string; isHost?: boolean }) {
  const { t } = useTranslation();
  const [body, setBody] = useState("");
  const [saving, setSaving] = useState(false);
  const [extract, setExtract] = useState(false);
  const [taskSeed, setTaskSeed] = useState<string | null>(null);
  const ta = useRef<HTMLTextAreaElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  const typing = useRef(false);

  // The meeting's note IS a Note (migration 00133): same object the Notes app lists, same sharing,
  // same "turn into tasks". Fetching it creates it on first use and shares it with everyone who was
  // in the meeting -- which is why the panel asks for it rather than for a body of text.
  const notes = useQuery({ queryKey: ["meeting-note", meetingId], queryFn: () => notesApi.forMeeting(meetingId), retry: false });
  const noteId = notes.data?.id ?? null;
  useEffect(() => { if (notes.data && !typing.current) setBody(notes.data.body_text); }, [notes.data]);

  // Chega edição de outro participante: aplica só se eu não estiver digitando (last-write-wins).
  useChatSocket((ev) => {
    if (ev.type !== "notes" || ev.channel_id !== meetingId) return;
    if (typing.current) return;
    setBody(String(ev.payload.body ?? ""));
  });

  // Autosave com debounce; `typing` evita que o eco do servidor sobrescreva o que estou escrevendo.
  const onChange = (v: string) => {
    setBody(v);
    typing.current = true;
    clearTimeout(timer.current);
    timer.current = setTimeout(async () => {
      setSaving(true);
      // The panel is a plain textarea, so it writes a plain paragraph. Someone who opens the same
      // note in the Notes app gets the rich editor; whoever saves last wins, exactly as two people
      // typing in this panel already did.
      try { if (noteId) await notesApi.update(noteId, { title: title, body_json: paragraphs(v) }); }
      catch (e) { toast((e as Error).message); }
      finally { setSaving(false); typing.current = false; }
    }, 700);
  };

  // Tarefa a partir do trecho selecionado (a IA expande no modal).
  const fromSelection = () => {
    const el = ta.current;
    const sel = el ? el.value.slice(el.selectionStart, el.selectionEnd).trim() : "";
    if (!sel) { toast(t("meet.selectFirst")); return; }
    setTaskSeed(sel);
  };

  // Envia as notas aos participantes. O retorno diz quantos e-mails saíram e quantos falharam —
  // um "enviado!" que esconde metade dos envios perdidos é pior que o silêncio.
  const sendNotes = useMutation({
    mutationFn: () => backend.meetSendNotes(meetingId),
    onSuccess: (r) => toast(r.failed > 0 ? t("meet.notesSentPartial", { sent: r.sent, failed: r.failed }) : t("meet.notesSent", { n: r.sent })),
    onError: (e) => toast((e as Error).message),
  });

  return (
    <div className="notes-panel">
      <textarea ref={ta} className="notes-ta" value={body} placeholder={t("meet.notesPh")}
        onChange={(e) => onChange(e.target.value)} />
      <div className="notes-actions">
        <button className="chipbtn" disabled={!body.trim()} onClick={() => setExtract(true)}>
          <IcSparkle size={14} /> {t("meet.extractTasks")}
        </button>
        <button className="chipbtn" onClick={fromSelection}>
          <IcColumns size={14} /> {t("meet.taskFromSelection")}
        </button>
        {/* Só o host envia: as notas são da sala dele, e a API recusa os outros de qualquer forma. */}
        {isHost && (
          <button className="chipbtn" disabled={!body.trim() || sendNotes.isPending} onClick={() => sendNotes.mutate()}>
            {sendNotes.isPending ? <Spinner size={14} /> : <IcMail size={14} />} {t("meet.emailNotes")}
          </button>
        )}
        {saving && <span className="notes-saving"><Spinner size={12} /></span>}
      </div>
      <p className="notes-hint">{t("meet.notesHint")}</p>

      {extract && <ExtractTasksModal subject={title} body={body} sourceEmailId="" onClose={() => setExtract(false)} />}
      {taskSeed !== null && <NoteTaskModal seed={taskSeed} onClose={() => setTaskSeed(null)} />}
    </div>
  );
}

// Abre o TaskModal do Kanban com o trecho selecionado como semente (o botão "Gerar com IA" do
// modal expande em título + descrição). Com mais de um quadro, pergunta antes onde criar.
function NoteTaskModal({ seed, onClose }: { seed: string; onClose: () => void }) {
  const boards = useBoards();
  const [picked, setPicked] = useState<string | null>(null);
  const boardId = picked ?? (boards.length === 1 ? boards[0].id : null);
  const board = useQuery({ queryKey: ["board", boardId], queryFn: () => backend.board(boardId!), enabled: !!boardId });

  if (boards.length > 1 && !picked) return <ChooseBoard boards={boards} onPick={setPicked} onClose={onClose} />;
  if (!boardId || !board.data) return null;
  return (
    <TaskModal boardId={boardId} columns={board.data.columns.map((c) => c.column)} initialTitle={seed}
      onClose={onClose} onSaved={onClose} />
  );
}

// Passo "em qual quadro?" — só aparece quando há mais de um.
function ChooseBoard({ boards, onPick, onClose }: { boards: Board[]; onPick: (id: string) => void; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="tk-bg open" onMouseDown={onClose}>
      <div className="tk" onMouseDown={(e) => e.stopPropagation()} style={{ width: 380 }}>
        <header><span>{t("webmail.kanban.whichBoard")}</span><button className="ib" title={t("common.close")} onClick={onClose}>✕</button></header>
        <div className="body">
          <div className="hc-access">
            {boards.map((b) => (
              <button key={b.id} onClick={() => onPick(b.id)}><b>{b.name}</b></button>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
