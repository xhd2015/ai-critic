package agentcli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xhd2015/ai-critic/client"
)

const serviceHostHelp = `Usage: %[1]s service host <subcommand> [args...]

Host-level systemd services: what boots, what runs, and who really owns it.

Subcommands:
  list [--json]
      Every boot-enabled systemd service with an owner verdict.

  disable <unit> [--mask]
      Turn off boot activation. --mask also blocks a manual 'systemctl start'.

Owners:
  systemd     the unit's own process is running
  ai-critic   an ai-critic managed service carries the same name
  both        defined in systemd and ai-critic at once (duplicate management)
  unused      boot-enabled but nothing is running (a stale unit)
`

// hostOwnerWord trims the JSON verdict for display.
func hostOwnerWord(owner string) string {
	switch owner {
	case "systemd":
		return "systemd"
	case "ai-critic":
		return "ai-critic"
	case "both":
		return "both (duplicate)"
	case "unused":
		return "unused"
	default:
		return owner
	}
}

func runServiceHost(resolve func() (*client.Client, error), args []string) error {
	if len(args) == 0 || isHelpToken(args[0]) {
		fmt.Fprintf(osStdout(), serviceHostHelp, active.Name)
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return runServiceHostList(resolve, args[1:])
	case "disable":
		return runServiceHostDisable(resolve, args[1:])
	default:
		return fmt.Errorf("unknown service host subcommand: %s", args[0])
	}
}

func runServiceHostList(resolve func() (*client.Client, error), args []string) error {
	asJSON := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprintf(osStdout(), serviceHostHelp, active.Name)
			return nil
		case "--json":
			asJSON = true
		default:
			return fmt.Errorf("service host list does not take %q", a)
		}
	}

	cli, err := resolve()
	if err != nil {
		return err
	}
	resp, err := cli.ListHostServices()
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(osStdout())
		enc.SetEscapeHTML(false)
		return enc.Encode(resp)
	}

	if len(resp.Services) == 0 {
		fmt.Fprintln(osStdout(), "No boot-enabled services found.")
		return nil
	}

	fmt.Fprintf(osStdout(), "%-34s %-9s %-9s %s\n", "UNIT", "BOOT", "NOW", "OWNER")
	for _, s := range resp.Services {
		owner := hostOwnerWord(s.Owner)
		if s.ManagedByAICritic != "" {
			owner = fmt.Sprintf("%s · ai-critic:%s", owner, s.ManagedByAICritic)
		}
		if s.Protected {
			owner += " · protected"
		}
		fmt.Fprintf(osStdout(), "%-34s %-9s %-9s %s\n", s.Unit, s.Boot, s.Active, owner)
	}
	fmt.Fprintf(osStdout(), "\ntotal=%d  unused=%d  duplicate=%d\n",
		resp.Summary.Total, resp.Summary.Unused, resp.Summary.Both)
	return nil
}

func runServiceHostDisable(resolve func() (*client.Client, error), args []string) error {
	var mask bool
	var unit string
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprintf(osStdout(), serviceHostHelp, active.Name)
			return nil
		case a == "--mask":
			mask = true
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		case unit == "":
			unit = a
		default:
			return fmt.Errorf("service host disable takes one unit, got %q", a)
		}
	}
	if unit == "" {
		return fmt.Errorf("unit is required (see %s service host disable -h)", active.Name)
	}

	cli, err := resolve()
	if err != nil {
		return err
	}
	res, err := cli.DisableHostService(unit, mask)
	if err != nil {
		return err
	}

	fmt.Fprintf(osStdout(), "disabled %s\n", res.Unit)
	fmt.Fprintf(osStdout(), "  boot: enabled -> %s\n", res.Boot)
	if res.Masked {
		fmt.Fprintln(osStdout(), "  masked: yes (systemctl start is now blocked)")
	}
	if res.Message != "" {
		fmt.Fprintf(osStdout(), "  %s\n", res.Message)
	}
	return nil
}
