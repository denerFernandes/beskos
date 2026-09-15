package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/beskosorg/beskos/backend/internal/assistant"
	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/office"
)

// assistantDocsRead lê o conteúdo de texto de um ficheiro do Drive do utilizador, com a versão
// corrente. A posse é a do dono (o mesmo filtro da interface): o assistente vê o que o utilizador
// vê, não mais (§6.2). O texto é o que o modelo lê antes de editar (§5.1).
func (s *Server) assistantDocsRead(ctx context.Context, id assistant.Identity, fileID string) (assistant.FileContent, error) {
	if s.storage == nil {
		return assistant.FileContent{}, fmt.Errorf("file storage is not configured")
	}
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("invalid file id")
	}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{
		ID: fid, OwnerUserID: pgtype.UUID{Bytes: id.UserID, Valid: true},
	})
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("file not found")
	}

	kind, editable := office.KindFromName(f.Name)
	fc := assistant.FileContent{
		ID: f.ID.String(), Name: f.Name, Version: f.Version, Hash: f.Hash, Editable: editable,
	}
	if !editable {
		// Binários (imagens, PDF) não se editam às cegas — só se substituem por um envio novo
		// (§5.1). Dizê-lo é melhor do que devolver bytes que o modelo interpretaria como lixo.
		fc.Kind = "other"
		return fc, nil
	}
	fc.Kind = kind

	rc, err := s.storage.Get(ctx, f.BlobKey)
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("read blob: %w", err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 20<<20))
	if err != nil {
		return assistant.FileContent{}, fmt.Errorf("read blob: %w", err)
	}
	text, err := office.ExtractText(kind, data)
	if err != nil {
		// O ficheiro tem a extensão de um Office mas não abre como tal. Não se inventa conteúdo:
		// devolve-se vazio, e o modelo vê que não há texto para editar.
		fc.Text = ""
		return fc, nil
	}
	fc.Text = text
	return fc, nil
}

// assistantDocsEdit grava uma versão NOVA de um ficheiro, construída em Go a partir do plano, mas
// só se a versão corrente ainda for expectVersion (§5.1, regra 3). As cinco regras de §5.1:
//
//  1. ler antes de escrever — garantida na ferramenta (o motor exige a marca de leitura);
//  2. cada alteração é uma versão nova — SnapshotFileVersion + incremento, nunca no lugar;
//  3. editar em cima da versão que se leu — UpdateStorageFileContentIfVersion, condicional;
//  4. permissão real — o filtro por owner_user_id, o mesmo da interface;
//  5. dizer o que mudou — o resumo volta no cartão de ação (feito na ferramenta).
func (s *Server) assistantDocsEdit(ctx context.Context, id assistant.Identity, fileID string, plan office.Plan, expectVersion int32) (assistant.DriveHit, error) {
	if s.storage == nil {
		return assistant.DriveHit{}, fmt.Errorf("file storage is not configured")
	}
	fid, err := uuid.Parse(fileID)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("invalid file id")
	}
	owner := pgtype.UUID{Bytes: id.UserID, Valid: true}
	f, err := s.q.GetStorageFile(ctx, db.GetStorageFileParams{ID: fid, OwnerUserID: owner})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("file not found")
	}
	// Ficheiros que não são Office não se editam por aqui (§5.1): reescrever um binário às cegas é
	// perder o ficheiro.
	if _, ok := office.KindFromName(f.Name); !ok {
		return assistant.DriveHit{}, fmt.Errorf("this file type cannot be edited")
	}
	// A guarda de concorrência, primeiro barato: se a versão já não bate, nem se constrói o
	// ficheiro. A verificação real e atómica é a da consulta condicional, mais abaixo.
	if f.Version != expectVersion {
		return assistant.DriveHit{}, fmt.Errorf("the file changed since you read it; read it again")
	}

	data, err := office.Build(plan)
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("build file: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	blobKey := id.TenantID.String() + "/" + hash

	// Cota, com dedup por hash — a mesma da criação (§5).
	if exists, err := s.storage.Exists(ctx, blobKey); err != nil {
		return assistant.DriveHit{}, fmt.Errorf("storage backend")
	} else if !exists {
		plan := s.tenantPlanByID(ctx, id.TenantID)
		if used, _ := s.q.SumStorageBytes(ctx, id.TenantID); used+int64(len(data)) > plan.MaxStorageBytes {
			return assistant.DriveHit{}, fmt.Errorf("storage quota exceeded")
		}
		if err := s.storage.Put(ctx, blobKey, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
			return assistant.DriveHit{}, fmt.Errorf("upload failed")
		}
	}

	// REGRA 2: a versão corrente é fotografada ANTES de se sobrepor. É o que torna o engano
	// reversível — um clique a repor, não o ficheiro.
	_ = s.q.SnapshotFileVersion(ctx, db.SnapshotFileVersionParams{
		FileID: f.ID, TenantID: f.TenantID, BlobKey: f.BlobKey, Hash: f.Hash, Size: f.Size, Version: f.Version,
	})
	// REGRA 3: a escrita é CONDICIONAL à versão. Entre o GetStorageFile e aqui outra escrita pode
	// ter entrado; se entrou, esta não atualiza nada e recusa-se, em vez de perder o trabalho dela.
	updated, err := s.q.UpdateStorageFileContentIfVersion(ctx, db.UpdateStorageFileContentIfVersionParams{
		ID: f.ID, TenantID: f.TenantID, OwnerUserID: owner,
		BlobKey: blobKey, Hash: hash, Size: int64(len(data)), ExpectedVersion: expectVersion,
	})
	if err != nil {
		return assistant.DriveHit{}, fmt.Errorf("the file changed since you read it; read it again")
	}
	_ = s.q.InsertAudit(ctx, db.InsertAuditParams{
		TenantID: pgtype.UUID{Bytes: id.TenantID, Valid: true},
		Actor:    id.Email, Action: "file.edit", Target: f.Name,
	})
	return assistant.DriveHit{
		ID: updated.ID.String(), Name: updated.Name, Kind: "file",
		Size: updated.Size, Version: updated.Version,
	}, nil
}
