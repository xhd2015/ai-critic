package agentcli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/xhd2015/ai-critic/client"
)

const wsproxyClientHelp = `Usage: remote-agent ws-proxy client <subcommand> [args...]

Manage the client half of the WS proxy: a local Xray process whose HTTP and
SOCKS inbounds tunnel through a remote ws-proxy instance. The server runs it as
a system service, so it comes back with ai-critic after a reboot.

Subcommands:
  list
      List configured clients with their live state.

  add --name X (--server URL --instance NAME [--token-stdin] | --vmess-link LINK)
      [--http-port P] [--socks-port P] [--auto-start] [--pac-domain D]…
      Add a client. --server points at the serving ai-critic instance.

  remove --name NAME
      Delete a client (stops it first).

  start --name NAME [--no-probe]
  stop --name NAME
      Start/stop one client. Start probes the tunnel unless --no-probe.

  status --name NAME
      Show one client's state, ports and uptime.

  pac --name NAME [--fallback host:port]
      Print a PAC script routing the client's domains through the tunnel.

Examples:
  remote-agent ws-proxy client add --name work-mac \
      --server https://mac-agent.example.com --instance ws-on-mac --token-stdin --auto-start
  remote-agent ws-proxy client list
  remote-agent ws-proxy client start --name work-mac
  remote-agent ws-proxy client pac --name work-mac --fallback proxy.internal:8897
`

// wsproxyClientPayload mirrors the server's write payload.
type wsproxyClientPayload struct {
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name"`
	Source     string   `json:"source,omitempty"`
	BaseURL    string   `json:"base_url,omitempty"`
	Instance   string   `json:"instance,omitempty"`
	Token      string   `json:"token,omitempty"`
	VMessLink  string   `json:"vmess_link,omitempty"`
	HTTPPort   int      `json:"http_port,omitempty"`
	SocksPort  int      `json:"socks_port,omitempty"`
	AutoStart  bool     `json:"auto_start,omitempty"`
	PacDomains []string `json:"pac_domains,omitempty"`
}

// wsproxyClientStatus mirrors the server's runtime status.
type wsproxyClientStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	Server    string `json:"server"`
	Instance  string `json:"instance"`
	HTTPPort  int    `json:"http_port"`
	SocksPort int    `json:"socks_port"`
	AutoStart bool   `json:"auto_start"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid"`
	Uptime    int    `json:"uptime_secs"`
	LastError string `json:"last_error"`
}

func runWSProxyClient(getClient func() (*client.Client, error), args []string) error {
	if len(args) == 0 {
		fmt.Print(wsproxyClientHelp)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		return wsproxyClientList(getClient, rest)
	case "add":
		return wsproxyClientAdd(getClient, rest)
	case "remove", "rm":
		return wsproxyClientRemove(getClient, rest)
	case "start":
		return wsproxyClientStartStop(getClient, rest, "start")
	case "stop":
		return wsproxyClientStartStop(getClient, rest, "stop")
	case "status":
		return wsproxyClientStatusCmd(getClient, rest)
	case "pac":
		return wsproxyClientPAC(getClient, rest)
	default:
		fmt.Print(wsproxyClientHelp)
		return nil
	}
}

// parseKVArgs splits "--k v" pairs; a bare "--flag" becomes true.
func parseKVArgs(args []string) map[string]string {
	opts := map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		key := strings.TrimPrefix(a, "--")
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			opts[key] = args[i+1]
			i++
			continue
		}
		opts[key] = "true"
	}
	return opts
}

func wsproxyClientList(getClient func() (*client.Client, error), _ []string) error {
	c, err := getClient()
	if err != nil {
		return err
	}
	var resp struct {
		Statuses []wsproxyClientStatus `json:"statuses"`
	}
	if err := wsproxyClientDo(c, http.MethodGet, "/api/ws-proxy/clients", nil, &resp); err != nil {
		return err
	}
	if len(resp.Statuses) == 0 {
		fmt.Println("no ws-proxy clients; add one with: ws-proxy client add --name … --server … --instance …")
		return nil
	}
	fmt.Printf("%-16s %-9s %-14s %-14s %-8s %s\n", "NAME", "STATE", "HTTP", "SOCKS", "AUTO", "SOURCE")
	for _, st := range resp.Statuses {
		state := "stopped"
		if st.Running {
			state = "running"
		}
		src := st.Source
		if st.Server != "" {
			src = st.Source + " " + st.Server
		}
		auto := "off"
		if st.AutoStart {
			auto = "on"
		}
		fmt.Printf("%-16s %-9s %-14s %-14s %-8s %s\n",
			st.ID, state,
			fmt.Sprintf("127.0.0.1:%d", st.HTTPPort),
			fmt.Sprintf("127.0.0.1:%d", st.SocksPort),
			auto, src)
	}
	return nil
}

func wsproxyClientAdd(getClient func() (*client.Client, error), args []string) error {
	opts := parseKVArgs(args)
	if opts["name"] == "" {
		return fmt.Errorf("--name is required")
	}
	p := wsproxyClientPayload{Name: opts["name"], AutoStart: opts["auto-start"] == "true"}
	if port := opts["http-port"]; port != "" {
		if _, err := fmt.Sscanf(port, "%d", &p.HTTPPort); err != nil {
			return fmt.Errorf("--http-port must be a number: %w", err)
		}
	}
	if port := opts["socks-port"]; port != "" {
		if _, err := fmt.Sscanf(port, "%d", &p.SocksPort); err != nil {
			return fmt.Errorf("--socks-port must be a number: %w", err)
		}
	}
	for _, d := range strings.Split(opts["pac-domain"], ",") {
		if d = strings.TrimSpace(d); d != "" {
			p.PacDomains = append(p.PacDomains, d)
		}
	}

	switch {
	case opts["vmess-link"] != "":
		p.Source, p.VMessLink = "vmess", opts["vmess-link"]
	case opts["server"] != "":
		p.Source, p.BaseURL, p.Instance = "remote", opts["server"], opts["instance"]
		if opts["token-stdin"] == "true" {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read token from stdin: %w", err)
			}
			p.Token = strings.TrimSpace(string(data))
		} else if tok := opts["token"]; tok != "" {
			p.Token = tok
		}
	default:
		return fmt.Errorf("provide --server URL --instance NAME, or --vmess-link LINK")
	}

	c, err := getClient()
	if err != nil {
		return err
	}
	var out struct {
		Client struct {
			ID string `json:"id"`
		} `json:"client"`
	}
	if err := wsproxyClientDo(c, http.MethodPost, "/api/ws-proxy/clients", p, &out); err != nil {
		return err
	}
	fmt.Printf("added client %s\n", out.Client.ID)
	return nil
}

func wsproxyClientRemove(getClient func() (*client.Client, error), args []string) error {
	opts := parseKVArgs(args)
	name := opts["name"]
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	c, err := getClient()
	if err != nil {
		return err
	}
	if err := wsproxyClientDo(c, http.MethodDelete, "/api/ws-proxy/clients/"+name, nil, nil); err != nil {
		return err
	}
	fmt.Printf("removed client %s\n", name)
	return nil
}

func wsproxyClientStartStop(getClient func() (*client.Client, error), args []string, action string) error {
	opts := parseKVArgs(args)
	name := opts["name"]
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	c, err := getClient()
	if err != nil {
		return err
	}

	if action == "stop" {
		if err := wsproxyClientDo(c, http.MethodPost, "/api/ws-proxy/clients/"+name+"/stop", nil, nil); err != nil {
			return err
		}
		fmt.Printf("client %s: stopped\n", name)
		return nil
	}

	url := "/api/ws-proxy/clients/" + name + "/start"
	if opts["no-probe"] == "true" {
		// The server probes by default; an empty probe disables it.
		url += "?probe=-"
	}
	var out struct {
		Status wsproxyClientStatus `json:"status"`
	}
	if err := wsproxyClientDo(c, http.MethodPost, url, nil, &out); err != nil {
		return err
	}
	fmt.Printf("client %s: running · http 127.0.0.1:%d · socks 127.0.0.1:%d\n",
		out.Status.ID, out.Status.HTTPPort, out.Status.SocksPort)
	return nil
}

func wsproxyClientStatusCmd(getClient func() (*client.Client, error), args []string) error {
	opts := parseKVArgs(args)
	name := opts["name"]
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	c, err := getClient()
	if err != nil {
		return err
	}
	var resp struct {
		Statuses []wsproxyClientStatus `json:"statuses"`
	}
	if err := wsproxyClientDo(c, http.MethodGet, "/api/ws-proxy/clients", nil, &resp); err != nil {
		return err
	}
	for _, st := range resp.Statuses {
		if st.ID != name {
			continue
		}
		state := "stopped"
		if st.Running {
			state = "running"
		}
		fmt.Printf("%-12s %s\n", "id:", st.ID)
		fmt.Printf("%-12s %s\n", "state:", state)
		fmt.Printf("%-12s 127.0.0.1:%d\n", "http:", st.HTTPPort)
		fmt.Printf("%-12s 127.0.0.1:%d\n", "socks:", st.SocksPort)
		fmt.Printf("%-12s %s\n", "source:", st.Source)
		if st.Server != "" {
			fmt.Printf("%-12s %s\n", "server:", st.Server)
		}
		if st.Running {
			fmt.Printf("%-12s %d\n", "pid:", st.PID)
			fmt.Printf("%-12s %ds\n", "uptime:", st.Uptime)
		}
		if st.LastError != "" {
			fmt.Printf("%-12s %s\n", "last error:", st.LastError)
		}
		return nil
	}
	return fmt.Errorf("client not found: %s", name)
}

func wsproxyClientPAC(getClient func() (*client.Client, error), args []string) error {
	opts := parseKVArgs(args)
	name := opts["name"]
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	c, err := getClient()
	if err != nil {
		return err
	}
	url := "/api/ws-proxy/clients/" + name + "/pac"
	if fb := opts["fallback"]; fb != "" {
		url += "?fallback=" + fb
	}
	req, err := c.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("fetch PAC: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

// wsproxyClientDo performs one API call and decodes the JSON response.
func wsproxyClientDo(c *client.Client, method, url string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = strings.NewReader(string(data))
	}
	req, err := c.NewRequest(method, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &apiErr) == nil && apiErr.Error != "" {
			return fmt.Errorf("%s", apiErr.Error)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
