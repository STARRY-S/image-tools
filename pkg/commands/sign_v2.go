package commands

import (
	"fmt"
	"time"

	"github.com/cnrancher/hangar/pkg/hangar"
	"github.com/cnrancher/hangar/pkg/utils"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/generate"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/options"
	"github.com/sigstore/cosign/v2/cmd/cosign/cli/sign"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

const (
	DefaultOIDCIssuerURL = "https://oauth2.sigstore.dev/auth"
	DefaultRekorURL      = "https://rekor.sigstore.dev"
)

type signOpts struct {
	file                    string
	key                     string
	cert                    string
	recursive               bool
	skipConfirmation        bool
	tlogUpload              bool
	issueCertificate        bool
	signContainerIdentity   string
	skUse                   bool
	skSlot                  string
	recordCreationTimestamp bool
	rekorURL                string
	oidcIssuer              string
	oidcClientID            string
	oidcProvider            string
}

type signCmd struct {
	*baseCmd
	*signOpts
}

func newSignCmd() *signCmd {
	cc := &signCmd{
		signOpts: new(signOpts),
	}
	cc.baseCmd = newBaseCmd(&cobra.Command{
		Use:     "sign",
		Short:   "hangar sign --key cosign.key <IMAGE>",
		Long:    ``,
		Example: ``,
		PreRun: func(cmd *cobra.Command, args []string) {
			utils.SetupLogrus(cc.hideLogTime)
			if cc.debug {
				logrus.SetLevel(logrus.DebugLevel)
				logrus.Debugf("Debug output enabled")
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := cc.prepareHangar()
			if err != nil {
				return err
			}
			// logrus.Infof("Signing images in %q with sigstore priv-key %q.",
			// 	cc.file, cc.privateKey)
			// if err := run(h); err != nil {
			// 	return err
			// }
			return nil
		},
	})

	flags := cc.baseCmd.cmd.Flags()
	flags.StringVarP(&cc.file, "file", "f", "", "image list file")
	flags.SetAnnotation("file", cobra.BashCompFilenameExt, []string{"txt"})
	flags.SetAnnotation("file", cobra.BashCompOneRequiredFlag, []string{""})
	flags.StringVar(&cc.key, "key", "",
		"path to the private key file, KMS URI or Kubernetes Secret")
	flags.SetAnnotation("key", cobra.BashCompFilenameExt, []string{})
	flags.StringVar(&cc.cert, "certificate", "",
		"path to the X.509 certificate in PEM format to include in the OCI Signature")
	flags.SetAnnotation("certificate", cobra.BashCompFilenameExt, []string{"cert"})
	flags.BoolVarP(&cc.recursive, "recursive", "r", false,
		"if a multi-arch image is specified, additionally sign each discrete image")
	flags.BoolVarP(&cc.skipConfirmation, "auto-yes", "y", false,
		"skip confirmation prompts for non-destructive operations")
	flags.BoolVar(&cc.tlogUpload, "tlog-upload", true,
		"whether or not to upload to the tlog")
	flags.StringVar(&cc.oidcIssuer, "oidc-issuer", DefaultOIDCIssuerURL,
		"OIDC provider to be used to issue ID token")
	flags.StringVar(&cc.oidcClientID, "oidc-client-id", "sigstore",
		"OIDC client ID for application")
	flags.StringVar(&cc.oidcProvider, "oidc-provider", "",
		"Specify the provider to get the OIDC token from (Optional). If unset, all options will be tried. "+
			"Options include: [spiffe, google, github-actions, filesystem, buildkite-agent]")
	flags.StringVar(&cc.rekorURL, "rekor-url", DefaultRekorURL,
		"address of rekor STL server")

	return cc
}

func (cc *signCmd) prepareHangar() (hangar.Hangar, error) {
	ko := options.KeyOpts{
		KeyRef:   cc.key,
		PassFunc: generate.GetPass,
		Sk:       cc.skUse,
		Slot:     cc.skSlot,
		// FulcioURL:                cc.fu,
		// IDToken:                  o.Fulcio.IdentityToken,
		// FulcioAuthFlow:           o.Fulcio.AuthFlow,
		// InsecureSkipFulcioVerify: o.Fulcio.InsecureSkipFulcioVerify,
		RekorURL:     cc.rekorURL,
		OIDCIssuer:   cc.oidcIssuer,
		OIDCClientID: cc.oidcClientID,
		// OIDCClientSecret:         cc.,
		// OIDCRedirectURL:      cc.oidc,
		// OIDCDisableProviders: o.OIDC.DisableAmbientProviders,
		OIDCProvider:     cc.oidcProvider,
		SkipConfirmation: cc.skipConfirmation,
		// TSAClientCACert:                o.TSAClientCACert,
		// TSAClientCert:                  o.TSAClientCert,
		// TSAClientKey:                   o.TSAClientKey,
		// TSAServerName:                  o.TSAServerName,
		// TSAServerURL:                   o.TSAServerURL,
		// IssueCertificateForExistingKey: o.IssueCertificate,
	}
	err := sign.SignCmd(
		&options.RootOptions{
			Timeout: time.Minute * 30,
		},
		ko,
		options.SignOptions{
			Key:                   cc.key,
			Cert:                  cc.cert,
			Upload:                true,
			Recursive:             cc.recursive,
			SkipConfirmation:      cc.skipConfirmation,
			TlogUpload:            cc.tlogUpload,
			IssueCertificate:      cc.issueCertificate,
			SignContainerIdentity: cc.signContainerIdentity,
			SecurityKey: options.SecurityKeyOptions{
				Use:  cc.skUse,
				Slot: cc.skSlot,
			},
			RecordCreationTimestamp: cc.recordCreationTimestamp,
			Rekor: options.RekorOptions{
				URL: cc.rekorURL,
			},
			OIDC: options.OIDCOptions{
				Issuer:   cc.oidcIssuer,
				ClientID: cc.oidcClientID,
				Provider: cc.oidcProvider,
			},
		},
		[]string{"registry.hxstarrys.me:5000/library/alpine@sha256:c10f729849a3b03cbf222e2220245dd44c39a06d444aa32cc30a35c4c1aba59d"})
	if err != nil {
		return nil, fmt.Errorf("signing attachment for image: %w", err)
	}

	return nil, nil
}
