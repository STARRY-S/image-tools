package signv2

import (
	"context"

	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/sign"
)

type Signer struct {
	ro   options.RootOptions
	ko   options.KeyOpts
	opts options.SignOptions
}

type SignerOption struct {
	options.RootOptions
	options.KeyOpts
	options.SignOptions
}

func NewSigner(o *SignerOption) *Signer {
	s := Signer{
		ro:   o.RootOptions,
		ko:   o.KeyOpts,
		opts: o.SignOptions,
	}
	return &s
}

func SignCmd(
	ctx context.Context,
	ro *options.RootOptions,
	ko options.KeyOpts,
	signOpts options.SignOptions,
	imgs []string,
) error {
	return nil
}

func (s *Signer) Sign(ctx context.Context, image string) error {
	return sign.SignCmd(&s.ro, s.ko, s.opts, []string{image})
}
