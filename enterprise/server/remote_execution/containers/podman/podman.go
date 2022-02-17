package podman

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildbuddy-io/buildbuddy/enterprise/server/remote_execution/commandutil"
	"github.com/buildbuddy-io/buildbuddy/enterprise/server/remote_execution/container"
	"github.com/buildbuddy-io/buildbuddy/server/interfaces"
	"github.com/buildbuddy-io/buildbuddy/server/util/background"
	"github.com/buildbuddy-io/buildbuddy/server/util/log"
	"github.com/buildbuddy-io/buildbuddy/server/util/random"
	"github.com/buildbuddy-io/buildbuddy/server/util/status"

	repb "github.com/buildbuddy-io/buildbuddy/proto/remote_execution"
)

var (
	containerFinalizationTimeout = 10 * time.Second
)

// podmanCommandContainer containerizes a command's execution using a Podman container.
// between containers.
type podmanCommandContainer struct {
	image       string
	hostRootDir string
	// workDir is the path to the workspace directory mounted to the container.
	workDir string
}

func NewPodmanCommandContainer(image, hostRootDir string) container.CommandContainer {
	return &podmanCommandContainer{
		image:       image,
		hostRootDir: hostRootDir,
	}
}

func (c *podmanCommandContainer) Run(ctx context.Context, command *repb.Command, workDir string, creds container.PullCredentials) *interfaces.CommandResult {
	result := &interfaces.CommandResult{
		CommandDebugString: fmt.Sprintf("(podman) %s", command.GetArguments()),
		ExitCode:           commandutil.NoExitCode,
	}
	containerName, err := generateContainerName()
	if err != nil {
		result.Error = status.UnavailableErrorf("failed to generate podman container name: %s", err)
		return result
	}
	if err := c.PullImageIfNecessary(ctx); err != nil {
		result.Error = status.UnavailableErrorf("failed to pull docker image: %s", err)
		return result
	}

	podmanRunArgs := []string{
		"--hostname",
		"localhost",
		"--workdir",
		workDir,
		"--name",
		containerName,
		"--rm",
		"--volume",
		fmt.Sprintf(
			"%s:%s",
			filepath.Join(c.hostRootDir, filepath.Base(workDir)),
			workDir,
		),
	}

	for _, envVar := range command.GetEnvironmentVariables() {
		podmanRunArgs = append(podmanRunArgs, "--env", fmt.Sprintf("%s=%s", envVar.GetName(), envVar.GetValue()))
	}
	podmanRunArgs = append(podmanRunArgs, c.image)
	podmanRunArgs = append(podmanRunArgs, command.Arguments...)
	result = runPodman(ctx, "run", workDir, nil, nil, podmanRunArgs...)
	if exitedCleanly := result.ExitCode >= 0; !exitedCleanly {
		err = killContainerIfRunning(ctx, containerName)
	}
	if err != nil {
		log.Printf("WARNING: Failed to shut down docker container: %s\n", err.Error())
	}
	return result
}

func (c *podmanCommandContainer) Create(ctx context.Context, workDir string) error {
	c.workDir = workDir
	return nil
}

func (c *podmanCommandContainer) Exec(ctx context.Context, cmd *repb.Command, stdin io.Reader, stdout io.Writer) *interfaces.CommandResult {
	return runPodman(ctx, "run" /*workDir=*/, "", stdin, stdout, cmd.Arguments...)
}

func (c *podmanCommandContainer) IsImageCached(ctx context.Context) (bool, error) { return false, nil }
func (c *podmanCommandContainer) PullImage(ctx context.Context, creds container.PullCredentials) error {
	return nil
}
func (c *podmanCommandContainer) Start(ctx context.Context) error   { return nil }
func (c *podmanCommandContainer) Remove(ctx context.Context) error  { return nil }
func (c *podmanCommandContainer) Pause(ctx context.Context) error   { return nil }
func (c *podmanCommandContainer) Unpause(ctx context.Context) error { return nil }

func (c *podmanCommandContainer) Stats(ctx context.Context) (*container.Stats, error) {
	return &container.Stats{}, nil
}

func runPodman(ctx context.Context, subCommand string, workDir string, stdin io.Reader, stdout io.Writer, args ...string) *interfaces.CommandResult {
	command := []string{"/opt/homebrew/bin/podman", subCommand}

	command = append(command, args...)
	result := commandutil.Run(ctx, &repb.Command{Arguments: command}, workDir, stdin, stdout)
	log.Infof("debug string: %q", result.CommandDebugString)
	return result
}

func generateContainerName() (string, error) {
	suffix, err := random.RandomString(20)
	if err != nil {
		return "", err
	}
	return "buildbuddy_exec_" + suffix, nil
}

func (r *podmanCommandContainer) PullImageIfNecessary(ctx context.Context) error {
	// Try to avoid the `pull` command which results in a network roundtrip.
	listResult := runPodman(ctx, "images", "", nil, nil, "--filter=reference="+r.image, "--format={{.ID}}")
	if listResult.Error != nil {
		return listResult.Error
	}
	if strings.TrimSpace(string(listResult.Stdout)) != "" {
		// Found at least one image matching the ref; `docker run` should succeed
		// without pulling the image.
		return nil
	}
	pullResult := runPodman(ctx, "pull", "", nil, nil, r.image)
	if pullResult.Error != nil {
		return pullResult.Error
	}
	return nil
}

func killContainerIfRunning(ctx context.Context, containerName string) error {
	ctx, cancel := background.ExtendContextForFinalization(ctx, containerFinalizationTimeout)
	defer cancel()

	result := runPodman(ctx, "kill", "", nil, nil, containerName)
	if result.Error != nil {
		return result.Error
	}
	if result.ExitCode == 0 || strings.Contains(string(result.Stderr), "No such container: "+containerName) {
		return nil
	}
	return status.UnknownErrorf("podman kill failed: %s", string(result.Stderr))
}
