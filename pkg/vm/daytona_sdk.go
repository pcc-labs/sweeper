package vm

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/daytona/clients/sdk-go/pkg/daytona"
	"github.com/daytona/clients/sdk-go/pkg/options"
	"github.com/daytona/clients/sdk-go/pkg/types"
)

// daytonaCredentialsPresent reports whether the SDK can authenticate. The
// SDK reads the same variables; checking here gives a sweeper-shaped error
// before any API call.
func daytonaCredentialsPresent() bool {
	return os.Getenv("DAYTONA_API_KEY") != "" || os.Getenv("DAYTONA_JWT_TOKEN") != ""
}

// newSDKClient wraps the Daytona Go SDK as a sandboxClient.
func newSDKClient() (sandboxClient, error) {
	if !daytonaCredentialsPresent() {
		return nil, fmt.Errorf("the daytona VM backend needs DAYTONA_API_KEY (or DAYTONA_JWT_TOKEN with DAYTONA_ORGANIZATION_ID) on the host")
	}
	c, err := daytona.NewClient()
	if err != nil {
		return nil, fmt.Errorf("daytona client: %w", err)
	}
	return &sdkClient{c: c}, nil
}

type sdkClient struct{ c *daytona.Client }

func (s *sdkClient) Create(ctx context.Context, params types.SnapshotParams) (sandboxAPI, error) {
	sb, err := s.c.Create(ctx, params, options.WithTimeout(daytonaCreateTimeout))
	if err != nil {
		return nil, err
	}
	return &sdkSandbox{sb: sb}, nil
}

func (s *sdkClient) Get(ctx context.Context, nameOrID string) (sandboxAPI, error) {
	sb, err := s.c.Get(ctx, nameOrID)
	if err != nil {
		return nil, err
	}
	return &sdkSandbox{sb: sb}, nil
}

type sdkSandbox struct{ sb *daytona.Sandbox }

func (s *sdkSandbox) ID() string { return s.sb.ID }

// Exec disables the server-side execution limit: a claude run can outlast
// any fixed timeout, and the caller's ctx already bounds the wait.
func (s *sdkSandbox) Exec(ctx context.Context, command, cwd string) (int, string, error) {
	resp, err := s.sb.Process.ExecuteCommand(ctx, command,
		options.WithCwd(cwd),
		options.WithExecuteTimeout(0),
	)
	if err != nil {
		return 0, "", err
	}
	return resp.ExitCode, resp.Result, nil
}

func (s *sdkSandbox) Upload(ctx context.Context, src io.Reader, remotePath string) error {
	return s.sb.FileSystem.UploadFileStream(ctx, src, remotePath)
}

func (s *sdkSandbox) Delete(ctx context.Context) error {
	return s.sb.Delete(ctx)
}
