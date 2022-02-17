package podman_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/buildbuddy-io/buildbuddy/enterprise/server/remote_execution/container"
	"github.com/buildbuddy-io/buildbuddy/enterprise/server/remote_execution/containers/podman"
	"github.com/buildbuddy-io/buildbuddy/server/testutil/testfs"
	"github.com/buildbuddy-io/buildbuddy/server/util/log"
	"github.com/stretchr/testify/assert"

	repb "github.com/buildbuddy-io/buildbuddy/proto/remote_execution"
)

func makeTempDirWithWorldTxt(t *testing.T) string {
	dir := testfs.MakeTempDir(t)

	f, err := os.Create(fmt.Sprintf("%s/world.txt", dir))
	log.Infof("created file at dir %q", dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString("world")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Second)
	return dir
}

func TestHelloWorld(t *testing.T) {
	ctx := context.Background()
	rootDir := makeTempDirWithWorldTxt(t)
	cmd := &repb.Command{
		EnvironmentVariables: []*repb.Command_EnvironmentVariable{
			&repb.Command_EnvironmentVariable{Name: "GREETING", Value: "Hello"},
		},
		Arguments: []string{"sh", "-c", fmt.Sprintf("printf \"$GREETING $(cat %s/world.txt)!\"", rootDir)},
	}
	// Need to give enough time to download the Docker image.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	podman := podman.NewPodmanCommandContainer("docker.io/library/busybox", rootDir)
	workDir := testfs.MakeDirAll(t, rootDir, "work")
	result := podman.Run(ctx, cmd, workDir, container.PullCredentials{})

	if result.Error != nil {
		t.Fatal(result.Error)
	}
	assert.Regexp(t, "^\\(podman\\)", result.CommandDebugString, "sanity check: command should be run bare")
	assert.Equal(t, "Hello world!", string(result.Stdout),
		"stdout should equal 'Hello world!' ('$GREETING' env var should be replaced with 'Hello', and "+
			"tempfile containing 'world' should be readable.)",
	)
	assert.Empty(t, string(result.Stderr), "stderr should be empty")
	assert.Equal(t, 0, result.ExitCode, "should exit with success")
}
