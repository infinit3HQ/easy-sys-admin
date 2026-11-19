package fleet

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/crypto"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/store"
)

func TestResealRewritesOldCiphertextAndSkipsCorrupt(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	owner := signIn(t, s, "reseal")
	oldKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("o", 32)))
	newKey := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))
	old, err := crypto.NewBox(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := old.Seal([]byte("ssh-secret"), keyContext(owner.AccountID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.db.Omit("Account").Create(&store.AccountKey{
		AccountID: owner.AccountID, PublicKey: "ssh-ed25519 AAAA", Fingerprint: "SHA256:test",
		PrivateKeyEnc: sealed, CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	other := signIn(t, s, "reseal-bad")
	if err := s.db.Omit("Account").Create(&store.AccountKey{
		AccountID: other.AccountID, PublicKey: "x", Fingerprint: "y",
		PrivateKeyEnc: "not-a-secret", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	both, err := crypto.NewBox(newKey, oldKey)
	if err != nil {
		t.Fatal(err)
	}
	s.box = both
	dry, err := s.Reseal(ctx, true)
	if err != nil || dry.Classes[0].Left != 1 || dry.Classes[0].Failed != 1 || dry.Classes[0].Resealed != 0 {
		t.Fatalf("dry: %+v %v", dry, err)
	}
	var still string
	s.db.Model(&store.AccountKey{}).Where("account_id = ?", owner.AccountID).Pluck("private_key_enc", &still)
	if still != sealed {
		t.Fatal("dry run wrote a secret")
	}
	rep, err := s.Reseal(ctx, false)
	if err != nil || rep.Classes[0].Resealed != 1 || rep.Classes[0].Failed != 1 || rep.Ready {
		t.Fatalf("reseal: %+v %v", rep, err)
	}
	s.db.Model(&store.AccountKey{}).Where("account_id = ?", owner.AccountID).Pluck("private_key_enc", &still)
	only, err := crypto.NewBox(newKey)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := only.Open(still, keyContext(owner.AccountID))
	if err != nil || string(pt) != "ssh-secret" {
		t.Fatalf("new key open: %q %v", pt, err)
	}
	again, err := s.Reseal(ctx, false)
	if err != nil || again.Classes[0].Resealed != 0 || again.Classes[0].Current != 1 {
		t.Fatalf("second run: %+v %v", again, err)
	}
}
