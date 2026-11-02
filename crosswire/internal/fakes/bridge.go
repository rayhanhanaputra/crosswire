package fakes

import (
	"net/http/httptest"

	"github.com/rayhanhanaputra/AIAgentsAllTheThings/crosswire/internal/bridge"
)

// Bridge is a device bridge in simulate mode on a loopback port. It is the
// same server `crosswire bridge --simulate` runs.
type Bridge struct {
	srv *httptest.Server
	// Sim records what the bridge was asked to do.
	Sim *bridge.Sim
}

// NewBridge starts a simulated bridge for one device.
func NewBridge(kind, deviceID, resource string) *Bridge {
	sim := &bridge.Sim{}
	s := bridge.NewWithBackend(bridge.Config{Kind: kind, DeviceID: deviceID, Resource: resource, Simulate: true}, sim)
	return &Bridge{srv: httptest.NewServer(s.Handler()), Sim: sim}
}

// ActionURL and StopURL are the two endpoints a bridge target needs.
func (b *Bridge) ActionURL() string { return b.srv.URL + "/action" }
func (b *Bridge) StopURL() string   { return b.srv.URL + "/stop" }

// Close stops the server.
func (b *Bridge) Close() { b.srv.Close() }
