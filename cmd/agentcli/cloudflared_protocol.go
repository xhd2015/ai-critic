package agentcli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/xhd2015/ai-critic/client"
)

const cloudflaredProtocolHelp = `Usage: %[1]s cloudflared protocol [http2|quic] [--no-restart] [--json]

Get or set the cloudflared edge transport for this server's tunnel.

Arguments:
  (none)    Print the effective protocol
  http2     HTTP/2 over TCP. Default. Survives networks that drop UDP/7844.
  quic      QUIC over UDP. Independent streams keep one lossy path from
            collapsing the whole connection; requires UDP/7844 to the edge.

Flags:
  --no-restart   Persist only; apply on the next tunnel start
  --json         Machine-readable output on stdout
  -h, --help     Show this help
`

func runCloudflaredProtocol(resolve func() (*client.Client, error), args []string) error {
	var noRestart bool
	var asJSON bool
	var positional []string

	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprintf(osStdout(), cloudflaredProtocolHelp, active.Name)
			return nil
		case "--no-restart":
			noRestart = true
		case "--json":
			asJSON = true
		default:
			if len(a) > 0 && a[0] == '-' {
				return fmt.Errorf("unknown flag %q (see %s cloudflared protocol -h)", a, active.Name)
			}
			positional = append(positional, a)
		}
	}
	if len(positional) > 1 {
		return fmt.Errorf("cloudflared protocol takes at most one protocol, got %v", positional)
	}

	cli, err := resolve()
	if err != nil {
		return err
	}

	if len(positional) == 0 {
		info, err := cli.GetTunnelProtocol()
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSONOut(map[string]any{
				"protocol":  info.Protocol,
				"effective": info.Effective,
				"default":   info.Default,
			})
		}
		fmt.Fprintln(osStdout(), info.Effective)
		return nil
	}

	res, err := cli.SetTunnelProtocol(positional[0], !noRestart)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSONOut(map[string]any{
			"protocol":  res.Protocol,
			"effective": res.Effective,
			"default":   res.Default,
			"restarted": res.Restarted,
		})
	}

	fmt.Fprintf(osStdout(), "edge protocol set to %s\n", res.Effective)
	if noRestart {
		fmt.Fprintln(os.Stderr, "warning: saved; tunnel not restarted (takes effect on next start)")
		return nil
	}
	if res.Restarted {
		fmt.Fprintln(osStdout(), "restarted: yes")
	} else {
		fmt.Fprintln(osStdout(), "restarted: no (already in effect)")
	}
	return nil
}

// writeJSONOut emits machine-readable output with no ANSI and no HTML escaping.
func writeJSONOut(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
