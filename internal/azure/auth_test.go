package azure

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/mitchell-wallace/rover/internal/config"
)

type azCall struct {
	env     []string
	inherit bool
	args    []string
}

func TestAccountUsesIsolatedEnvAndConfiguredSubscription(t *testing.T) {
	st := config.Default()
	st.Azure.ConfigDir = t.TempDir()
	st.Azure.Subscription = "configured-sub"
	c := New(st, t.TempDir())

	var calls []azCall
	c.runAZ = func(_ context.Context, env []string, inherit bool, args ...string) ([]byte, error) {
		calls = append(calls, azCall{env: slices.Clone(env), inherit: inherit, args: slices.Clone(args)})
		return []byte(`{"id":"sub-id","name":"Work","tenantId":"tenant-id","user":{"name":"dev@example.com"}}`), nil
	}

	account, err := c.Account()
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if !account.LoggedIn || account.SubscriptionID != "sub-id" || account.SubscriptionName != "Work" || account.TenantID != "tenant-id" {
		t.Fatalf("unexpected account: %+v", account)
	}
	if len(calls) != 2 {
		t.Fatalf("az calls = %d, want 2", len(calls))
	}
	wantArgs := []string{"account", "show", "--subscription", "configured-sub", "-o", "json"}
	if !slices.Equal(calls[1].args, wantArgs) {
		t.Errorf("configured account args = %v, want %v", calls[1].args, wantArgs)
	}
	if !envContains(calls[0].env, "AZURE_CONFIG_DIR="+st.Azure.ConfigDir) {
		t.Errorf("account environment missing isolated AZURE_CONFIG_DIR: %v", calls[0].env)
	}
}

func TestAccountReportsLoggedOut(t *testing.T) {
	st := config.Default()
	st.Azure.ConfigDir = t.TempDir()
	c := New(st, t.TempDir())
	c.runAZ = func(context.Context, []string, bool, ...string) ([]byte, error) {
		return []byte("Please run 'az login' to setup account."), errors.New("exit status 1")
	}

	account, err := c.Account()
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if account.LoggedIn {
		t.Fatalf("Account = %+v, want logged out", account)
	}
}

func TestAccountReturnsUnexpectedCLIError(t *testing.T) {
	st := config.Default()
	st.Azure.ConfigDir = t.TempDir()
	c := New(st, t.TempDir())
	c.runAZ = func(context.Context, []string, bool, ...string) ([]byte, error) {
		return []byte("Azure config is corrupt"), errors.New("exit status 1")
	}

	if _, err := c.Account(); err == nil {
		t.Fatal("Account() error = nil, want unexpected CLI failure")
	}
}

func TestLoginAppliesTenantAndSubscription(t *testing.T) {
	st := config.Default()
	st.Azure.ConfigDir = t.TempDir()
	st.Azure.Tenant = "tenant-config"
	st.Azure.Subscription = "subscription-config"
	c := New(st, t.TempDir())

	var calls []azCall
	c.runAZ = func(_ context.Context, env []string, inherit bool, args ...string) ([]byte, error) {
		calls = append(calls, azCall{env: slices.Clone(env), inherit: inherit, args: slices.Clone(args)})
		return nil, nil
	}

	if err := c.Login(true); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("az calls = %d, want 2", len(calls))
	}
	wantLogin := []string{"login", "--use-device-code", "--tenant", "tenant-config"}
	if !slices.Equal(calls[0].args, wantLogin) || !calls[0].inherit {
		t.Errorf("login call = %+v, want inherited %v", calls[0], wantLogin)
	}
	wantSet := []string{"account", "set", "--subscription", "subscription-config"}
	if !slices.Equal(calls[1].args, wantSet) || !calls[1].inherit {
		t.Errorf("account set call = %+v, want inherited %v", calls[1], wantSet)
	}
}

func TestLogoutUsesIsolatedEnv(t *testing.T) {
	st := config.Default()
	st.Azure.ConfigDir = t.TempDir()
	c := New(st, t.TempDir())

	var call azCall
	c.runAZ = func(_ context.Context, env []string, inherit bool, args ...string) ([]byte, error) {
		call = azCall{env: slices.Clone(env), inherit: inherit, args: slices.Clone(args)}
		return nil, nil
	}
	if err := c.Logout(); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if !slices.Equal(call.args, []string{"logout"}) || !call.inherit {
		t.Errorf("logout call = %+v", call)
	}
	if !envContains(call.env, "AZURE_CONFIG_DIR="+st.Azure.ConfigDir) {
		t.Errorf("logout environment missing isolated AZURE_CONFIG_DIR: %v", call.env)
	}
}

func envContains(env []string, want string) bool {
	return slices.Contains(env, want)
}
