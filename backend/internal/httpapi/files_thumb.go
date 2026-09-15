package httpapi

import (
	"image"
	"image/color"
	_ "image/gif" // registra o decoder GIF
	"image/jpeg"
	_ "image/png" // registra o decoder PNG
	"net/http"
	"strings"

	"github.com/beskosorg/beskos/backend/internal/db"
)

// Favoritos e miniaturas (thumbnails) de imagens. As miniaturas usam só a stdlib (image/*) — sem
// dependência nova (CLAUDE.md §2.5): decodifica, reduz por média de caixa e reencoda em JPEG.

const thumbMax = 256 // maior lado da miniatura, em pixels

// handleStarFile marca/desmarca um ficheiro como favorito do dono.
func (s *Server) handleStarFile(w http.ResponseWriter, r *http.Request) {
	f, ok := s.fileOwned(w, r)
	if !ok {
		return
	}
	mu := mailUserFrom(r.Context())
	var req struct {
		Starred bool `json:"starred"`
	}
	if !decode(w, r, &req) {
		return
	}
	rec, err := s.q.SetFileStarred(r.Context(), db.SetFileStarredParams{ID: f.ID, OwnerUserID: ownerPg(mu), Starred: req.Starred})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "storage.record_failed")
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// isThumbable diz se o nome tem extensão de imagem que a stdlib decodifica (jpeg/png/gif).
func isThumbable(name string) bool {
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".jpg"),
		strings.HasSuffix(strings.ToLower(name), ".jpeg"),
		strings.HasSuffix(strings.ToLower(name), ".png"),
		strings.HasSuffix(strings.ToLower(name), ".gif"):
		return true
	}
	return false
}

// handleThumbnail serve uma miniatura JPEG de um ficheiro de imagem. Não-imagens → 415 (o frontend
// cai no ícone por tipo). A miniatura é imutável por blob, então marca Cache-Control longo.
func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	if !s.storageReady(w) {
		return
	}
	f, ok := s.fileForDownload(w, r)
	if !ok {
		return
	}
	if !isThumbable(f.Name) {
		writeErr(w, http.StatusUnsupportedMediaType, "storage.not_image")
		return
	}
	obj, err := s.storage.Get(r.Context(), f.BlobKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "storage.download_failed")
		return
	}
	defer obj.Close()
	src, _, err := image.Decode(obj)
	if err != nil {
		writeErr(w, http.StatusUnsupportedMediaType, "storage.not_image")
		return
	}
	thumb := downscale(src, thumbMax)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_ = jpeg.Encode(w, thumb, &jpeg.Options{Quality: 80})
}

// downscale reduz a imagem para caber em `max` no maior lado, por média de caixa (cada pixel de
// destino é a média da região correspondente na origem). Cada pixel de origem é visitado uma vez.
func downscale(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}
	nw, nh := w, h
	if w >= h {
		nw = max
		nh = h * max / w
	} else {
		nh = max
		nw = w * max / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0, sy1 := b.Min.Y+y*h/nh, b.Min.Y+(y+1)*h/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		for x := 0; x < nw; x++ {
			sx0, sx1 := b.Min.X+x*w/nw, b.Min.X+(x+1)*w/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			var rs, gs, bs, as, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA()
					rs += uint64(pr)
					gs += uint64(pg)
					bs += uint64(pb)
					as += uint64(pa)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			dst.Set(x, y, color.RGBA{uint8((rs / n) >> 8), uint8((gs / n) >> 8), uint8((bs / n) >> 8), uint8((as / n) >> 8)})
		}
	}
	return dst
}
