package azure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const accountCommandTimeout = 30 * time.Second

// AccountInfo describes the Azure identity visible inside Rover's isolated az
// configuration directory.
type AccountInfo struct {
	LoggedIn         bool
	SubscriptionID   string
	SubscriptionName string
	TenantID         string
	User             string
}

type azRunner func(ctx context.Context, env []string, inherit bool, args ...string) ([]byte, error)

var defaultAZRunner azRunner = func(ctx context.Context, env []string, inherit bool, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = env
	cmd.Stdin = os.Stdin
	if inherit {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return nil, cmd.Run()
	}
	return cmd.CombinedOutput()
}

func (c *Client) commandEnv() ([]string, error) {
	env, err := c.state.Env()
	if err != nil {
		return nil, fmt.Errorf("resolve Azure config directory: %w", err)
	}
	dir, err := c.state.AzureConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve Azure config directory: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create Azure config directory %s: %w", dir, err)
	}
	return env, nil
}

func (c *Client) runAzure(ctx context.Context, inherit bool, args ...string) ([]byte, error) {
	env, err := c.commandEnv()
	if err != nil {
		return nil, err
	}
	runner := c.runAZ
	if runner == nil {
		runner = defaultAZRunner
	}
	return runner(ctx, env, inherit, args...)
}

// Account returns the active account in Rover's Azure CLI context. A normal
// az "not logged in" result is represented as LoggedIn=false rather than an
// error so status can give a useful next step.
func (c *Client) Account() (AccountInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), accountCommandTimeout)
	defer cancel()

	out, err := c.runAzure(ctx, false, "account", "show", "-o", "json")
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return AccountInfo{}, commandError("az account show", out, err)
		}
		detail := strings.ToLower(string(out))
		if strings.Contains(detail, "az login") || strings.Contains(detail, "not logged in") {
			return AccountInfo{LoggedIn: false}, nil
		}
		return AccountInfo{}, commandError("az account show", out, err)
	}

	// Resolve a configured subscription explicitly instead of relying on az's
	// process-global default inside this otherwise isolated context.
	if subscription := c.state.AzureSubscription(); subscription != "" {
		out, err = c.runAzure(ctx, false, "account", "show", "--subscription", subscription, "-o", "json")
		if err != nil {
			return AccountInfo{}, commandError("select configured Azure subscription", out, err)
		}
	}

	var account struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		TenantID string `json:"tenantId"`
		User     struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &account); err != nil {
		return AccountInfo{}, fmt.Errorf("parse az account show output: %w", err)
	}
	return AccountInfo{
		LoggedIn:         true,
		SubscriptionID:   account.ID,
		SubscriptionName: account.Name,
		TenantID:         account.TenantID,
		User:             account.User.Name,
	}, nil
}

// Login authenticates Rover's isolated Azure CLI context. Device-code flow is
// the default for reliable terminal/headless use. A configured tenant scopes
// the login, and a configured subscription is selected after authentication.
func (c *Client) Login(deviceCode bool) error {
	args := []string{"login"}
	if deviceCode {
		args = append(args, "--use-device-code")
	}
	if tenant := c.state.AzureSettings().Tenant; tenant != "" {
		args = append(args, "--tenant", tenant)
	}
	if out, err := c.runAzure(context.Background(), true, args...); err != nil {
		return commandError("az login", out, err)
	}
	if subscription := c.state.AzureSubscription(); subscription != "" {
		if out, err := c.runAzure(context.Background(), true, "account", "set", "--subscription", subscription); err != nil {
			return commandError("select configured Azure subscription", out, err)
		}
	}
	return nil
}

// Logout removes credentials only from Rover's effective Azure CLI context.
func (c *Client) Logout() error {
	if out, err := c.runAzure(context.Background(), true, "logout"); err != nil {
		return commandError("az logout", out, err)
	}
	return nil
}

// BicepAvailable checks Bicep using Rover's Azure CLI context.
func (c *Client) BicepAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), accountCommandTimeout)
	defer cancel()
	_, err := c.runAzure(ctx, false, "bicep", "version")
	return err == nil
}

// InstallBicep installs Bicep using Rover's Azure CLI context.
func (c *Client) InstallBicep() error {
	out, err := c.runAzure(context.Background(), true, "bicep", "install")
	if err != nil {
		return commandError("az bicep install", out, err)
	}
	return nil
}

func commandError(action string, output []byte, err error) error {
	detail := strings.TrimSpace(string(output))
	if detail != "" {
		return fmt.Errorf("%s: %w: %s", action, err, detail)
	}
	return fmt.Errorf("%s: %w", action, err)
}
