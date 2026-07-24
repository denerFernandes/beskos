// Package sms abstrai o envio de SMS (códigos de MFA). Interface genérica para trocar de
// provedor (Twilio, etc.) só por config — como internal/ai. Chaves só no backend (lei §10).
package sms

import (
	"context"
	"fmt"
	"log"
	"os"
)

// Sender envia uma mensagem curta para um telefone (E.164). Implementado por cada provedor.
type Sender interface {
	Send(ctx context.Context, phone, message string) error
}

// LogSender apenas registra o envio no log (dev/testes). NUNCA usar em produção. Não loga o corpo
// (contém o código MFA) nem o telefone completo — só confirma o envio, com o telefone mascarado.
type LogSender struct{}

func (LogSender) Send(_ context.Context, phone, _ string) error {
	log.Printf("[SMS:log] enviado para %s (corpo omitido: contém código MFA)", maskPhone(phone))
	return nil
}

// maskPhone mostra só os 2 últimos dígitos: "+5511999998888" → "***8888"? mantemos 4 finais.
func maskPhone(p string) string {
	if len(p) <= 4 {
		return "****"
	}
	return "***" + p[len(p)-4:]
}

// Config seleciona o provedor por ambiente.
type Config struct {
	Provider string // "log" (default) | "sns" (Amazon SNS)
}

func FromEnv() Config { return Config{Provider: getenv("SMS_PROVIDER", "log")} }

// New constrói o Sender. Único ponto que conhece provedores concretos (adicionar = novo case).
// "sns": Amazon SNS (Publish direto ao número). Envs: SNS_REGION (fallback SES_REGION) +
// AWS_ACCESS_KEY_ID/SECRET (as mesmas do SES) + SMS_SENDER_ID opcional. Falha fechado no boot
// se faltar credencial — melhor não arrancar do que MFA por SMS silenciosamente morto.
func New(cfg Config) (Sender, error) {
	switch cfg.Provider {
	case "log", "":
		return LogSender{}, nil
	case "sns":
		region := getenv("SNS_REGION", os.Getenv("SES_REGION"))
		key, secret := os.Getenv("AWS_ACCESS_KEY_ID"), os.Getenv("AWS_SECRET_ACCESS_KEY")
		if region == "" || key == "" || secret == "" {
			return nil, fmt.Errorf("sms: provider sns requires SNS_REGION (or SES_REGION) + AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY")
		}
		return NewSNS(region, key, secret, os.Getenv("SMS_SENDER_ID")), nil
	default:
		return nil, fmt.Errorf("sms: provedor desconhecido %q", cfg.Provider)
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
