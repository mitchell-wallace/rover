package cmd

import (
	"fmt"

	"github.com/mitchell-wallace/rover/internal/azure"
	"github.com/mitchell-wallace/rover/internal/config"
	"github.com/mitchell-wallace/rover/internal/ui"
	"github.com/spf13/cobra"
)

func init() {
	var browser bool
	loginCmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Rover's isolated Azure CLI context",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			st, az, err := loadAzureOnly()
			if err != nil {
				return err
			}
			return loginAzure(st, az, !browser)
		},
	}
	loginCmd.Flags().BoolVar(&browser, "browser", false, "use the Azure CLI browser login flow instead of device code")
	rootCmd.AddCommand(loginCmd)

	logoutCmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of Rover's isolated Azure CLI context",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			st, az, err := loadAzureOnly()
			if err != nil {
				return err
			}
			return logoutAzure(st, az)
		},
	}
	rootCmd.AddCommand(logoutCmd)
}

func loginAzure(st *config.State, az *azure.Client, deviceCode bool) error {
	dir, err := st.AzureConfigDir()
	if err != nil {
		return err
	}
	ui.Info("Logging in to Rover's Azure context (%s)...", dir)
	if err := az.Login(deviceCode); err != nil {
		return err
	}
	account, err := az.Account()
	if err != nil {
		return err
	}
	if !account.LoggedIn {
		return fmt.Errorf("azure CLI login completed but no active account was found")
	}
	ui.Info("Azure login complete.")
	printAzureAccount(st, account)
	return nil
}

func logoutAzure(st *config.State, az *azure.Client) error {
	if err := az.Logout(); err != nil {
		return err
	}
	dir, err := st.AzureConfigDir()
	if err != nil {
		return err
	}
	ui.Info("Logged out of Rover's Azure context (%s).", dir)
	return nil
}

func (a *appContext) status() error {
	account, err := a.azure.Account()
	if err != nil {
		return err
	}
	printAzureAccount(a.state, account)
	if !account.LoggedIn {
		return nil
	}
	fmt.Println()
	return a.vm.Status()
}

func printAzureAccount(st *config.State, account azure.AccountInfo) {
	dir, err := st.AzureConfigDir()
	if err != nil {
		dir = "(unavailable: " + err.Error() + ")"
	}
	if !account.LoggedIn {
		fmt.Println("Azure: logged out — run 'rover login'")
		fmt.Printf("  config dir:   %s\n", dir)
		return
	}
	fmt.Println("Azure: logged in")
	if account.User != "" {
		fmt.Printf("  account:      %s\n", account.User)
	}
	subscription := account.SubscriptionName
	if subscription == "" {
		subscription = account.SubscriptionID
	} else if account.SubscriptionID != "" {
		subscription += " (" + account.SubscriptionID + ")"
	}
	fmt.Printf("  subscription: %s\n", subscription)
	if account.TenantID != "" {
		fmt.Printf("  tenant:       %s\n", account.TenantID)
	}
	fmt.Printf("  config dir:   %s\n", dir)
}
