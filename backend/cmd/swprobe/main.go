// swprobe diz o que o Stalwart está a fazer na porta HTTP dele.
//
// PORQUÊ. Depois de um `ReloadSettings`, o `POST /jmap/` passou a ser fechado com reset enquanto o
// TCP continuava a aceitar. Isso tem várias causas possíveis e uma delas é decisiva: o listener ter
// passado a esperar TLS. Adivinhar custa um reinício de cada vez; perguntar custa um pedido.
//
// Tenta, pela ordem: ligar, falar HTTP simples, e falar TLS. O que responder diz o que ele espera.
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	log.SetFlags(0)
	target := envOr("SW_ADDR", "stalwart:8080")
	log.Printf("=== sonda %s a %s ===", time.Now().UTC().Format(time.RFC3339), target)

	// 1. O TCP aceita?
	c, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		log.Fatalf("1. TCP: %v", err)
	}
	c.Close()
	log.Printf("1. TCP: aceita")

	// 2. HTTP simples.
	log.Printf("2. HTTP simples: %s", attempt(func() (string, error) { return request("http://" + target + "/") }))

	// 3. TLS. Se ele responder aqui e não em 2, o listener passou a ser HTTPS — e todo o resto da
	//    infraestrutura que lhe fala em HTTP simples deixa de funcionar sem mudar nada nela.
	log.Printf("3. TLS: %s", attempt(func() (string, error) {
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", target,
			&tls.Config{InsecureSkipVerify: true})
		if err != nil {
			return "", err
		}
		defer conn.Close()
		e := conn.ConnectionState()
		names := "sem certificado"
		if len(e.PeerCertificates) > 0 {
			names = e.PeerCertificates[0].Subject.CommonName
		}
		fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", target)
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("handshake OK (certificado %s) → %s", names, strings.TrimSpace(line)), nil
	}))

	// 4. O caminho que a aplicação usa mesmo.
	log.Printf("4. POST /jmap/ em HTTP simples: %s", attempt(func() (string, error) {
		resp, err := (&http.Client{Timeout: 15 * time.Second}).Post("http://"+target+"/jmap/",
			"application/json", strings.NewReader(`{"using":[],"methodCalls":[]}`))
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		return resp.Status, nil
	}))

	log.Printf("=== end ===")
	for {
		time.Sleep(time.Hour)
	}
}

func attempt(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		return "FALHOU: " + err.Error()
	}
	return s
}

func request(url string) (string, error) {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return resp.Status, nil
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
