package services

import (
	"fmt"
	"strings"

	"github.com/xhd2015/dot-pkgs/go-pkgs/service"
)

// The system-service model lives in the shared service package so every host
// (ai-critic, eluc) speaks the same vocabulary. These aliases keep the existing
// ai-critic call sites and JSON payloads unchanged.
type (
	ServiceKind             = service.ServiceKind
	SystemRuntime           = service.SystemRuntime
	SystemHostStatus        = service.SystemHostStatus
	SystemHost              = service.SystemHost
	SystemController        = service.SystemController
	SystemLifecycle         = service.SystemLifecycle
	SystemRestarter         = service.SystemRestarter
	SystemHostProvider      = service.SystemHostProvider
	SystemAutoStarter       = service.SystemAutoStarter
	SystemOperationStreamer = service.SystemOperationStreamer
	SystemService           = service.SystemService
)

const (
	ServiceKindUser   = service.ServiceKindUser
	ServiceKindSystem = service.ServiceKindSystem

	SystemHostLive     = service.SystemHostLive
	SystemHostStarting = service.SystemHostStarting
	SystemHostDead     = service.SystemHostDead
	SystemHostMissing  = service.SystemHostMissing
	SystemHostUnknown  = service.SystemHostUnknown

	SystemActionStart   = service.SystemActionStart
	SystemActionStop    = service.SystemActionStop
	SystemActionRestart = service.SystemActionRestart

	msgSystemDisable = service.SystemMessageDisable
	msgSystemEnable  = service.SystemMessageEnable
)

// ErrSystemServiceImmutable marks a write aimed at a system service. Handlers
// map it to 400 rather than 404: the service exists, it just cannot be changed.
var ErrSystemServiceImmutable = service.ErrSystemServiceImmutable

// errSystemActionUnsupported explains a refused action in terms of what the
// caller should do instead, rather than just "not supported".
func errSystemActionUnsupported(svc SystemService, action string) error {
	return fmt.Errorf("system service %q does not support %s; %s",
		svc.Name, action, service.SystemActionHint(action))
}

// serviceKindOrUser normalizes a kind read from JSON. An absent kind is a user
// service, which keeps payloads written before kinds existed readable.
func serviceKindOrUser(kind ServiceKind) ServiceKind {
	return service.NormalizeServiceKind(kind)
}

// RegisterSystemService adds a server-owned service to this manager. It fails
// when the id is empty, duplicated, or already taken by a user service.
func (m *Manager) RegisterSystemService(svc SystemService) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.system == nil {
		return fmt.Errorf("system service registry is not initialized")
	}
	if _, ok := m.findDefinitionLocked(svc.ID); ok && strings.TrimSpace(svc.ID) != "" {
		return fmt.Errorf("system service %s collides with a user service", strings.TrimSpace(svc.ID))
	}
	return m.system.Register(svc)
}

// SystemServices returns the registered system services in registration order.
func (m *Manager) SystemServices() []SystemService {
	if m.system == nil {
		return nil
	}
	return m.system.List()
}

// systemService resolves id to a registered system service.
func (m *Manager) systemService(id string) (SystemService, bool) {
	if m.system == nil {
		return SystemService{}, false
	}
	return m.system.Get(id)
}

// buildSystemStatusListLocked renders the registry for the API and the GUI.
func (m *Manager) buildSystemStatusListLocked() []ServiceStatus {
	svcs := m.SystemServices()
	result := make([]ServiceStatus, 0, len(svcs))
	for _, svc := range svcs {
		if provider, ok := svc.Controller.(SystemHostProvider); ok {
			provider.SetSystemHosts(m.cloudflareOwnedHostsLocked())
		}
		runtime := svc.Controller.SystemStatus()

		status := StatusStopped
		if runtime.Status != "" {
			status = runtime.Status
		} else if runtime.Running {
			status = StatusRunning
		}
		enabled := true
		if runtime.AutoStart != nil {
			enabled = *runtime.AutoStart
		}

		result = append(result, ServiceStatus{
			ID:              svc.ID,
			Name:            svc.Name,
			Kind:            ServiceKindSystem,
			Description:     svc.Description,
			Status:          status,
			DesiredRunning:  enabled,
			Enabled:         enabled,
			LogPath:         runtime.LogPath,
			Detail:          runtime.Detail,
			PublicURL:       runtime.PublicURL,
			Port:            runtime.Port,
			Mocked:          runtime.Mocked,
			AutoStartSwitch: runtime.AutoStart != nil,
			Edge:            runtime.Edge,
			Hosts:           runtime.Hosts,
			Actions:         service.SystemActions(svc.Controller),
			OperationStream: service.SystemSupportsOperationStream(svc.Controller),
		})
	}
	return result
}

// setSystemAutoStart toggles a system service's own auto-start switch.
func (m *Manager) setSystemAutoStart(svc SystemService, enabled bool) (*ServiceActionResponse, error) {
	starter, ok := svc.Controller.(SystemAutoStarter)
	if !ok {
		return nil, fmt.Errorf("system service %q has no auto-start switch", svc.Name)
	}
	if err := starter.SetSystemAutoStart(enabled); err != nil {
		return nil, err
	}

	message := msgSystemDisable
	if enabled {
		message = msgSystemEnable
	}
	status, ok := m.statusByID(svc.ID)
	if !ok {
		return nil, fmt.Errorf("system service %s not found after action", svc.ID)
	}
	return &ServiceActionResponse{
		Status:  "ok",
		Message: message,
		Service: status,
	}, nil
}

// rejectSystemServiceEdit guards the definition-writing paths. System services
// have no definition to edit, so allowing a write would silently create a user
// service that shadows the registered one.
func (m *Manager) rejectSystemServiceEdit(id string, action string) error {
	svc, ok := m.systemService(id)
	if !ok {
		return nil
	}
	return fmt.Errorf("system service %q cannot be %s: %w", svc.Name, action, ErrSystemServiceImmutable)
}
