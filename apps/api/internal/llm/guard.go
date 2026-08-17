package llm

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// EgressAssertion is the machine-checkable proof shown in the Admin panel
// that inference traffic never leaves the institution's network.
type EgressAssertion struct {
	Endpoint      string   `json:"endpoint"`
	Host          string   `json:"host"`
	ResolvedIPs   []string `json:"resolved_ips"`
	AllPrivate    bool     `json:"all_private"`
	PublicIPs     []string `json:"public_ips,omitempty"`
	OverrideInUse bool     `json:"override_in_use"`
	Message       string   `json:"message"`
}

// AssertLocalEndpoint resolves the configured inference endpoint and reports
// whether every resolved address is inside a private/loopback range.
func AssertLocalEndpoint(rawURL string, allowPublic bool) (*EgressAssertion, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("LLM_BASE_URL is not a valid URL: %q", rawURL)
	}
	host := u.Hostname()

	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve LLM host %q: %w", host, err)
		}
		ips = resolved
	}

	a := &EgressAssertion{
		Endpoint:      rawURL,
		Host:          host,
		AllPrivate:    true,
		OverrideInUse: allowPublic,
	}
	for _, ip := range ips {
		a.ResolvedIPs = append(a.ResolvedIPs, ip.String())
		if !isPrivateIP(ip) {
			a.AllPrivate = false
			a.PublicIPs = append(a.PublicIPs, ip.String())
		}
	}

	switch {
	case a.AllPrivate:
		a.Message = "All inference traffic stays on institution-private addresses."
	case allowPublic:
		a.Message = "WARNING: endpoint resolves to a public address and LLM_ALLOW_PUBLIC_IP=true is overriding the guard."
	default:
		a.Message = "Blocked: LLM_BASE_URL resolves to a public address."
		return a, fmt.Errorf("refusing to start: LLM_BASE_URL %q resolves to public address(es) %s — local inference only",
			rawURL, strings.Join(a.PublicIPs, ", "))
	}
	return a, nil
}

func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4[0] == 10:
			return true
		case ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31:
			return true
		case ip4[0] == 192 && ip4[1] == 168:
			return true
		case ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127: // CGNAT, common on lab VLANs
			return true
		case ip4[0] == 127:
			return true
		}
		return false
	}
	// IPv6 unique-local fc00::/7
	return len(ip) == net.IPv6len && (ip[0]&0xfe) == 0xfc
}
