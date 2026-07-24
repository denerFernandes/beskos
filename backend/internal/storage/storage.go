// Package storage é o serviço de arquivos sobre MinIO (S3-compatível, self-hosted, brief §3).
// Isolamento por tenant via prefixo de chave (tenant_id/…) e checagem no control plane.
// Credenciais só no backend (lei §10).
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Service struct {
	client *minio.Client
	bucket string
}

// New conecta a um storage S3-compatível e garante o bucket. Funciona com MinIO (endpoint
// "minio:9000", region "") E com Cloudflare R2 (endpoint "<acct>.r2.cloudflarestorage.com",
// useSSL=true, region "auto"). No R2 o bucket é pré-criado no painel — se não conseguirmos criá-lo
// (o R2 restringe o CreateBucket por S3), assumimos que já existe em vez de rebentar.
func New(endpoint, accessKey, secretKey, bucket, region string, useSSL bool) (*Service, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: region, // R2 assina com "auto"; MinIO fica com "" (default)
	})
	if err != nil {
		return nil, err
	}
	s := &Service{client: c, bucket: bucket}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exists, err := c.BucketExists(ctx, bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		// No R2 os buckets criam-se no painel; o CreateBucket via S3 pode ser negado — não é fatal.
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{Region: region}); err != nil {
			return nil, fmt.Errorf("bucket %q não existe e não foi possível criá-lo (cria-o no painel do storage): %w", bucket, err)
		}
	}
	return s, nil
}

// Put envia um blob. key deve ser prefixado por tenant (ex.: "<tenant>/<hash>").
func (s *Service) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

// Get devolve o conteúdo do blob para streaming ao cliente.
func (s *Service) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// GetSeek devolve o objeto como ReadSeekCloser (o *minio.Object busca por Range/StatObject). Serve
// para o http.ServeContent honrar o cabeçalho Range — player de vídeo com seek (avançar/retroceder).
func (s *Service) GetSeek(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// Exists diz se o blob já existe (usado para dedup: não reenviar conteúdo idêntico).
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.StatusCode == 404 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Remove apaga um blob (usar só quando nenhum registro lógico aponta mais para ele).
func (s *Service) Remove(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
