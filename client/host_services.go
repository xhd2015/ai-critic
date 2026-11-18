package client

// HostService is one boot-enabled systemd unit with its resolved owner.
type HostService struct {
	Unit              string `json:"unit"`
	Boot              string `json:"boot"`
	Active            string `json:"active"`
	Sub               string `json:"sub,omitempty"`
	Description       string `json:"description,omitempty"`
	Owner             string `json:"owner"`
	ManagedByAICritic string `json:"managed_by_ai_critic,omitempty"`
	Protected         bool   `json:"protected,omitempty"`
}

// HostServicesSummary counts the interesting verdicts.
type HostServicesSummary struct {
	Total  int `json:"total"`
	Unused int `json:"unused"`
	Both   int `json:"both"`
}

// HostServicesResponse is the /api/host/services payload.
type HostServicesResponse struct {
	Services []HostService       `json:"services"`
	Summary  HostServicesSummary `json:"summary"`
}

// ListHostServices returns every boot-enabled systemd service.
func (c *Client) ListHostServices() (*HostServicesResponse, error) {
	var out HostServicesResponse
	if err := c.getJSON("/api/host/services", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// HostServiceDisableResult reports a unit that was turned off at boot.
type HostServiceDisableResult struct {
	Unit    string `json:"unit"`
	Boot    string `json:"boot"`
	Masked  bool   `json:"masked"`
	Message string `json:"message,omitempty"`
}

// DisableHostService turns off boot activation, optionally masking the unit.
func (c *Client) DisableHostService(unit string, mask bool) (*HostServiceDisableResult, error) {
	body := map[string]any{"unit": unit, "mask": mask}
	var out HostServiceDisableResult
	if err := c.postJSON("/api/host/services/disable", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
