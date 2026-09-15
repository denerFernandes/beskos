package main

import (
	"context"
	"log"
	"time"

	"github.com/beskosorg/beskos/backend/internal/storage"
)

// Selagem em fundo: percorre o balde e cifra o que ainda estiver em claro.
//
// Cobre os dois casos com o mesmo mecanismo, porque são o mesmo problema — um objeto no
// armazenamento que não passou pela nossa cifra:
//
//   - o conteúdo anterior à cifra existir;
//   - as gravações compostas, escritas diretamente pelo LiveKit Egress com as credenciais S3.
//
// Não guarda estado nenhum sobre o progresso. O que decide se um objeto já está selado é o próprio
// objeto (a magia no cabeçalho), o que torna cada passagem idempotente e imune a reinícios: uma
// marca na base de dados que dessincronizasse levaria a cifrar o cifrado.
//
// RITMO. Uma pausa entre objetos, de propósito: isto corre no mesmo servidor que serve o correio dos
// clientes, e migrar depressa não vale interromper quem está a trabalhar. Uma passagem que não
// termine não é problema — a seguinte continua onde a anterior parou, porque salta o que já está
// feito.

const (
	sealingInterval     = 10 * time.Minute
	pauseBetweenObjects = 200 * time.Millisecond
	// Teto por passagem: mantém a passagem curta e o registo legível.
	maxPerPass = 500
)

func startSealing(st *storage.Service) {
	if st == nil || !st.Encrypting() {
		log.Printf("sealing worker disabled (no storage or FILE_ENC_KEYS unset)")
		return
	}
	go func() {
		// Primeira passagem com atraso: o arranque já tem que fazer.
		time.Sleep(time.Minute)
		for {
			sealPass(context.Background(), st)
			time.Sleep(sealingInterval)
		}
	}()
	log.Printf("sealing worker started (encrypts objects left in the clear)")
}

func sealPass(ctx context.Context, st *storage.Service) {
	start := time.Now()
	var seen, sealed, failures int

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	for obj := range st.ListObjects(ctx, "") {
		seen++
		if !obj.Sealable() {
			continue
		}
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			failures++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		if cifrado {
			continue
		}
		if err := st.Seal(ctx, obj.Key); err != nil {
			failures++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		sealed++
		if sealed >= maxPerPass {
			break
		}
		time.Sleep(pauseBetweenObjects)
	}

	if sealed > 0 || failures > 0 {
		log.Printf("sealing: %d objects seen, %d sealed, %d failures, in %s",
			seen, sealed, failures, time.Since(start).Round(time.Second))
	}
}
