package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cnrancher/hangar/pkg/hangar/archive"
	"github.com/cnrancher/hangar/pkg/image/copy"
	"github.com/cnrancher/hangar/pkg/image/destination"
	"github.com/cnrancher/hangar/pkg/image/manifest"
	"github.com/cnrancher/hangar/pkg/image/types"
	"github.com/cnrancher/hangar/pkg/utils"
	"github.com/containers/common/pkg/retry"
	"github.com/sirupsen/logrus"

	copyv5 "github.com/containers/image/v5/copy"
	manifestv5 "github.com/containers/image/v5/manifest"
	signaturev5 "github.com/containers/image/v5/signature"
	alltransportsv5 "github.com/containers/image/v5/transports/alltransports"
	typesv5 "github.com/containers/image/v5/types"
	imgspecv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type CopyOptions struct {
	SigstorePrivateKey string
	SigstorePassphrase []byte
	RemoveSignatures   bool

	Destination *destination.Destination
	Set         types.FilterSet
	Policy      *signaturev5.Policy
}

func (s *Source) Copy(
	ctx context.Context,
	opts *CopyOptions,
) error {
	switch s.mime {
	case manifestv5.DockerV2ListMediaType:
		// manifest is docker image list
		num, err := s.copyDockerV2ListMediaType(ctx, opts)
		if err != nil {
			return err
		}
		logrus.Debugf("copied [%d] images", num)
		if num == 0 {
			return utils.ErrNoAvailableImage
		}
		return nil
	case imgspecv1.MediaTypeImageIndex:
		// manifest is oci image list
		num, err := s.copyMediaTypeImageIndex(ctx, opts)
		if err != nil {
			return err
		}
		logrus.Debugf("copied [%d] images", num)
		if num == 0 {
			return utils.ErrNoAvailableImage
		}
		return nil
	case manifestv5.DockerV2Schema2MediaType:
		// manifest is docker image schema2
		err := s.copyDockerV2Schema2MediaType(ctx, opts)
		if err != nil {
			return err
		}
		return nil
	case manifestv5.DockerV2Schema1MediaType,
		manifestv5.DockerV2Schema1SignedMediaType:
		// manifest is docker image schema1
		err := s.copyDockerV2Schema1MediaType(ctx, opts)
		if err != nil {
			return err
		}
		return nil
	case imgspecv1.MediaTypeImageManifest:
		// manifest is oci image
		err := s.copyMediaTypeImageManifest(ctx, opts)
		if err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unsupported MIME %q of image [%v]",
			s.mime, s.referenceName)
	}
}

func (s *Source) copyDockerV2ListMediaType(
	ctx context.Context,
	opts *CopyOptions,
) (int, error) {
	var copiedNum int
	var errs []error
	for _, m := range s.schema2List.Manifests {
		arch := m.Platform.Architecture
		osInfo := m.Platform.OS
		osVersion := m.Platform.OSVersion
		osFeatures := m.Platform.OSFeatures
		variant := m.Platform.Variant
		dig := m.Digest
		mime := m.MediaType

		// Skip image
		if !opts.Set.Allow(arch, osInfo, variant) {
			continue
		}
		if opts.SigstorePrivateKey == "" && opts.Destination.HaveDigest(m.Digest) {
			logrus.Debugf("dest already have digest %v, skip copy", m.Digest)
			copiedNum++
			continue
		}

		sourceRef, err := alltransportsv5.ParseImageName(fmt.Sprintf(
			"%s%s/%s/%s@%s",
			s.imageType.Transport(), s.registry, s.project, s.name, dig))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		destRef, err := opts.Destination.ReferenceMultiArch(
			osInfo, osVersion, arch, variant, dig.Encoded())
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = copyImage(ctx, &copyOptions{
			sigstorePrivateKey:           opts.SigstorePrivateKey,
			sigstorePrivateKeyPassphrase: opts.SigstorePassphrase,
			removeSignatures:             opts.RemoveSignatures,

			sourceRef:  sourceRef,
			destRef:    destRef,
			sourceCtx:  s.systemCtx,
			destCtx:    opts.Destination.SystemContext(),
			policy:     opts.Policy,
			sourceMIME: mime,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}

		inspector, err := manifest.NewInspector(ctx, &manifest.InspectorOption{
			Reference:     destRef,
			SystemContext: opts.Destination.SystemContext(),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("newInspector failed: %w", err))
			continue
		}
		defer inspector.Close()

		b, imageMIME, err := inspector.Raw(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("inspector.Raw failed: %w", err))
			continue
		}
		if needInspectConfig(osInfo, arch, variant, osVersion, osFeatures) {
			c, err := inspector.Config(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("inspector.Config failed: %w", err))
				continue
			}
			ociConfig := &imgspecv1.Image{}
			err = json.Unmarshal(c, ociConfig)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to unmarshal OCI config: %w", err))
				continue
			}
			if osVersion == "" {
				osVersion = ociConfig.OSVersion
			}
			if len(osFeatures) == 0 {
				osFeatures = ociConfig.OSFeatures
			}
			if variant == "" {
				variant = ociConfig.Variant
			}
		}
		manifestDigest, err := manifestv5.Digest(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to get digest: %w", err))
			continue
		}
		spec := archive.ImageSpec{
			Arch:       arch,
			OS:         osInfo,
			OSVersion:  osVersion,
			OSFeatures: osFeatures,
			Variant:    variant,
			MediaType:  mime,
			Layers:     nil,
			Config:     "",
			Digest:     manifestDigest,
		}
		switch imageMIME {
		case manifestv5.DockerV2Schema2MediaType:
			schema2, err := manifestv5.Schema2FromManifest(b)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			updateSpecDockerV2Schema2(&spec, schema2)
		// case imagemanifest.DockerV2Schema1MediaType,
		// 	imagemanifest.DockerV2Schema1SignedMediaType:
		// 	schema1, err := imagemanifest.Schema1FromManifest(b)
		// 	if err != nil {
		// 		errs = append(errs, err)
		// 		continue
		// 	}
		// 	updateSpecDockerV2Schema1(&spec, schema1)
		case imgspecv1.MediaTypeImageManifest:
			ociManifest := new(imgspecv1.Manifest)
			if err = json.Unmarshal(b, ociManifest); err != nil {
				errs = append(errs, err)
				continue
			}
			updateSpecImageManifest(&spec, ociManifest)
		default:
			errs = append(errs, fmt.Errorf("copied image mime unknow: %v", imageMIME))
			continue
		}
		err = s.recordCopiedImage(spec)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		copiedNum++
	}

	if len(errs) > 0 {
		err := errors.Join(errs...)
		return copiedNum, fmt.Errorf(
			"error occurred when copy image [%v] => [%v]: %w",
			s.referenceName, opts.Destination.ReferenceName(), err,
		)
	}
	return copiedNum, nil
}

func (s *Source) copyMediaTypeImageIndex(
	ctx context.Context,
	opts *CopyOptions,
) (int, error) {
	var copiedNum int
	var errs []error
	for _, m := range s.ociIndex.Manifests {
		mime := m.MediaType
		arch := m.Platform.Architecture
		osInfo := m.Platform.OS
		osVersion := m.Platform.OSVersion
		osFeatures := m.Platform.OSFeatures
		variant := m.Platform.Variant
		dig := m.Digest

		// Skip image
		if !opts.Set.Allow(arch, osInfo, variant) {
			continue
		}
		if opts.SigstorePrivateKey == "" && opts.Destination.HaveDigest(m.Digest) {
			logrus.Debugf("dest already have digest %v, skip copy", m.Digest)
			copiedNum++
			continue
		}

		sourceRef, err := alltransportsv5.ParseImageName(fmt.Sprintf(
			"%s%s/%s/%s@%s",
			s.imageType.Transport(), s.registry, s.project, s.name, dig))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		destRef, err := opts.Destination.ReferenceMultiArch(
			osInfo, osVersion, arch, variant, dig.Encoded())
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = copyImage(ctx, &copyOptions{
			sigstorePrivateKey:           opts.SigstorePrivateKey,
			sigstorePrivateKeyPassphrase: opts.SigstorePassphrase,
			removeSignatures:             opts.RemoveSignatures,

			sourceRef:  sourceRef,
			destRef:    destRef,
			sourceCtx:  s.systemCtx,
			destCtx:    opts.Destination.SystemContext(),
			policy:     opts.Policy,
			sourceMIME: mime,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}

		inspector, err := manifest.NewInspector(ctx, &manifest.InspectorOption{
			Reference:     destRef,
			SystemContext: opts.Destination.SystemContext(),
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("newInspector failed: %w", err))
			continue
		}
		defer inspector.Close()

		b, imageMIME, err := inspector.Raw(ctx)
		if err != nil {
			errs = append(errs, fmt.Errorf("inspector.Raw failed: %w", err))
			continue
		}
		if needInspectConfig(osInfo, arch, variant, osVersion, osFeatures) {
			c, err := inspector.Config(ctx)
			if err != nil {
				errs = append(errs, fmt.Errorf("inspector.Config failed: %w", err))
				continue
			}
			ociConfig := &imgspecv1.Image{}
			err = json.Unmarshal(c, ociConfig)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to unmarshal OCI config: %w", err))
				continue
			}
			if osVersion == "" {
				osVersion = ociConfig.OSVersion
			}
			if len(osFeatures) == 0 {
				osFeatures = ociConfig.OSFeatures
			}
			if variant == "" {
				variant = ociConfig.Variant
			}
		}
		manifestDigest, err := manifestv5.Digest(b)
		if err != nil {
			errs = append(errs, fmt.Errorf("imagemanifest.Digest failed: %w", err))
			continue
		}
		spec := archive.ImageSpec{
			Arch:       arch,
			OS:         osInfo,
			OSVersion:  osVersion,
			OSFeatures: osFeatures,
			Variant:    variant,
			MediaType:  mime,
			Layers:     nil,
			Config:     "",
			Digest:     manifestDigest,
		}
		switch imageMIME {
		case manifestv5.DockerV2Schema2MediaType:
			schema2, err := manifestv5.Schema2FromManifest(b)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			updateSpecDockerV2Schema2(&spec, schema2)
		// case imagemanifest.DockerV2Schema1MediaType,
		// 	imagemanifest.DockerV2Schema1SignedMediaType:
		// 	schema1, err := imagemanifest.Schema1FromManifest(b)
		// 	if err != nil {
		// 		errs = append(errs, err)
		// 		continue
		// 	}
		// 	updateSpecDockerV2Schema1(&spec, schema1)
		case imgspecv1.MediaTypeImageManifest:
			ociManifest := new(imgspecv1.Manifest)
			if err = json.Unmarshal(b, ociManifest); err != nil {
				errs = append(errs, err)
				continue
			}
			updateSpecImageManifest(&spec, ociManifest)
		default:
			errs = append(errs, fmt.Errorf("copied image mime unknow: %v", imageMIME))
			continue
		}
		err = s.recordCopiedImage(spec)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		copiedNum++
	}
	if len(errs) > 0 {
		err := errors.Join(errs...)
		return copiedNum, fmt.Errorf(
			"error occurred when copy image [%v] => [%v]: %w",
			s.referenceName, opts.Destination.ReferenceName(), err,
		)
	}
	return copiedNum, nil
}

func (s *Source) copyDockerV2Schema2MediaType(
	ctx context.Context,
	opts *CopyOptions,
) error {
	arch := s.ociConfig.Architecture
	osInfo := s.ociConfig.OS
	osVersion := s.ociConfig.OSVersion
	osFeatures := s.ociConfig.OSFeatures
	variant := s.ociConfig.Variant

	// skip image
	if !opts.Set.Allow(arch, osInfo, variant) {
		return utils.ErrNoAvailableImage
	}
	if opts.SigstorePrivateKey == "" && opts.Destination.HaveDigest(s.manifestDigest) {
		logrus.Debugf("dest already have digest %v, skip copy", s.manifestDigest)
		return nil
	}

	sourceRef, err := s.Reference()
	if err != nil {
		return err
	}
	destRef, err := opts.Destination.ReferenceMultiArch(
		osInfo, osVersion, arch, variant, s.manifestDigest.Encoded())
	if err != nil {
		return err
	}
	err = copyImage(ctx, &copyOptions{
		sigstorePrivateKey:           opts.SigstorePrivateKey,
		sigstorePrivateKeyPassphrase: opts.SigstorePassphrase,
		removeSignatures:             opts.RemoveSignatures,

		sourceRef:  sourceRef,
		destRef:    destRef,
		sourceCtx:  s.systemCtx,
		destCtx:    opts.Destination.SystemContext(),
		policy:     opts.Policy,
		sourceMIME: s.mime,
	})
	if err != nil {
		return err
	}
	spec := archive.ImageSpec{
		Arch:       arch,
		OS:         osInfo,
		OSVersion:  osVersion,
		OSFeatures: osFeatures,
		Variant:    variant,
		MediaType:  s.mime,
		Layers:     nil,
		Config:     s.schema2.ConfigDescriptor.Digest,
		Digest:     s.manifestDigest,
	}
	updateSpecDockerV2Schema2(&spec, s.schema2)
	return s.recordCopiedImage(spec)
}

func (s *Source) copyDockerV2Schema1MediaType(
	ctx context.Context,
	opts *CopyOptions,
) error {
	arch := s.imageInspectInfo.Architecture
	osInfo := s.imageInspectInfo.Os
	osVersion := ""
	variant := s.imageInspectInfo.Variant

	// skip image
	if !opts.Set.Allow(arch, osInfo, variant) {
		return utils.ErrNoAvailableImage
	}
	// Cannot detect whether the destination registry have Schema1 image here.
	// if opts.SigstorePrivateKey == "" && dest.HaveDigest(s.manifestDigest) {
	// 	logrus.Debugf("dest already have digest %v, skip copy", s.manifestDigest)
	// 	return nil
	// }

	sourceRef, err := s.Reference()
	if err != nil {
		return err
	}
	// Copy the images to temporary dir and rename its directory after copy.
	destRef, err := opts.Destination.ReferenceMultiArch(
		osInfo, osVersion, arch, variant, "UNKNOW")
	if err != nil {
		return err
	}
	err = copyImage(ctx, &copyOptions{
		sigstorePrivateKey:           opts.SigstorePrivateKey,
		sigstorePrivateKeyPassphrase: opts.SigstorePassphrase,
		removeSignatures:             opts.RemoveSignatures,

		sourceRef:  sourceRef,
		destRef:    destRef,
		sourceCtx:  s.systemCtx,
		destCtx:    opts.Destination.SystemContext(),
		policy:     opts.Policy,
		sourceMIME: s.mime,
	})
	if err != nil {
		return err
	}

	// Need to re-inspect the copied destination image digest
	// since the copied image mediaType was changed.
	inspector, err := manifest.NewInspector(ctx, &manifest.InspectorOption{
		Reference:     destRef,
		SystemContext: opts.Destination.SystemContext(),
	})
	if err != nil {
		return err
	}
	defer inspector.Close()

	b, mime, err := inspector.Raw(ctx)
	if err != nil {
		return err
	}
	manifestDigest, err := manifestv5.Digest(b)
	if err != nil {
		return fmt.Errorf("failed to get digest: %w", err)
	}
	schema2, err := manifestv5.Schema2FromManifest(b)
	if err != nil {
		return err
	}
	spec := archive.ImageSpec{
		Arch:      arch,
		OS:        osInfo,
		OSVersion: osVersion,
		Variant:   variant,
		MediaType: mime,
		Layers:    nil,
		Config:    schema2.ConfigDescriptor.Digest,
		Digest:    manifestDigest,
	}
	updateSpecDockerV2Schema2(&spec, schema2)
	if opts.Destination.Type() == types.TypeOci {
		o := filepath.Join(opts.Destination.Directory(), "UNKNOW")
		n := filepath.Join(opts.Destination.Directory(), manifestDigest.Encoded())
		err = os.Rename(o, n)
		if err != nil {
			return fmt.Errorf("failed to rename [%v] to [%v]: %w",
				o, n, err)
		}
	}
	return s.recordCopiedImage(spec)
}

func (s *Source) copyMediaTypeImageManifest(
	ctx context.Context,
	opts *CopyOptions,
) error {
	arch := s.ociConfig.Architecture
	osInfo := s.ociConfig.OS
	osVersion := s.ociConfig.OSVersion
	osFeatures := s.ociConfig.OSFeatures
	variant := s.ociConfig.Variant

	// Skip image
	if !opts.Set.Allow(arch, osInfo, variant) {
		return utils.ErrNoAvailableImage
	}

	sourceRef, err := s.Reference()
	if err != nil {
		return err
	}
	destRef, err := opts.Destination.ReferenceMultiArch(
		osInfo, osVersion, arch, variant, s.manifestDigest.Encoded())
	if err != nil {
		return err
	}
	err = copyImage(ctx, &copyOptions{
		sigstorePrivateKey:           opts.SigstorePrivateKey,
		sigstorePrivateKeyPassphrase: opts.SigstorePassphrase,
		removeSignatures:             opts.RemoveSignatures,

		sourceRef:  sourceRef,
		destRef:    destRef,
		sourceCtx:  s.systemCtx,
		destCtx:    opts.Destination.SystemContext(),
		policy:     opts.Policy,
		sourceMIME: s.mime,
	})
	if err != nil {
		return err
	}
	spec := archive.ImageSpec{
		Arch:       arch,
		OS:         osInfo,
		OSVersion:  osVersion,
		OSFeatures: osFeatures,
		Variant:    variant,
		MediaType:  s.mime,
		Layers:     nil,
		Config:     s.ociManifest.Config.Digest,
		Digest:     s.manifestDigest,
	}
	updateSpecImageManifest(&spec, s.ociManifest)
	return s.recordCopiedImage(spec)
}

func (s *Source) recordCopiedImage(image archive.ImageSpec) error {
	s.copiedList = append(s.copiedList, image)
	s.copiedArch[image.Arch] = true
	s.copiedOS[image.OS] = true
	return nil
}

func (s *Source) GetCopiedImage() *archive.Image {
	var (
		archies = make([]string, 0, len(s.copiedArch))
		oses    = make([]string, 0, len(s.copiedOS))
	)
	for a := range s.copiedArch {
		archies = append(archies, a)
	}
	for o := range s.copiedOS {
		oses = append(oses, o)
	}
	list := &archive.Image{
		Source:   fmt.Sprintf("%s/%s/%s", s.registry, s.project, s.name),
		Tag:      s.tag,
		ArchList: archies,
		OsList:   oses,
		Images:   s.copiedList,
	}
	return list
}

type copyOptions struct {
	sigstorePrivateKey           string
	sigstorePrivateKeyPassphrase []byte
	removeSignatures             bool

	sourceRef  typesv5.ImageReference
	destRef    typesv5.ImageReference
	sourceCtx  *typesv5.SystemContext
	destCtx    *typesv5.SystemContext
	policy     *signaturev5.Policy
	sourceMIME string
}

func copyImage(
	ctx context.Context,
	o *copyOptions,
) error {
	copyOpts := &copyv5.Options{
		RemoveSignatures:                 o.removeSignatures,
		SignBySigstorePrivateKeyFile:     o.sigstorePrivateKey,
		SignSigstorePrivateKeyPassphrase: o.sigstorePrivateKeyPassphrase,

		ReportWriter:         nil,
		SourceCtx:            utils.CopySystemContext(o.sourceCtx),
		DestinationCtx:       utils.CopySystemContext(o.destCtx),
		PreserveDigests:      true,
		MaxParallelDownloads: 3,
	}
	switch o.sourceMIME {
	case manifestv5.DockerV2Schema1MediaType,
		manifestv5.DockerV2Schema1SignedMediaType:
		// Docker schema1 image cannot preserve digest
		copyOpts.PreserveDigests = false
		// Convert image mediaType to DockerV2Schema2
		copyOpts.ForceManifestMIMEType = manifestv5.DockerV2Schema2MediaType
	case manifestv5.DockerV2ListMediaType,
		imgspecv1.MediaTypeImageIndex:
		return fmt.Errorf("copyImage: the image MIME type should be a single image, not %q",
			o.sourceMIME)
	}

	var err error
	copier := copy.NewCopier(&copy.CopierOption{
		Options: copyOpts,
		RetryOptions: &retry.Options{
			MaxRetry: 3,
			Delay:    time.Millisecond * 100,
		},

		SourceRef: o.sourceRef,
		DestRef:   o.destRef,
		Policy:    o.policy,
	})
	_, err = copier.Copy(ctx)
	return err
}

func updateSpecDockerV2Schema2(
	spec *archive.ImageSpec, schema2 *manifestv5.Schema2,
) *archive.ImageSpec {
	spec.Config = schema2.ConfigDescriptor.Digest
	for _, layer := range schema2.LayersDescriptors {
		if len(layer.URLs) != 0 {
			// The layer is from internet, ignore here.
			continue
		}
		spec.Layers = append(spec.Layers, layer.Digest)
	}
	return spec
}

// func updateSpecDockerV2Schema1(
// 	spec *archive.ImageSpec, schema1 *imagemanifest.Schema1,
// ) {
// 	layerDigestSet := map[digest.Digest]bool{}
// 	for _, layer := range schema1.FSLayers {
// 		layerDigestSet[layer.BlobSum] = true
// 	}
// 	for layer := range layerDigestSet {
// 		spec.Layers = append(spec.Layers, layer)
// 	}
// }

func updateSpecImageManifest(
	spec *archive.ImageSpec, ociManifest *imgspecv1.Manifest,
) {
	spec.Config = ociManifest.Config.Digest
	for _, layer := range ociManifest.Layers {
		if len(layer.URLs) != 0 {
			// The layer is from internet, ignore here.
			continue
		}
		spec.Layers = append(spec.Layers, layer.Digest)
	}
}

func needInspectConfig(
	osInfo, arch, variant, osVersion string, osFeatures []string,
) bool {
	switch osInfo {
	case "windows":
		return osVersion == "" || len(osFeatures) == 0
	case "linux":
		if arch == "arm" {
			return variant == ""
		}
	}

	return false
}
