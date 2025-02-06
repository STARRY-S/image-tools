package signv2

import (
	"context"
	"testing"

	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
)

func Test_Sign(t *testing.T) {
	t.Skip()
	s := NewSigner(&SignerOption{
		RootOptions: options.RootOptions{},
		KeyOpts: options.KeyOpts{
			KeyRef: "../../../sigstore.key",
		},
		SignOptions: options.SignOptions{},
	})
	err := s.Sign(context.TODO(), "10.1.1.2:5000/library/alpine")
	if err != nil {
		t.Error(err)
	}
}
