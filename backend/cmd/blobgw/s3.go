package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"

	"mailin/backend/internal/storage"
)

// As quatro operações. Cada uma devolve o estado que escreveu, só para o registo.

// put cifra e guarda. O Stalwart manda o corpo inteiro com Content-Length — nunca multipart —, o
// que é o que permite cifrar em passagem, sem estado e sem disco.
func (g *gateway) put(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) int {
	size := r.ContentLength
	if size < 0 {
		s3Err(w, http.StatusLengthRequired, "MissingContentLength", "este gateway precisa de Content-Length")
		return http.StatusLengthRequired
	}
	defer r.Body.Close()

	// A assinatura cobre o resumo DECLARADO do corpo, não o corpo. Confere-se o corpo real em
	// passagem — sem o guardar em memória, que é o que permite um anexo grande passar por aqui.
	body := newChecker(r.Body, r.Header.Get("X-Amz-Content-Sha256"))

	// O tipo de conteúdo não é preservado: o objeto passa a ser opaco e dizer ao fornecedor que
	// aquilo é um `message/rfc822` devolvia-lhe parte do que a cifra acabou de esconder.
	if err := g.st.Put(ctx, key, body, size, ""); err != nil {
		log.Printf("blobgw: put %s: %v", key, err)
		s3Err(w, http.StatusInternalServerError, "InternalError", "falha a guardar")
		return http.StatusInternalServerError
	}

	// Só se sabe no fim. Um corpo que não corresponde ao que foi assinado não fica no
	// armazenamento: apaga-se o que acabou de se escrever antes de responder.
	if err := body.verify(); err != nil {
		log.Printf("blobgw: put %s rejected: %v", key, err)
		if err := g.st.Remove(context.WithoutCancel(ctx), key); err != nil {
			log.Printf("blobgw: and could not delete %s: %v", key, err)
		}
		s3Err(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch", "o corpo não corresponde ao resumo assinado")
		return http.StatusBadRequest
	}
	// O ETag tem de existir, mas NÃO pode ser o do objeto cifrado nem fingir ser o md5 do que o
	// cliente mandou. O Stalwart não o verifica (confirma a escrita com um HEAD, ver
	// verify_after_write); devolve-se a chave, que é estável e não mente sobre ser um md5.
	w.Header().Set("ETag", `"`+keyETag(key)+`"`)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK
}

// get devolve o conteúdo decifrado. Com Range, decifra só os blocos pedidos — é para isto que o
// formato é em blocos.
func (g *gateway) get(ctx context.Context, w http.ResponseWriter, r *http.Request, key string) int {
	interval := r.Header.Get("Range")
	if interval == "" {
		obj, err := g.st.GetSeek(ctx, key)
		if err != nil {
			return g.readErr(w, key, err)
		}
		defer obj.Close()
		total, err := obj.Seek(0, io.SeekEnd)
		if err == nil {
			_, err = obj.Seek(0, io.SeekStart)
		}
		if err != nil {
			s3Err(w, http.StatusInternalServerError, "InternalError", "falha a medir o objeto")
			return http.StatusInternalServerError
		}
		objectHeaders(w, obj, key, total)
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, obj); err != nil {
			log.Printf("blobgw: get %s interrupted: %v", key, err)
		}
		return http.StatusOK
	}

	sr, err := g.st.GetSeek(ctx, key)
	if err != nil {
		return g.readErr(w, key, err)
	}
	defer sr.Close()

	total, err := sr.Seek(0, io.SeekEnd)
	if err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "falha a medir o objeto")
		return http.StatusInternalServerError
	}
	start, end, err := readRange(interval, total)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", total))
		s3Err(w, http.StatusRequestedRangeNotSatisfiable, "InvalidRange", err.Error())
		return http.StatusRequestedRangeNotSatisfiable
	}
	if _, err := sr.Seek(start, io.SeekStart); err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "falha a posicionar")
		return http.StatusInternalServerError
	}

	objectHeaders(w, sr, key, total)
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	if _, err := io.CopyN(w, sr, end-start+1); err != nil {
		log.Printf("blobgw: get %s [%d-%d] interrupted: %v", key, start, end, err)
	}
	return http.StatusPartialContent
}

// head confirma que o objeto existe e diz o tamanho EM CLARO — que é o que o cliente espera, e não
// o do cifrado. O Stalwart usa isto logo a seguir a cada PUT (verify_after_write) para se defender
// de armazenamentos que confirmam escritas antes de as tornarem visíveis.
func (g *gateway) head(ctx context.Context, w http.ResponseWriter, key string) int {
	sr, err := g.st.GetSeek(ctx, key)
	if err != nil {
		return g.readErr(w, key, err)
	}
	defer sr.Close()

	total, err := sr.Seek(0, io.SeekEnd)
	if err != nil {
		s3Err(w, http.StatusInternalServerError, "InternalError", "falha a medir o objeto")
		return http.StatusInternalServerError
	}
	objectHeaders(w, sr, key, total)
	w.WriteHeader(http.StatusOK)
	return http.StatusOK
}

// cabecalhosDoObjeto escreve o que um cliente S3 espera de um GET ou HEAD. O Last-Modified não é
// enfeite: um cliente legítimo recusa a resposta sem ele (o minio-go apanhou isto no ensaio), e
// vem de borla na resposta que já se fez ao armazenamento.
func objectHeaders(w http.ResponseWriter, obj *storage.SeekObject, key string, total int64) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(total, 10))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("ETag", `"`+keyETag(key)+`"`)
	when, err := obj.ModTime()
	if err != nil {
		when = time.Now().UTC()
	}
	w.Header().Set("Last-Modified", when.UTC().Format(http.TimeFormat))
}

func (g *gateway) delete(ctx context.Context, w http.ResponseWriter, key string) int {
	if err := g.st.Remove(ctx, key); err != nil {
		log.Printf("blobgw: delete %s: %v", key, err)
		s3Err(w, http.StatusInternalServerError, "InternalError", "falha a apagar")
		return http.StatusInternalServerError
	}
	w.WriteHeader(http.StatusNoContent)
	return http.StatusNoContent
}

// erroLeitura traduz o erro do armazenamento. O 404 tem de passar como 404: o Stalwart distingue
// "não existe" (resposta legítima) de "avariou" (que ele repete com recuo).
func (g *gateway) readErr(w http.ResponseWriter, key string, err error) int {
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && (resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey") {
		s3Err(w, http.StatusNotFound, "NoSuchKey", "não existe")
		return http.StatusNotFound
	}
	log.Printf("blobgw: read %s: %v", key, err)
	s3Err(w, http.StatusInternalServerError, "InternalError", "falha a ler")
	return http.StatusInternalServerError
}

// lerRange interpreta `bytes=início-fim`, com as formas abertas dos dois lados. Devolve limites
// inclusivos, como o HTTP manda.
func readRange(header string, total int64) (int64, int64, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, errf("intervalo %q não é em bytes", header)
	}
	spec := strings.TrimPrefix(header, "bytes=")
	if strings.Contains(spec, ",") {
		return 0, 0, errf("intervalos múltiplos não são suportados")
	}
	from, until, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, errf("intervalo %q malformado", spec)
	}

	switch {
	case from == "" && until == "":
		return 0, 0, errf("intervalo vazio")
	case from == "": // sufixo: os últimos N bytes
		n, err := strconv.ParseInt(until, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, errf("sufixo %q inválido", until)
		}
		if n > total {
			n = total
		}
		return total - n, total - 1, nil
	}

	start, err := strconv.ParseInt(from, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, errf("início %q inválido", from)
	}
	if start >= total {
		return 0, 0, errf("início %d além do fim (%d)", start, total)
	}
	end := total - 1
	if until != "" {
		if end, err = strconv.ParseInt(until, 10, 64); err != nil {
			return 0, 0, errf("fim %q inválido", until)
		}
		if end > total-1 {
			end = total - 1
		}
	}
	if end < start {
		return 0, 0, errf("fim %d antes do início %d", end, start)
	}
	return start, end, nil
}

// etagDaChave dá um ETag estável e honesto. As chaves do Stalwart já são o resumo do conteúdo, por
// isso isto identifica a versão sem fingir ser o md5 de bytes que ninguém tem.
func keyETag(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 {
		return key[i+1:]
	}
	return key
}

type xmlErr struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func s3Err(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_ = xml.NewEncoder(w).Encode(xmlErr{Code: code, Message: message})
}

func errf(format string, args ...any) error { return fmt.Errorf(format, args...) }
