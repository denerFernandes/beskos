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
//	--apply escreve a alteração
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
	apply := flag.Bool("apply", false, "write the change; without this it only reports")
	flag.Parse()

	log.Printf("=== corrida %s ===", time.Now().UTC().Format(time.RFC3339))

	c := stalwart.New(env("STALWART_URL"), env("STALWART_ADMIN_ID"), env("STALWART_ADMIN_SECRET"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	current, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("read the current configuration: %v", err)
	}
	log.Printf("singleton id: %s", current.ID)
	log.Printf("CURRENT CONFIGURATION (keep this — it is the rollback):\n%s", indent(current.Config))

	target := stalwart.S3BlobStore{
		Bucket:    env("BLOBGW_S3_BUCKET"),
		Endpoint:  env("BLOBGW_ENDPOINT"),
		Region:    envOr("BLOBGW_REGION", "auto"),
		AccessKey: env("BLOBGW_ACCESS_KEY"),
		SecretKey: env("BLOBGW_SECRET_KEY"),
		KeyPrefix: envOr("BLOBGW_KEY_PREFIX", "mail/"),
	}
	log.Printf("TARGET: %s, bucket %s, prefix %s", target.Endpoint, target.Bucket, target.KeyPrefix)

	if !*apply {
		log.Printf("nothing was changed (--apply was not given)")
		wait()
		return
	}

	if err := c.SetBlobStoreS3(ctx, current.ID, target); err != nil {
		log.Fatalf("aplicar: %v", err)
	}
	log.Printf("applied")

	// Sem isto o Stalwart continua a usar o armazenamento antigo até reiniciar — e o correio novo
	// ia para o sítio antigo sem dar erro nenhum.
	if err := c.ReloadSettings(ctx); err != nil {
		log.Fatalf("reload the configuration: %v", err)
	}
	log.Printf("configuration reloaded")

	// Ler de volta. "Escrevi" não é o mesmo que "está lá": um `set` aceite e um estado por
	// confirmar são coisas diferentes, e esta é fácil de conferir.
	after, err := c.GetBlobStore(ctx)
	if err != nil {
		log.Fatalf("re-read: %v", err)
	}
	log.Printf("CONFIGURATION NOW:\n%s", indent(after.Config))
	wait()
}

// esperar mantém o contentor vivo depois do trabalho feito. Sem isto o CapRover vê o processo sair,
// dá-o como caído e reinicia-o em ciclo — e o registo, que é o resultado deste comando, perdia-se
// no meio das repetições.
func wait() {
	log.Printf("done; the app can be deleted")
	// Dormir em ciclo, e não `select {}`: sem casos, o Go deteta impasse e aborta o processo — o
	// que faz o CapRover reiniciar em ciclo e mistura o registo de várias corridas, que é
	// exatamente o que este comando não pode fazer, porque o registo É o resultado.
	for {
		time.Sleep(time.Hour)
	}
}

func indent(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(pretty)
}

func env(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("%s missing", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
