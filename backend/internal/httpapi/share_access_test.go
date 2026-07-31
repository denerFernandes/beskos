package httpapi

import (
	"testing"

	"mailin/backend/internal/db"
)

// shareAccess decide o acesso pelo MELHOR entre a partilha direta e a de organização. É a peça que
// torna "qualquer pessoa na organização" um acesso real, não só um rótulo — por isso tem teste.
func TestShareAccess(t *testing.T) {
	sh := func(principal, level string) db.StorageShare {
		return db.StorageShare{Principal: principal, Level: level}
	}
	cases := []struct {
		name               string
		shares             []db.StorageShare
		email              string
		wantHas, wantWrite bool
	}{
		{"sem partilhas", nil, "ana@x.pt", false, false},
		{"partilha direta leitor", []db.StorageShare{sh("ana@x.pt", "leitor")}, "ana@x.pt", true, false},
		{"partilha direta editor", []db.StorageShare{sh("ana@x.pt", "editor")}, "ana@x.pt", true, true},
		{"partilha de organização leitor dá acesso a qualquer um", []db.StorageShare{sh(orgSharePrincipal, "leitor")}, "bob@x.pt", true, false},
		{"partilha de organização editor", []db.StorageShare{sh(orgSharePrincipal, "editor")}, "bob@x.pt", true, true},
		{"editor direto vence leitor de organização", []db.StorageShare{sh(orgSharePrincipal, "leitor"), sh("ana@x.pt", "editor")}, "ana@x.pt", true, true},
		{"partilha de outra pessoa não me dá acesso", []db.StorageShare{sh("outro@x.pt", "editor")}, "ana@x.pt", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			write, has := shareAccess(c.shares, c.email)
			if has != c.wantHas || write != c.wantWrite {
				t.Errorf("shareAccess = (write=%v, has=%v), quero (write=%v, has=%v)", write, has, c.wantWrite, c.wantHas)
			}
		})
	}
}
