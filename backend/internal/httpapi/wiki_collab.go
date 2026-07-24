package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"mailin/backend/internal/wiki"
)

// Colaboração em tempo real da wiki (Bloco 2 B10, decisão do utilizador: relay em GO, sem container
// novo). O servidor NÃO entende Yjs: retransmite as mensagens binárias do protocolo y-websocket aos
// pares da mesma sala (página) — os próprios clientes respondem aos pedidos de sync uns dos outros.
// A fonte de verdade persistida continua a ser wiki_pages.body_json (autosave via UpdatePage), com o
// RAG/versões/render intactos; isto é só a camada "ao vivo".
//
// Regra de seed (anti-duplicação): o 1º frame que o servidor envia é {"type":"hello","peers":N}.
// peers==0 → o cliente semeia o Y.Doc a partir do body_json; peers>0 → começa VAZIO e sincroniza dos
// pares. Semear duas vezes duplicaria o conteúdo (docs Yjs independentes fundidos).
//
// Escala: as salas são in-process — o deploy corre UMA instância da api. Se um dia houver várias,
// acrescenta-se fan-out por Redis (padrão do chat.Hub) sem mudar o protocolo do cliente.

type collabPeer struct {
	send chan collabFrame
}

type collabFrame struct {
	kind websocket.MessageType
	data []byte
}

type collabRooms struct {
	mu    sync.Mutex
	rooms map[uuid.UUID]map[*collabPeer]bool
}

var wikiCollab = &collabRooms{rooms: map[uuid.UUID]map[*collabPeer]bool{}}

// join regista o peer e devolve quantos pares JÁ estavam na sala (para a regra de seed).
func (c *collabRooms) join(page uuid.UUID, p *collabPeer) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.rooms[page]
	if room == nil {
		room = map[*collabPeer]bool{}
		c.rooms[page] = room
	}
	n := len(room)
	room[p] = true
	return n
}

func (c *collabRooms) leave(page uuid.UUID, p *collabPeer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.rooms[page]; room != nil {
		delete(room, p)
		if len(room) == 0 {
			delete(c.rooms, page)
		}
	}
	close(p.send)
}

// broadcast entrega a mensagem a todos os OUTROS peers da sala; peer lento (buffer cheio) perde o
// frame — o protocolo Yjs resincroniza sozinho (state vectors), nunca corrompe.
func (c *collabRooms) broadcast(page uuid.UUID, from *collabPeer, f collabFrame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for p := range c.rooms[page] {
		if p == from {
			continue
		}
		select {
		case p.send <- f:
		default:
		}
	}
}

// handleWikiCollabWS liga um editor à sala da página. Sessão + canAccess>=editor (lei 5.2): quem só
// lê não entra na edição colaborativa.
func (s *Server) handleWikiCollabWS(w http.ResponseWriter, r *http.Request) {
	mu := mailUserFrom(r.Context())
	pid, ok := wikiURLID(w, r, "id")
	if !ok {
		return
	}
	role, err := s.wiki.PageRole(r.Context(), mu.TenantID, mu.ID, pid)
	if err != nil || role < wiki.RoleViewer {
		writeErr(w, http.StatusNotFound, "wiki.not_found")
		return
	}
	if role < wiki.RoleEditor {
		writeErr(w, http.StatusForbidden, "wiki.forbidden")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originHostPatterns(s.corsOrigins)})
	if err != nil {
		return
	}
	defer ws.CloseNow()
	// Updates Yjs podem ser grandes (colagens, imagens base64 não — só texto); 4 MiB de teto.
	ws.SetReadLimit(4 << 20)

	rctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	peer := &collabPeer{send: make(chan collabFrame, 256)}
	existing := wikiCollab.join(pid, peer)
	defer wikiCollab.leave(pid, peer)

	// Writer: única goroutine a escrever neste socket.
	go func() {
		for {
			select {
			case <-rctx.Done():
				return
			case f, okc := <-peer.send:
				if !okc {
					return
				}
				wctx, wcancel := context.WithTimeout(rctx, 10*time.Second)
				werr := ws.Write(wctx, f.kind, f.data)
				wcancel()
				if werr != nil {
					cancel()
					return
				}
			}
		}
	}()

	// 1º frame: hello com a contagem de pares (regra de seed do cliente).
	hello, _ := json.Marshal(map[string]any{"type": "hello", "peers": existing})
	peer.send <- collabFrame{kind: websocket.MessageText, data: hello}

	// Reader: retransmite tudo (sync/awareness Yjs são opacos para nós) aos restantes da sala.
	for {
		kind, data, rerr := ws.Read(rctx)
		if rerr != nil {
			return
		}
		wikiCollab.broadcast(pid, peer, collabFrame{kind: kind, data: data})
	}
}
