package registry

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	u "github.com/cnrancher/hangar/pkg/utils"
	"github.com/sirupsen/logrus"
)

// RunCommandFunc specifies the custom function to run command for registry.
//
// Only used for testing purpose!
var RunCommandFunc u.RunCmdFuncType = nil

var (
	DockerPath = "docker"
	SkopeoPath = "skopeo"
)

const (
	skopeoInsGuideURL = "https://github.com/containers/skopeo/blob/main/install.md"
)

// SelfCheck checks the registry related commands is installed or not
func SelfCheckSkopeo() error {
	// ensure skopeo is installed
	path, err := exec.LookPath("skopeo")
	if err != nil {
		logrus.Warnf("skopeo not found, please install by refer: %q",
			skopeoInsGuideURL)
		return fmt.Errorf("%w", err)
	}
	SkopeoPath = path
	var buff bytes.Buffer
	cmd := exec.Command(path, "-v")
	cmd.Stdout = &buff
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("'skopeo -v': %w", err)
	}
	logrus.Infof(strings.TrimSpace(buff.String()))

	return nil
}

func SelfCheckBuildX() error {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return u.ErrDockerNotFound
	}
	DockerPath = dockerPath

	var execCommandFunc u.RunCmdFuncType
	if RunCommandFunc != nil {
		execCommandFunc = RunCommandFunc
	} else {
		execCommandFunc = u.DefaultRunCommandFunc
	}

	// ensure docker-buildx is installed
	if err = execCommandFunc(dockerPath, nil, nil, "buildx"); err != nil {
		if strings.Contains(err.Error(), "is not a docker command") {
			return u.ErrDockerBuildxNotFound
		}
	}
	logrus.Debugf("docker buildx found")

	return nil
}

func SelfCheckDocker() error {
	// check docker
	path, err := exec.LookPath("docker")
	if err != nil {
		return u.ErrDockerNotFound
	}
	DockerPath = path
	logrus.Debugf("docker found: %v", path)

	return nil
}
