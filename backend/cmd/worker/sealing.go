package main

import (
	"context"
	"log"
	"time"

	"mailin/backend/internal/storage"
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
	intervaloSelagem  = 10 * time.Minute
	pausaEntreObjetos = 200 * time.Millisecond
	// Teto por passagem: mantém a passagem curta e o registo legível.
	maxPorPassagem = 500
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
			selarPassagem(context.Background(), st)
			time.Sleep(intervaloSelagem)
		}
	}()
	log.Printf("sealing worker started (encrypts objects left in the clear)")
}

func selarPassagem(ctx context.Context, st *storage.Service) {
	inicio := time.Now()
	var vistos, selados, falhas int

	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	for obj := range st.ListObjects(ctx, "") {
		vistos++
		if !obj.Selavel() {
			continue
		}
		cifrado, err := st.IsEncrypted(ctx, obj.Key)
		if err != nil {
			falhas++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		if cifrado {
			continue
		}
		if err := st.Seal(ctx, obj.Key); err != nil {
			falhas++
			log.Printf("sealing: %s: %v", obj.Key, err)
			continue
		}
		selados++
		if selados >= maxPorPassagem {
			break
		}
		time.Sleep(pausaEntreObjetos)
	}

	if selados > 0 || falhas > 0 {
		log.Printf("sealing: %d objetos vistos, %d selados, %d falhas, em %s",
			vistos, selados, falhas, time.Since(inicio).Round(time.Second))
	}
}
