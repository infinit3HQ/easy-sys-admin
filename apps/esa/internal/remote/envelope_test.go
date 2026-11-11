package remote

import (
	"strings"
	"testing"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/hostops/proto"
)

func TestDecodeEnvelopeRejectsOtherProtocol(t *testing.T) {
	body := []byte(`{"ok":true,"protocol":99,"data":{}}`)
	err := decodeEnvelope(ExecResult{Stdout: body}, &struct{}{})
	if err == nil || !strings.Contains(err.Error(), "protocol 99") {
		t.Fatalf("got %v", err)
	}
	ok := []byte(`{"ok":true,"protocol":1,"data":{"version":"x"}}`)
	var v proto.VersionInfo
	if err := decodeEnvelope(ExecResult{Stdout: ok}, &v); err != nil {
		t.Fatal(err)
	}
}
