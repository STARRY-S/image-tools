package commands

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cnrancher/hangar/pkg/cmdconfig"
	"github.com/cnrancher/hangar/pkg/hangar"
	"github.com/cnrancher/hangar/pkg/utils"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type syncOpts struct {
	file        string
	arch        []string
	os          []string
	source      string
	destination string
	failed      string
	jobs        int
	timeout     time.Duration
	tlsVerify   bool
}

type syncCmd struct {
	*baseCmd
	*syncOpts
}

func newSyncCmd() *syncCmd {
	cc := &syncCmd{
		syncOpts: new(syncOpts),
	}
	cc.baseCmd = newBaseCmd(&cobra.Command{
		Use:   "sync -f IMAGE_LIST.txt -d SAVED_ARCHIVE.zip",
		Short: "Sync (append) images from registry server into local archive file",
		Long:  "",
		Example: `
hangar sync \
	-f IMAGE_LIST.txt \
	--arch amd64,arm64 \
	--os linux \
	-d SAVED_ARCHIVE.zip`,
		RunE: func(cmd *cobra.Command, args []string) error {
			initializeFlagsConfig(cmd, cmdconfig.DefaultProvider)
			if cc.baseCmd.debug {
				logrus.SetLevel(logrus.DebugLevel)
				logrus.Debugf("debug output enabled")
				logrus.Debugf("%v", utils.PrintObject(cmdconfig.Get("")))
			}

			h, err := cc.prepareHangar()
			if err != nil {
				return err
			}
			if err := run(h); err != nil {
				return err
			}
			return nil
		},
	})

	flags := cc.baseCmd.cmd.Flags()
	flags.StringVarP(&cc.file, "file", "f", "", "image list file")
	flags.StringSliceVarP(&cc.arch, "arch", "a", []string{"amd64", "arm64"}, "architecture list of images")
	flags.StringSliceVarP(&cc.os, "os", "", []string{"linux", "windows"}, "OS list of images")
	flags.StringVarP(&cc.source, "source", "s", "", "override the source registry in image list")
	flags.StringVarP(&cc.destination, "destination", "d", "saved-images.zip", "file name of the output saved images")
	flags.StringVarP(&cc.failed, "failed", "o", "save-failed.txt", "file name of the save failed image list")
	flags.IntVarP(&cc.jobs, "jobs", "j", 1, "worker number, copy images parallelly")
	flags.DurationVarP(&cc.timeout, "timeout", "", time.Minute*10, "timeout when save each images")
	flags.BoolVarP(&cc.tlsVerify, "tls-verify", "", true, "require HTTPS and verify certificates")

	addCommands(
		cc.cmd,
		newSyncValidateCmd(cc.syncOpts),
	)
	return cc
}

func (cc *syncCmd) prepareHangar() (hangar.Hangar, error) {
	if cc.file == "" {
		return nil, fmt.Errorf("image list not provided, use '--file' to specify the image list file")
	}
	if cc.debug {
		logrus.Infof("debug mode enabled, force worker number to 1")
		cc.jobs = 1
	} else {
		if cc.jobs > utils.MAX_WORKER_NUM || cc.jobs < utils.MIN_WORKER_NUM {
			logrus.Warnf("invalid worker num: %v, set to 1", cc.jobs)
			cc.jobs = 1
		}
	}

	_, err := os.Stat(cc.destination)
	if err != nil {
		return nil, fmt.Errorf("failed to stat %v: %w", cc.destination, err)
	}
	file, err := os.Open(cc.file)
	if err != nil {
		return nil, fmt.Errorf("failed to open %q: %v", cc.file, err)
	}
	images := []string{}
	sc := bufio.NewScanner(file)
	sc.Split(bufio.ScanLines)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "//") {
			continue
		}
		images = append(images, l)
	}
	if err := file.Close(); err != nil {
		logrus.Fatalf("failed to close %q: %v", cc.file, err)
	}

	s := hangar.NewSyncer(&hangar.SyncerOpts{
		CommonOpts: hangar.CommonOpts{
			Images:              images,
			Arch:                cc.arch,
			OS:                  cc.os,
			Variant:             nil,
			Timeout:             cc.timeout,
			Workers:             cc.jobs,
			SkipTlsVerify:       !cc.tlsVerify,
			FailedImageListName: cc.failed,
		},

		SourceRegistry:    cc.source,
		SharedBlobDirPath: "", // Use the default shared blob dir path.
		ArchiveName:       cc.destination,
	})
	logrus.Infof("Arch List: [%v]", strings.Join(cc.arch, ","))
	logrus.Infof("OS List: [%v]", strings.Join(cc.os, ","))

	return s, nil
}
