package kdmimages

import (
	"fmt"
	"strings"

	u "github.com/cnrancher/image-tools/pkg/utils"
	rketypes "github.com/rancher/rke/types"
	"github.com/sirupsen/logrus"
)

type SystemImages struct {
	RancherVersion    string
	RkeSysImages      map[string]rketypes.RKESystemImages
	LinuxSvcOptions   map[string]rketypes.KubernetesServicesOptions
	WindowsSvcOptions map[string]rketypes.KubernetesServicesOptions
	RancherVersions   map[string]rketypes.K8sVersionInfo

	LinuxInfo   *VersionInfo
	WindowsInfo *VersionInfo

	// map[image][source]bool
	LinuxImageSet   map[string]map[string]bool
	WindowsImageSet map[string]map[string]bool
}

func (s *SystemImages) GetImages() error {
	if s.RancherVersion == "" ||
		s.RkeSysImages == nil ||
		s.LinuxSvcOptions == nil ||
		s.WindowsSvcOptions == nil ||
		s.RancherVersions == nil {
		return fmt.Errorf("GetImages: SystemImages not initialized")
	}
	if s.LinuxImageSet == nil {
		s.LinuxImageSet = make(map[string]map[string]bool)
	}
	if s.WindowsImageSet == nil {
		s.WindowsImageSet = make(map[string]map[string]bool)
	}

	if err := s.getK8sVersionInfo(); err != nil {
		return err
	}

	logrus.Infof("Generating KDM system images...")
	if err := fetchImages(s.LinuxInfo, s.LinuxImageSet); err != nil {
		return err
	}

	if err := fetchImages(s.WindowsInfo, s.WindowsImageSet); err != nil {
		return err
	}
	logrus.Infof("Finished generating KDM system images")

	return nil
}

func fetchImages(
	versionInfo *VersionInfo,
	imageSet map[string]map[string]bool,
) error {
	if versionInfo == nil || len(versionInfo.RKESystemImages) <= 0 {
		return nil
	}
	collectionImagesList := []interface{}{versionInfo.RKESystemImages}
	images, err := flatImagesFromCollections(collectionImagesList...)
	if err != nil {
		return fmt.Errorf("fetchImages: %w", err)
	}
	for _, image := range images {
		u.AddSourceToImage(imageSet, image, "system")
	}
	return nil
}

func flatImagesFromCollections(
	cols ...interface{},
) (images []string, err error) {
	for _, col := range cols {
		colObj := map[string]interface{}{}
		if err := u.ToObj(col, &colObj); err != nil {
			return []string{}, err
		}
		images = append(images, fetchImagesFromCollection(colObj)...)
	}
	return images, nil
}

func fetchImagesFromCollection(obj map[string]interface{}) (images []string) {
	for _, v := range obj {
		switch t := v.(type) {
		case string:
			images = append(images, t)
		case map[string]interface{}:
			images = append(images, fetchImagesFromCollection(t)...)
		}
	}
	return images
}

func (s *SystemImages) getK8sVersionInfo() error {
	linuxInfo := newVersionInfo()
	windowsInfo := newVersionInfo()
	s.LinuxInfo = linuxInfo
	s.WindowsInfo = windowsInfo

	maxVersionForMajorK8sVersion := map[string]string{}
	for k8sVersion := range s.RkeSysImages {
		rancherVersionInfo, ok := s.RancherVersions[k8sVersion]
		if ok && toIgnoreForAllK8s(rancherVersionInfo, s.RancherVersion) {
			continue
		}
		majorVersion := getTagMajorVersion(k8sVersion)
		majorVersionInfo, ok := s.RancherVersions[majorVersion]
		if ok && toIgnoreForK8sCurrent(majorVersionInfo, s.RancherVersion) {
			continue
		}
		curr, ok := maxVersionForMajorK8sVersion[majorVersion]
		res, err := u.SemverCompare(k8sVersion, curr)
		if err != nil || !ok || res > 0 {
			maxVersionForMajorK8sVersion[majorVersion] = k8sVersion
		}
	}
	for majorVersion, k8sVersion := range maxVersionForMajorK8sVersion {
		sysImgs, exist := s.RkeSysImages[k8sVersion]
		if !exist {
			continue
		}
		// windows has been supported since v1.14,
		// the following logic would not find `< v1.14` service options
		if svcOptions, exist := s.WindowsSvcOptions[majorVersion]; exist {
			// only keep the related images for windows
			windowsSysImgs := rketypes.RKESystemImages{
				NginxProxy:                sysImgs.NginxProxy,
				CertDownloader:            sysImgs.CertDownloader,
				KubernetesServicesSidecar: sysImgs.KubernetesServicesSidecar,
				Kubernetes:                sysImgs.Kubernetes,
				WindowsPodInfraContainer:  sysImgs.WindowsPodInfraContainer,
			}

			windowsInfo.RKESystemImages[k8sVersion] = windowsSysImgs
			windowsInfo.KubernetesServicesOptions[k8sVersion] = svcOptions
		}
		if svcOptions, exist := s.LinuxSvcOptions[majorVersion]; exist {
			// clean the unrelated images for linux
			sysImgs.WindowsPodInfraContainer = ""

			linuxInfo.RKESystemImages[k8sVersion] = sysImgs
			linuxInfo.KubernetesServicesOptions[k8sVersion] = svcOptions
		}
	}

	return nil
}

func getTagMajorVersion(tag string) string {
	splitTag := strings.Split(tag, ".")
	if len(splitTag) < 2 {
		return ""
	}
	return strings.Join(splitTag[:2], ".")
}

type VersionInfo struct {
	RKESystemImages           map[string]rketypes.RKESystemImages
	KubernetesServicesOptions map[string]rketypes.KubernetesServicesOptions
}

func newVersionInfo() *VersionInfo {
	return &VersionInfo{
		RKESystemImages:           map[string]rketypes.RKESystemImages{},
		KubernetesServicesOptions: map[string]rketypes.KubernetesServicesOptions{},
	}
}

func toIgnoreForAllK8s(
	rancherVersionInfo rketypes.K8sVersionInfo,
	rancherVersion string,
) bool {
	if rancherVersionInfo.DeprecateRancherVersion != "" {
		res, err := u.SemverCompare(
			rancherVersion, rancherVersionInfo.DeprecateRancherVersion)
		if err != nil {
			logrus.Warn(err)
		} else if res >= 0 {
			return true
		}
	}
	if rancherVersionInfo.MinRancherVersion != "" {
		res, err := u.SemverCompare(
			rancherVersion, rancherVersionInfo.MinRancherVersion)
		if err != nil {
			logrus.Warn(err)
		} else if res < 0 {
			// only respect min versions, even if max is present
			// we need to support upgraded clusters
			return true
		}
	}
	return false
}

func toIgnoreForK8sCurrent(
	majorVersionInfo rketypes.K8sVersionInfo,
	rancherVersion string,
) bool {
	if majorVersionInfo.MaxRancherVersion != "" {
		res, err := u.SemverCompare(
			rancherVersion, majorVersionInfo.MaxRancherVersion)
		if err != nil {
			logrus.Warn(err)
		} else if res > 0 {
			// include in K8sVersionCurrent only if less then max version
			return true
		}
	}
	return false
}
