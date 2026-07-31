package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"mailin/backend/internal/db"
)

// Meet — videochamada via LiveKit. Nosso Go assina o token de sala (JWT HS256 com o segredo do
// LiveKit, mesmo padrão do OnlyOffice) e o browser entra com o SDK. O LiveKit roda como serviço
// rede-only, sem modificação (leis §2/§5.4).

type meet struct {
	url    string // WebSocket que o BROWSER usa (ws://…)
	apiKey string
	secret []byte

	// Gravação (Egress). httpURL = API Twirp do LiveKit (nosso Go a alcança); s3* = destino MinIO.
	httpURL    string
	s3endpoint string
	s3key      string
	s3secret   string
	s3bucket   string
}

// EnableMeet liga o Meet. Sem URL/chaves, fica desligado.
func (s *Server) EnableMeet(url, apiKey, secret string) {
	if url == "" || apiKey == "" || secret == "" {
		return
	}
	s.meet = &meet{url: url, apiKey: apiKey, secret: []byte(secret)}
}

// EnableMeetRecording liga a gravação via Egress (opcional). Precisa da API Twirp do LiveKit e do
// destino S3/MinIO.
func (s *Server) EnableMeetRecording(httpURL, s3endpoint, s3key, s3secret, s3bucket string) {
	if s.meet == nil || httpURL == "" || s3bucket == "" {
		return
	}
	s.meet.httpURL = strings.TrimRight(httpURL, "/")
	s.meet.s3endpoint, s.meet.s3key, s.meet.s3secret, s.meet.s3bucket = s3endpoint, s3key, s3secret, s3bucket
}

func (m *meet) recordingEnabled() bool { return m != nil && m.httpURL != "" && m.s3bucket != "" }

// signVideo assina um token do LiveKit com um grant `video` arbitrário (join, record, etc.).
func (m *meet) signVideo(identity string, video map[string]any, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := map[string]any{"iss": m.apiKey, "sub": identity, "nbf": now.Unix(), "exp": now.Add(ttl).Unix(), "video": video}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// videoGrant são os grants de sala do LiveKit embutidos no token.
type videoGrant struct {
	Room              string   `json:"room"`
	RoomJoin          bool     `json:"roomJoin"`
	CanPublish        bool     `json:"canPublish"`
	CanSubscribe      bool     `json:"canSubscribe"`
	CanPublishData    bool     `json:"canPublishData"`
	CanPublishSources []string `json:"canPublishSources,omitempty"`
}

// roomPolicy é o que o organizador libera aos participantes (controles estilo Meet).
type roomPolicy struct{ Mic, Cam, Share, Chat bool }

func fullPolicy() roomPolicy { return roomPolicy{true, true, true, true} }

// meetingPolicy é a política da sala (o que vale para participantes e convidados; o dono recebe
// fullPolicy).
func meetingPolicy(m db.Meeting) roomPolicy {
	return roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
}

// sources traduz a política para as fontes publicáveis do token (nomes minúsculos, formato do grant).
func (p roomPolicy) sources() []string {
	s := []string{}
	if p.Mic {
		s = append(s, "microphone")
	}
	if p.Cam {
		s = append(s, "camera")
	}
	if p.Share {
		s = append(s, "screen_share", "screen_share_audio")
	}
	return s
}

// grant monta o videoGrant da política. ATENÇÃO: no LiveKit, canPublishSources vazio/ausente
// significa "todas as fontes"; por isso, se nada é permitido, negamos canPublish por inteiro.
func (p roomPolicy) grant(room string) videoGrant {
	// CanPublishData sempre true: o data channel é app-controlado (chat "digitando" + apresentação —
	// slides/caneta/laser). Sem isto, um apresentador não-organizador não conseguiria transmitir a
	// apresentação aos demais. A permissão de anotar é aplicada no cliente pelo apresentador.
	g := videoGrant{Room: room, RoomJoin: true, CanSubscribe: true, CanPublishData: true}
	srcs := p.sources()
	switch {
	case len(srcs) == 0:
		g.CanPublish = false
	case p.Mic && p.Cam && p.Share:
		g.CanPublish = true // tudo liberado: omite a lista
	default:
		g.CanPublish = true
		g.CanPublishSources = srcs
	}
	return g
}

// token assina um access token do LiveKit para (identity, room), com os grants da política.
func (m *meet) token(identity, name, room string, ttl time.Duration, p roomPolicy) (string, error) {
	now := time.Now()
	claims := map[string]any{
		"iss":   m.apiKey,
		"sub":   identity,
		"name":  name,
		"nbf":   now.Unix(),
		"exp":   now.Add(ttl).Unix(),
		"video": p.grant(room),
	}
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := header + "." + enc.EncodeToString(body)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// verifyWebhook confere o JWT que o LiveKit envia no header Authorization (HS256 com o segredo),
// incluindo o hash sha256 do corpo. Garante que o webhook veio do nosso LiveKit.
func (m *meet) verifyWebhook(authz string, body []byte) bool {
	tok := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return false
	}
	enc := base64.RawURLEncoding
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := enc.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	raw, err := enc.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Sha256 string `json:"sha256"`
		Exp    int64  `json:"exp"`
		Nbf    int64  `json:"nbf"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return false
	}
	// Rejeita token expirado / ainda não válido: sem isto um webhook capturado é reproduzível
	// indefinidamente (auditoria). LiveKit emite exp; exige presença e validade.
	now := time.Now().Unix()
	if claims.Exp == 0 || now > claims.Exp || (claims.Nbf != 0 && now < claims.Nbf) {
		return false
	}
	sum := sha256.Sum256(body)
	return claims.Sha256 == base64.StdEncoding.EncodeToString(sum[:])
}

// protoSources traduz a política para o enum TrackSource do protobuf (usado no UpdateParticipant).
func (p roomPolicy) protoSources() []string {
	s := []string{}
	if p.Mic {
		s = append(s, "MICROPHONE")
	}
	if p.Cam {
		s = append(s, "CAMERA")
	}
	if p.Share {
		s = append(s, "SCREEN_SHARE", "SCREEN_SHARE_AUDIO")
	}
	return s
}

// roomAPI chama a API RoomService do LiveKit com token de admin da sala.
func (m *meet) roomAPI(ctx context.Context, method string, req any, room string) ([]byte, error) {
	tok, err := m.signVideo("admin", map[string]any{"roomAdmin": true, "room": room}, 10*time.Minute)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.httpURL+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("roomservice %s: HTTP %d: %s", method, resp.StatusCode, out)
	}
	return out, nil
}

// applyPolicy aplica a política a quem JÁ está conectado (o token só vale para quem entra depois).
// O organizador mantém os grants completos.
func (m *meet) applyPolicy(ctx context.Context, room, hostIdentity string, p roomPolicy) error {
	out, err := m.roomAPI(ctx, "ListParticipants", map[string]any{"room": room}, room)
	if err != nil {
		return err
	}
	var lp struct {
		Participants []struct {
			Identity string `json:"identity"`
		} `json:"participants"`
	}
	if err := json.Unmarshal(out, &lp); err != nil {
		return err
	}
	// canPublishData SEMPRE true (igual ao grant do token): o data channel é app-controlado
	// (apresentação/caneta/laser/"digitando"). Amarrá-lo ao chat quebrava a apresentação de quem já
	// estava conectado quando o host desligava o chat. O chat on/off é aplicado no cliente (allow_chat).
	perm := map[string]any{"canSubscribe": true, "canPublishData": true}
	if srcs := p.protoSources(); len(srcs) == 0 {
		perm["canPublish"] = false
	} else {
		perm["canPublish"] = true
		perm["canPublishSources"] = srcs
	}
	for _, pt := range lp.Participants {
		if pt.Identity == hostIdentity {
			continue
		}
		if _, err := m.roomAPI(ctx, "UpdateParticipant", map[string]any{
			"room": room, "identity": pt.Identity, "permission": perm,
		}, room); err != nil {
			return err
		}
	}
	return nil
}

const meetTokenTTL = 6 * time.Hour

type joinMeetReq struct {
	Room   string `json:"room"`   // vazio → cria uma sala nova (ad-hoc)
	Access string `json:"access"` // public | approval (só quando cria uma sala nova)
}

// handleJoinMeet devolve a URL do LiveKit e um token para entrar na sala. Se room vier vazio,
// cria uma reunião instantânea (com registro, para ser compartilhável e ter controle de acesso).
func (s *Server) handleJoinMeet(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		writeErr(w, http.StatusServiceUnavailable, "meet.disabled")
		return
	}
	var req joinMeetReq
	if !decode(w, r, &req) {
		return
	}
	mu := mailUserFrom(r.Context())
	room := strings.TrimSpace(req.Room)
	if room == "" {
		room = "mailin-" + uuid.NewString()[:8]
	}
	// Toda sala tem registro (dono = quem cria/entra primeiro). É o que dá organizador,
	// controles e acesso também para salas ad-hoc (ex.: chamada iniciada no chat).
	m, err := s.q.GetMeetingByRoom(r.Context(), room)
	if err != nil {
		m, err = s.q.CreateMeeting(r.Context(), db.CreateMeetingParams{
			TenantID: mu.TenantID, OwnerUserID: mu.ID, Title: "Reunião instantânea",
			RoomName: room, Access: normalizeAccess(req.Access),
		})
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "meet.create_failed")
			return
		}
		_, _ = s.q.AddParticipant(r.Context(), db.AddParticipantParams{
			MeetingID: m.ID, TenantID: mu.TenantID, Email: mu.Email,
			UserID: pgtype.UUID{Bytes: mu.ID, Valid: true}, Role: "host",
		})
	} else if m.TenantID != mu.TenantID {
		// A sala é indexada só por nome (GetMeetingByRoom não filtra tenant). Sem esta checagem,
		// um usuário de outro tenant que adivinhe/enumere o nome recebe um token de mídia da sala
		// alheia, furando inclusive o lobby de aprovação (auditoria H3). Nunca emitir token aqui.
		writeErr(w, http.StatusForbidden, "meet.forbidden")
		return
	}

	// O organizador (e os co-hosts) publicam tudo; os demais recebem os grants da política da sala.
	isHost := s.isHostOf(r.Context(), m, mu.ID)

	// "Organizador entra primeiro": sem ele na sala, ninguém entra. Recusa ANTES de assinar o
	// token — um token emitido já daria acesso, mesmo que a UI mostrasse uma sala de espera.
	if s.lockedOut(r.Context(), m, isHost) {
		writeJSON(w, http.StatusOK, map[string]any{
			"locked": true, "room": room, "title": m.Title, "meeting_id": m.ID,
		})
		return
	}

	// Limite de participantes por plano do tenant. Hosts/co-hosts entram sempre (para poderem abrir
	// e moderar); só conta para os demais. Erro do LiveKit (-1) não bloqueia (a sala importa mais).
	if !isHost && s.meet != nil {
		if t, terr := s.q.GetTenant(r.Context(), mu.TenantID); terr == nil {
			limit := s.planByKey(r.Context(), t.Plan).MaxMeetParticipants
			if n := s.meet.participantCount(r.Context(), room); n >= 0 && n >= limit {
				writeJSON(w, http.StatusOK, map[string]any{
					"full": true, "room": room, "title": m.Title, "meeting_id": m.ID, "limit": limit,
				})
				return
			}
		}
	}
	policy := roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
	if isHost {
		policy = fullPolicy()
	}
	tok, err := s.meet.token(mu.ID.String(), mu.Name, room, meetTokenTTL, policy)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "meet.token_failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"url":        s.meet.url,
		"token":      tok,
		"room":       room,
		"identity":   mu.ID.String(),
		"name":       mu.Name,
		"meeting_id": m.ID,
		"title":      m.Title,
		"is_host":    isHost,
		"is_owner":   m.OwnerUserID == mu.ID,
		"access":     m.Access,
		"controls":   controlsDTO(m),
	})
}

// controlsDTO expõe os controles do organizador ao frontend.
func controlsDTO(m db.Meeting) map[string]bool {
	return map[string]bool{
		"allow_mic": m.AllowMic, "allow_cam": m.AllowCam,
		"allow_share": m.AllowShare, "allow_chat": m.AllowChat,
		"lock_until_host": m.LockUntilHost, "locked": m.Locked,
	}
}

// handleUpdateMeetControls — SÓ O DONO da sala altera: a query filtra por owner_user_id, então
// um não-dono recebe "no rows" → 403. Aplica também a quem já está conectado.
func (s *Server) handleUpdateMeetControls(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req struct {
		AllowMic      bool `json:"allow_mic"`
		AllowCam      bool `json:"allow_cam"`
		AllowShare    bool `json:"allow_share"`
		AllowChat     bool `json:"allow_chat"`
		LockUntilHost bool `json:"lock_until_host"`
		Locked        bool `json:"locked"` // trancar a sala a novas entradas
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := s.q.UpdateMeetingControls(r.Context(), db.UpdateMeetingControlsParams{
		ID: id, OwnerUserID: mu.ID,
		AllowMic: req.AllowMic, AllowCam: req.AllowCam, AllowShare: req.AllowShare, AllowChat: req.AllowChat,
		LockUntilHost: req.LockUntilHost, Locked: req.Locked,
	})
	if err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	if s.meet != nil {
		policy := roomPolicy{Mic: m.AllowMic, Cam: m.AllowCam, Share: m.AllowShare, Chat: m.AllowChat}
		if perr := s.meet.applyPolicy(r.Context(), m.RoomName, m.OwnerUserID.String(), policy); perr != nil {
			log.Printf("meet: apply policy in room %s: %v", m.RoomName, perr)
		}
	}
	writeJSON(w, http.StatusOK, controlsDTO(m))
}

// handleUpdateMeetAccess — SÓ O DONO: troca aberta ↔ aprovação sem sair da sala.
func (s *Server) handleUpdateMeetAccess(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "meet.invalid_id")
		return
	}
	var req struct {
		Access string `json:"access"`
	}
	if !decode(w, r, &req) {
		return
	}
	m, err := s.q.UpdateMeetingAccess(r.Context(), db.UpdateMeetingAccessParams{
		ID: id, OwnerUserID: mu.ID, Access: normalizeAccess(req.Access),
	})
	if err != nil {
		writeErr(w, http.StatusForbidden, "meet.not_host")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"access": m.Access})
}

// handleMeetWebhook recebe os eventos do LiveKit (server-to-server) e atualiza os tempos reais
// da reunião e a presença dos participantes. Autenticado pela assinatura do LiveKit (não sessão).
func (s *Server) handleMeetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.meet == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !s.meet.verifyWebhook(r.Header.Get("Authorization"), body) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	var ev struct {
		Event string `json:"event"`
		Room  struct {
			Name string `json:"name"`
		} `json:"room"`
		Participant struct {
			Identity string `json:"identity"`
		} `json:"participant"`
		Track struct {
			Sid  string          `json:"sid"`
			Type json.RawMessage `json:"type"`
		} `json:"track"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	switch ev.Event {
	case "room_started":
		_ = s.q.SetMeetingStarted(ctx, ev.Room.Name)
	case "room_finished":
		_ = s.q.SetMeetingEnded(ctx, ev.Room.Name)
	case "participant_joined":
		if id, err := uuid.Parse(ev.Participant.Identity); err == nil {
			_ = s.q.MarkParticipantJoined(ctx, db.MarkParticipantJoinedParams{RoomName: ev.Room.Name, UserID: id})
		}
	case "participant_left":
		if id, err := uuid.Parse(ev.Participant.Identity); err == nil {
			_ = s.q.MarkParticipantLeft(ctx, db.MarkParticipantLeftParams{RoomName: ev.Room.Name, UserID: pgtype.UUID{Bytes: id, Valid: true}})
			// Se quem saiu era o apresentador, encerra a apresentação ativa da sala (senão fica
			// órfã: ao reentrar continuava "a apresentar"). Só afeta apresentações criadas por ele.
			_ = s.q.DeactivateRoomPresentationsByCreator(ctx, db.DeactivateRoomPresentationsByCreatorParams{Room: ev.Room.Name, CreatedBy: id})
		}
	case "track_published":
		// Faixa nova numa sala com gravação em modo tracks ativa → liga egress para ela.
		s.handleTrackPublished(ctx, ev.Room.Name, ev.Participant.Identity, ev.Track.Type, ev.Track.Sid)
	case "egress_started", "egress_updated", "egress_ended", "egress_failed":
		s.handleEgressEvent(ctx, ev.Event, body)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
