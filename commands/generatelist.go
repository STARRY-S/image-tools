package commands

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/cnrancher/hangar/pkg/config"
	"github.com/cnrancher/hangar/pkg/rancher/chartimages"
	"github.com/cnrancher/hangar/pkg/rancher/listgenerator"
	"github.com/cnrancher/hangar/pkg/utils"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

type generateListCmd struct {
	baseCmd

	isRPMGC        bool
	rancherVersion string
	generator      *listgenerator.Generator
}

func newGenerateListCmd() *generateListCmd {
	cc := &generateListCmd{}

	cc.baseCmd.cmd = &cobra.Command{
		Use:   "generate-list",
		Short: "Generate list according Rancher version",
		Long: `'generate-list' generates an image-list from KDM data and Chart repositories used by Rancher.

Generate image list by just specifying Rancher version:

    hangar generate-list --rancher="v2.7.0-ent"

Generate image-list from custom cloned chart repos & KDM data.json file.

    hangar generate-list --rancher="v2.7.0-ent"  \
        --chart="./chart-repo-dir"               \
        --system-chart="./system-chart-repo-dir" \
        --kdm="./kdm-data.json"`,
		// Example: `  hangar generate-list --rancher="v2.7.0-ent"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initializeFlagsConfig(cmd, config.DefaultProvider)

			if config.GetBool("debug") {
				logrus.SetLevel(logrus.DebugLevel)
			}

			if err := cc.setupFlags(); err != nil {
				return err
			}
			if err := cc.prepareGenerator(); err != nil {
				return err
			}
			if err := cc.run(); err != nil {
				return err
			}
			if err := cc.finish(); err != nil {
				return err
			}

			return nil
		},
	}
	cc.cmd.Flags().StringP("registry", "", "", "customize the registry URL of generated image list")
	cc.cmd.Flags().StringP("kdm", "", "", "KDM file path or URL")
	cc.cmd.Flags().StringP("output", "o", "", "output generated image list file (default \"[RANCHER_VERSION]-images.txt\")")
	cc.cmd.Flags().StringP("output-linux", "", "", "generate linux image list")
	cc.cmd.Flags().StringP("output-windows", "", "", "generate windows image list")
	cc.cmd.Flags().StringP("output-source", "", "", "generate image list with image source")
	cc.cmd.Flags().StringP("rancher", "", "", "rancher version (semver with 'v' prefix) "+
		"(use '-ent' suffix to distinguish with RPM GC) (required)")
	cc.cmd.Flags().BoolP("dev", "", false, "switch to dev branch/URL of charts & KDM data")
	cc.cmd.Flags().StringSliceP("chart", "", nil, "cloned chart repo path (URL is not supported)")
	cc.cmd.Flags().StringSliceP("system-chart", "", nil, "cloned system chart repo path (URL is not supported)")

	return cc
}

func (cc *generateListCmd) setupFlags() error {
	configData := config.DefaultProvider.Get("")
	b, _ := json.MarshalIndent(configData, "", "  ")
	logrus.Debugf("Config: %v", string(b))

	if config.GetString("rancher") == "" {
		return fmt.Errorf("rancher version not specified, use '--rancher' to specify the rancher version")
	}

	cc.rancherVersion = config.GetString("rancher")
	if !strings.HasPrefix(cc.rancherVersion, "v") {
		cc.rancherVersion = "v" + cc.rancherVersion
	}
	if strings.Contains(cc.rancherVersion, "-ent") {
		logrus.Infof("Set to RPM GC")
		cc.isRPMGC = true
		v := strings.Split(cc.rancherVersion, "-ent")
		cc.rancherVersion = v[0]
		config.Set("rancher", cc.rancherVersion)
	}
	if !semver.IsValid(cc.rancherVersion) {
		return fmt.Errorf("%q is not valid semver", cc.rancherVersion)
	}

	if config.GetString("output") == "" {
		output := cc.rancherVersion + "-images.txt"
		config.Set("output", output)
	}

	return nil
}

func (cc *generateListCmd) prepareGenerator() error {
	cc.generator = &listgenerator.Generator{
		RancherVersion: cc.rancherVersion,
		MinKubeVersion: "",
		ChartsPaths:    make(map[string]chartimages.ChartRepoType),
		ChartURLs: make(map[string]struct {
			Type   chartimages.ChartRepoType
			Branch string
		}),
	}
	switch {
	case utils.SemverMajorMinorEqual(cc.rancherVersion, "v2.5"):
		cc.generator.MinKubeVersion = ""
	case utils.SemverMajorMinorEqual(cc.rancherVersion, "v2.6"):
		cc.generator.MinKubeVersion = "v1.21.0"
	case utils.SemverMajorMinorEqual(cc.rancherVersion, "v2.7"):
		cc.generator.MinKubeVersion = "v1.21.0"
	}
	kdm := config.GetString("kdm")
	if kdm != "" {
		if _, err := url.ParseRequestURI(kdm); err != nil {
			cc.generator.KDMPath = kdm
		} else {
			cc.generator.KDMURL = kdm
		}
	}

	charts := config.GetStringSlice("chart")
	if len(charts) != 0 {
		for _, chart := range charts {
			if _, err := url.ParseRequestURI(chart); err != nil {
				logrus.Debugf("add chart path to load images: %q", chart)
				cc.generator.ChartsPaths[chart] = chartimages.RepoTypeDefault
			} else {
				// cc.generator.ChartURLs[chart] = struct {
				// 	Type   chartimages.ChartRepoType
				// 	Branch string
				// }{
				// 	Type:   chartimages.RepoTypeDefault,
				// 	Branch: "", // use default branch
				// }
				return fmt.Errorf("chart url is not supported, please provide the cloned chart path")
			}
		}
	}
	systemCharts := config.GetStringSlice("system-chart")
	if len(systemCharts) != 0 {
		for _, chart := range systemCharts {
			if _, err := url.ParseRequestURI(chart); err != nil {
				logrus.Debugf("add system chart path to load images: %q", chart)
				cc.generator.ChartsPaths[chart] = chartimages.RepoTypeSystem
			} else {
				return fmt.Errorf("chart url is not supported, please provide the cloned chart path")
			}
		}
	}
	dev := config.GetBool("dev")
	if kdm == "" && len(charts) == 0 && len(systemCharts) == 0 {
		if dev {
			logrus.Info("using dev branch")
		} else {
			logrus.Info("using release branch")
		}
		if cc.isRPMGC {
			logrus.Debugf("add RPM GC charts & KDM to generate list")
			addRPMCharts(cc.rancherVersion, cc.generator, dev)
			addRPMGCCharts(cc.rancherVersion, cc.generator, dev)
			addRPMGCSystemCharts(cc.rancherVersion, cc.generator, dev)
			addRPM_GC_KDM(cc.rancherVersion, cc.generator, dev)
		} else {
			logrus.Debugf("add RPM charts & KDM to generate list")
			addRPMCharts(cc.rancherVersion, cc.generator, dev)
			addRPMSystemCharts(cc.rancherVersion, cc.generator, dev)
			addRPM_KDM(cc.rancherVersion, cc.generator, dev)
		}
	}

	return nil
}

func (cc *generateListCmd) run() error {
	return cc.generator.Generate()
}

func (cc *generateListCmd) finish() error {
	// merge windows images and linux images into one file
	imagesLinuxSet := map[string]bool{}
	imagesWindowsSet := map[string]bool{}
	var imageSources = make([]string, 0,
		len(cc.generator.GeneratedLinuxImages)+
			len(cc.generator.GeneratedWindowsImages))

	registry := config.GetString("registry")
	for image := range cc.generator.GeneratedLinuxImages {
		imgWithRegistry := image
		if registry != "" {
			imgWithRegistry = utils.ConstructRegistry(image, registry)
		}
		imagesLinuxSet[imgWithRegistry] = true
		imageSources = append(imageSources,
			fmt.Sprintf("%s %s", imgWithRegistry,
				getSourcesList(cc.generator.GeneratedLinuxImages[image])))
	}
	for image := range cc.generator.GeneratedWindowsImages {
		imgWithRegistry := image
		if registry != "" {
			imgWithRegistry = utils.ConstructRegistry(image, registry)
		}
		imagesWindowsSet[imgWithRegistry] = true
		imageSources = append(imageSources,
			fmt.Sprintf("%s %s", imgWithRegistry,
				getSourcesList(cc.generator.GeneratedWindowsImages[image])))
	}
	var imagesAllSet = map[string]bool{}
	var imagesLinuxList = make([]string, 0, len(imagesLinuxSet))
	var imagesWindowsList = make([]string, 0, len(imagesWindowsSet))
	for img := range imagesLinuxSet {
		imagesLinuxList = append(imagesLinuxList, img)
		imagesAllSet[img] = true
	}
	for img := range imagesWindowsSet {
		imagesWindowsList = append(imagesWindowsList, img)
		imagesAllSet[img] = true
	}
	var imagesList = make([]string, 0,
		len(imagesLinuxSet)+len(imagesWindowsSet))
	for img := range imagesAllSet {
		imagesList = append(imagesList, img)
	}
	sort.Strings(imagesList)
	sort.Strings(imagesLinuxList)
	sort.Strings(imagesWindowsList)
	sort.Strings(imageSources)
	output := config.GetString("output")
	if output != "" {
		err := utils.SaveSlice(output, imagesList)
		if err != nil {
			logrus.Error(err)
		}
	}
	outputLinux := config.GetString("output-linux")
	if outputLinux != "" {
		err := utils.SaveSlice(outputLinux, imagesLinuxList)
		if err != nil {
			logrus.Error(err)
		}
	}
	outputWindows := config.GetString("output-windows")
	if outputWindows != "" {
		err := utils.SaveSlice(outputWindows, imagesWindowsList)
		if err != nil {
			logrus.Error(err)
		}
	}
	outputSource := config.GetString("output-source")
	if outputSource != "" {
		err := utils.SaveSlice(outputSource, imageSources)
		if err != nil {
			logrus.Error(err)
		}
	}
	return nil
}

func getSourcesList(imageSources map[string]bool) string {
	var sources []string
	for source := range imageSources {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return strings.Join(sources, ",")
}
