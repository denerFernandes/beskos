// Package storage é o serviço de arquivos sobre MinIO (S3-compatível, self-hosted, brief §3).
// Isolamento por tenant via prefixo de chave (tenant_id/…) e checagem no control plane.
// Credenciais só no backend (lei §10).
package storage

import (
	"context"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Service struct {
	client *minio.Client
	bucket string
}

// New conecta ao MinIO e garante o bucket. endpoint ex.: "localhost:9000".
func New(endpoint, accessKey, secretKey, bucket string, useSSL bool) (*Service, error) {
	c, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
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
		if err := c.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, err
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
