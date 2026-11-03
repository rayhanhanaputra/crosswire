package guard

import (
	"context"
	"net"
	"testing"
)

func TestClassifyIP(t *testing.T) {
	tests := []struct {
		ip       string
		publicOK bool
		bad      bool
	}{
		{"127.0.0.1", false, false},
		{"::1", false, false},
		{"10.1.2.3", false, false},
		{"192.168.1.5", false, false},
		{"100.101.129.10", false, false},
		{"fd00::1", false, false},
		{"8.8.8.8", false, true},
		{"8.8.8.8", true, false},
		{"0.0.0.0", true, true},
		{"224.0.0.1", true, true},
		{"169.254.169.254", true, true},
		{"fe80::1", true, true},
		{"240.0.0.1", true, true},
	}
	for _, tc := range tests {
		err := ClassifyIP(net.ParseIP(tc.ip), tc.publicOK)
		if (err != nil) != tc.bad {
			t.Errorf("ClassifyIP(%s, public=%v) = %v, want bad=%v", tc.ip, tc.publicOK, err, tc.bad)
		}
	}
}

func TestDialRefusesMetadataAddress(t *testing.T) {
	dial := DialContext(true, 0)
	if _, err := dial(context.Background(), "tcp", "169.254.169.254:80"); err == nil {
		t.Fatal("dial to a link-local address should be refused")
	}
}

func TestParseURLAndTLS(t *testing.T) {
	if _, err := ParseURL("http://user:pw@host/x", "http"); err == nil {
		t.Error("credentials in a URL should be refused")
	}
	if _, err := ParseURL("http://host/x#frag", "http"); err == nil {
		t.Error("fragment should be refused")
	}
	if _, err := ParseURL("ftp://host/x", "http", "https"); err == nil {
		t.Error("wrong scheme should be refused")
	}
	u, _ := ParseURL("http://agent.example.com/x", "http", "https")
	if RequireTLSOffHost(u) == nil {
		t.Error("plain http off-host must be refused for production")
	}
	u, _ = ParseURL("http://127.0.0.1:8080/x", "http", "https")
	if err := RequireTLSOffHost(u); err != nil {
		t.Errorf("loopback http is fine: %v", err)
	}
	u, _ = ParseURL("wss://agent.example.com/x", "ws", "wss")
	if err := RequireTLSOffHost(u); err != nil {
		t.Errorf("wss is fine: %v", err)
	}
}

func TestHeaders(t *testing.T) {
	if CheckStaticHeader("Authorization", "Bearer x") == nil {
		t.Error("Authorization must go through secret_headers")
	}
	if err := CheckStaticHeader("X-Trace", "1"); err != nil {
		t.Error(err)
	}
	if CheckSecretHeaderName("Host") == nil {
		t.Error("Host cannot be a secret header")
	}
}
