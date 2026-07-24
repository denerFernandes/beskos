// Package chat implementa o tempo real do chat: um hub que assina o Redis (pub/sub) e distribui
// eventos às conexões WebSocket locais. O Redis desacopla instâncias da API (fan-out horizontal);
// cada instância entrega só às conexões que ela mantém.
package chat

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const redisChannel = "mailin:chat"

// Event trafega pelo Redis e é entregue às conexões dos destinatários.
type Event struct {
	Type       string          `json:"type"` // message | reaction | typing | message_edit | message_delete
	ChannelID  string          `json:"channel_id"`
	Recipients []string        `json:"recipients"` // ids de usuário destinatários
	Payload    json.RawMessage `json:"payload"`
}

type conn struct {
	send chan []byte
}

// Hub mantém as conexões locais por usuário e faz a ponte com o Redis.
type Hub struct {
	rdb   *redis.Client
	mu    sync.RWMutex
	conns map[uuid.UUID]map[*conn]struct{}
}

func New(rdb *redis.Client) *Hub {
	return &Hub{rdb: rdb, conns: map[uuid.UUID]map[*conn]struct{}{}}
}

// Run assina o Redis e distribui eventos às conexões locais. Bloqueia até ctx cancelar.
func (h *Hub) Run(ctx context.Context) {
	sub := h.rdb.Subscribe(ctx, redisChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var ev Event
			if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
				continue
			}
			h.deliver(ev)
		}
	}
}

// Publish envia um evento a todos os destinatários (via Redis → todas as instâncias).
func (h *Hub) Publish(ctx context.Context, ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	if err := h.rdb.Publish(ctx, redisChannel, data).Err(); err != nil {
		log.Printf("chat: publish: %v", err)
	}
}

func (h *Hub) deliver(ev Event) {
	out, _ := json.Marshal(map[string]any{"type": ev.Type, "channel_id": ev.ChannelID, "payload": ev.Payload})
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, rid := range ev.Recipients {
		uid, err := uuid.Parse(rid)
		if err != nil {
			continue
		}
		for c := range h.conns[uid] {
			select {
			case c.send <- out:
			default: // buffer cheio (cliente lento): descarta
			}
		}
	}
}

const presenceTTL = 90 * time.Second

func presenceKey(uid uuid.UUID) string { return "chat:presence:" + uid.String() }

func (h *Hub) markOnline(ctx context.Context, uid uuid.UUID) {
	h.rdb.Set(ctx, presenceKey(uid), "1", presenceTTL)
}
func (h *Hub) clearOnline(ctx context.Context, uid uuid.UUID) { h.rdb.Del(ctx, presenceKey(uid)) }

func (h *Hub) hasConns(uid uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[uid]) > 0
}

// OnlineAmong devolve, dentre os ids dados, quais estão online (presença no Redis).
func (h *Hub) OnlineAmong(ctx context.Context, ids []uuid.UUID) []string {
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = presenceKey(id)
	}
	vals, err := h.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil
	}
	out := []string{}
	for i, v := range vals {
		if v != nil {
			out = append(out, ids[i].String())
		}
	}
	return out
}

// Serve registra a conexão do usuário, roda os laços de leitura/escrita e retorna ao desconectar.
// onClient recebe cada mensagem enviada pelo cliente (ex.: "digitando").
func (h *Hub) Serve(ctx context.Context, userID uuid.UUID, ws *websocket.Conn, onClient func([]byte)) {
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := &conn{send: make(chan []byte, 64)}
	h.register(userID, c)
	h.markOnline(context.Background(), userID)
	defer func() {
		h.unregister(userID, c)
		if !h.hasConns(userID) {
			h.clearOnline(context.Background(), userID)
		}
	}()
	// Heartbeat: renova a presença enquanto conectado (o TTL cobre quedas abruptas).
	go func() {
		tk := time.NewTicker(30 * time.Second)
		defer tk.Stop()
		for {
			select {
			case <-rctx.Done():
				return
			case <-tk.C:
				h.markOnline(context.Background(), userID)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-rctx.Done():
				return
			case msg, ok := <-c.send:
				if !ok {
					return
				}
				wctx, wcancel := context.WithTimeout(rctx, 10*time.Second)
				err := ws.Write(wctx, websocket.MessageText, msg)
				wcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		_, data, err := ws.Read(rctx)
		if err != nil {
			return
		}
		if onClient != nil {
			onClient(data)
		}
	}
}

func (h *Hub) register(uid uuid.UUID, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[uid] == nil {
		h.conns[uid] = map[*conn]struct{}{}
	}
	h.conns[uid][c] = struct{}{}
}

func (h *Hub) unregister(uid uuid.UUID, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.conns[uid]; m != nil {
		delete(m, c)
		if len(m) == 0 {
			delete(h.conns, uid)
		}
	}
	close(c.send)
}
