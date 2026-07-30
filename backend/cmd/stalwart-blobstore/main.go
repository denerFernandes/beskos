// stalwart-blobstore aponta o armazenamento de blocos do Stalwart para o nosso gateway de cifra.
//
// PORQUÊ UM COMANDO e não a interface de administração: a administração do Stalwart escuta em
// 127.0.0.1 no VPS, de propósito — por ali passam credenciais de administração do correio. Expô-la
// à internet para fazer uma alteração de dois campos trocaria uma afirmação da página de segurança
// por cinco minutos de comodidade. As apps do CapRover já alcançam o Stalwart por dentro; este
// comando corre lá e não abre nada.
//
// LÊ ANTES DE ESCREVER. A primeira coisa que faz é imprimir a configuração atual, inteira, para o
// registo. Se a alteração correr mal, a reversão está no registo e não na memória de alguém.
//
//	--ver     mostra a configuração atual e não toca em nada (por omissão)
//	--aplicar escreve a alteração
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"mailin/backend/internal/stalwart"
)

func main() {
	log.SetFlags(0)
	aplicar := flag.Bool("aplicar", false, "escreve a alteração; sem isto só mostra")
	flag.Parse()

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	atual, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("ler a configuração atual: %v", err)
	}
	log.Printf("id do singleton: %s", atual.ID)
	log.Printf("CONFIGURAÇÃO ATUAL (guardar isto — é a reversão):\n%s", identar(atual.Config))

	novo := stalwart.S3BlobStore{
		Bucket:    env("BLOBGW_S3_BUCKET"),
		Endpoint:  env("BLOBGW_ENDPOINT"),
		Region:    envOu("BLOBGW_REGION", "auto"),
		AccessKey: env("BLOBGW_ACCESS_KEY"),
		SecretKey: env("BLOBGW_SECRET_KEY"),
		KeyPrefix: envOu("BLOBGW_KEY_PREFIX", "mail/"),
	}
	log.Printf("DESTINO: %s, balde %s, prefixo %s", novo.Endpoint, novo.Bucket, novo.KeyPrefix)

	if !*aplicar {
		log.Printf("nada foi alterado (faltou --aplicar)")
		esperar()
		return
	}

	if err := c.SetBlobStoreS3(ctx, atual.ID, novo); err != nil {
		log.Fatalf("aplicar: %v", err)
	}
	log.Printf("aplicado")

	// Ler de volta. "Escrevi" não é o mesmo que "está lá": um `set` aceite e um estado por
	// confirmar são coisas diferentes, e esta é fácil de conferir.
	depois, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("reler: %v", err)
	}
	log.Printf("CONFIGURAÇÃO AGORA:\n%s", identar(depois.Config))
	esperar()
}

// esperar mantém o contentor vivo depois do trabalho feito. Sem isto o CapRover vê o processo sair,
// dá-o como caído e reinicia-o em ciclo — e o registo, que é o resultado deste comando, perdia-se
// no meio das repetições.
func esperar() {
	log.Printf("terminado; a app pode ser apagada")
	select {}
}

func identar(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	bonito, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(bonito)
}

func env(nome string) string {
	v := os.Getenv(nome)
	if v == "" {
		log.Fatalf("%s em falta", nome)
	}
	return v
}

func envOu(nome, omissao string) string {
	if v := os.Getenv(nome); v != "" {
		return v
	}
	return omissao
}
