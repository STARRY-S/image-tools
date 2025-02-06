package signv2

import (
	"context"
	"crypto"
	"testing"

	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/verify"
)

func Test_Validate(t *testing.T) {
	v := NewValidator(&ValidatorOption{
		VerifyCommand: verify.VerifyCommand{
			CertVerifyOptions: options.CertVerifyOptions{
				CertIdentity:   "https://github.com/rancher/rancher-prime/.github/workflows/release.yml@refs/tags/v2.10.2",
				CertOidcIssuer: "https://token.actions.githubusercontent.com",
			},
			CheckClaims:   true,
			HashAlgorithm: crypto.SHA256,
			// IgnoreTlog:    true,
		},
	})
	err := v.Validate(context.TODO(), "registry.rancher.com/rancher/rancher:v2.10.2")
	if err != nil {
		t.Error(err)
	}
}
