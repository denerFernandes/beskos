// mailprova envia uma mensagem de teste pelo caminho real do correio e diz o que procurar a seguir.
//
// PORQUÊ. Depois de trocar o armazenamento de blocos do Stalwart, olhar para a configuração não
// prova nada: prova que a configuração mudou. O que interessa é uma mensagem a entrar, o Stalwart a
// escrever o original, e esse original a aparecer cifrado no balde certo. Isso só se vê com correio
// a sério.
//
// Corre DENTRO do cluster porque as portas de correio não são alcançáveis de fora da infraestrutura.
package main

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	log.Printf("=== corrida %s ===", time.Now().UTC().Format(time.RFC3339))

	host := env("NOTIFY_SMTP_HOST")
	port := envOr("NOTIFY_SMTP_PORT", "465")
	user := env("NOTIFY_SMTP_USER")
	pwd := env("NOTIFY_SMTP_PASSWORD")
	para := envOr("PROVA_PARA", user)

	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		log.Fatal(err)
	}
	marker := hex.EncodeToString(b)

	// Corpo grande o suficiente para o bloco ser reconhecível no balde e para valer a pena cifrar
	// em mais do que um pedaço de conteúdo.
	body := strings.Repeat("linha de enchimento para a prova do gateway de cifra. ", 300)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: prova do gateway %s\r\n\r\n"+
		"Marca unica: %s\r\n%s\r\n", user, para, marker, marker, body)

	// Onde LIGAR pode não ser o mesmo que o nome do certificado. De dentro do VPS, sair até ao IP
	// público e voltar a entrar (hairpin) costuma não funcionar — liga-se ao contentor pelo nome
	// interno e mantém-se `mail.beskos.com` como nome do certificado, para a verificação continuar
	// a valer.
	address := envOr("PROVA_ADDR", host+":"+port)
	log.Printf("sending to %s connecting to %s (certificate from %s), marker %s", para, address, host, marker)

	// TLS IMPLÍCITO na 465. O smtp.SendMail do Go fala StartTLS, que aqui não serve: o 587 deste
	// Stalwart larga a ligação (docs/DEPLOY.md), e a 465 espera TLS desde o primeiro byte. Por isso
	// a ligação é feita à mão.
	conn, err := tls.Dial("tcp", address, &tls.Config{ServerName: host})
	if err != nil {
		log.Fatalf("ligar por TLS a %s: %v", address, err)
	}
	defer conn.Close()

	cli, err := smtp.NewClient(conn, host)
	if err != nil {
		log.Fatalf("smtp: %v", err)
	}
	defer cli.Quit()

	if err := cli.Auth(smtp.PlainAuth("", user, pwd, host)); err != nil {
		log.Fatalf("autenticar: %v", err)
	}
	if err := cli.Mail(user); err != nil {
		log.Fatalf("MAIL FROM: %v", err)
	}
	if err := cli.Rcpt(para); err != nil {
		log.Fatalf("RCPT TO: %v", err)
	}
	w, err := cli.Data()
	if err != nil {
		log.Fatalf("DATA: %v", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		log.Fatalf("write: %v", err)
	}
	if err := w.Close(); err != nil {
		log.Fatalf("fechar: %v", err)
	}

	log.Printf("enviada")
	log.Printf("look now: a PUT in the blobgw log, and a new object under mail/ in the bucket")
	log.Printf("marker to look for in the decrypted content: %s", marker)
	for {
		time.Sleep(time.Hour)
	}
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
