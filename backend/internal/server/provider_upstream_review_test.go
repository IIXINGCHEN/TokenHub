package server

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func redirectRequest(t *testing.T, raw string) *http.Request {
	t.Helper()
	endpoint, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse redirect URL %q: %v", raw, err)
	}
	return &http.Request{URL: endpoint}
}

func TestStrictProviderUpstreamRedirectAllowsSameOrigin(t *testing.T) {
	original := redirectRequest(t, "https://api.example.com/v1/messages")
	next := redirectRequest(t, "https://api.example.com/v2/messages")
	if err := strictProviderUpstreamRedirect(next, []*http.Request{original}); err != nil {
		t.Fatalf("expected same-origin redirect to pass, got %v", err)
	}
}

func TestStrictProviderUpstreamRedirectRejectsOriginChanges(t *testing.T) {
	original := redirectRequest(t, "https://api.example.com/v1/messages")
	for _, raw := range []string{
		"https://attacker.example/v1/messages",
		"https://api.example.com:8443/v1/messages",
		"http://api.example.com/v1/messages",
	} {
		next := redirectRequest(t, raw)
		if err := strictProviderUpstreamRedirect(next, []*http.Request{original}); err == nil {
			t.Fatalf("expected redirect to %q to be rejected", raw)
		}
	}
}

func TestConfiguredProviderUpstreamNAT64PrefixClassifiesEmbeddedIPv4(t *testing.T) {
	t.Setenv(providerUpstreamNAT64PrefixEnv, "64:ff9b:1::/48")
	for _, ip := range []string{
		"64:ff9b:1:808:8:800::",    // 8.8.8.8
		"64:ff9b:1:c000:2:100::",   // 192.0.2.1
		"64:ff9b:1:a9fe:a9:fe00::", // 169.254.169.254
	} {
		err := checkProviderUpstreamLiteralDial(net.ParseIP(ip), nil)
		if ip == "64:ff9b:1:808:8:800::" {
			if err != nil {
				t.Fatalf("expected public NAT64 target %s to pass, got %v", ip, err)
			}
			continue
		}
		if !errors.Is(err, errProviderUpstreamDialDisallowed) {
			t.Fatalf("expected special-use NAT64 target %s to be rejected, got %v", ip, err)
		}
	}
}

func TestConfiguredProviderUpstreamNAT64PrefixDecodesRFC6052Formats(t *testing.T) {
	cases := []struct {
		prefix  string
		address string
	}{
		{"2001:db8::/32", "2001:db8:c000:221::"},
		{"2001:db8:100::/40", "2001:db8:1c0:2:21::"},
		{"2001:db8:122::/48", "2001:db8:122:c000:2:2100::"},
		{"2001:db8:122:300::/56", "2001:db8:122:3c0:0:221::"},
		{"2001:db8:122:344::/64", "2001:db8:122:344:c0:2:2100:0"},
		{"2001:db8:122:344::/96", "2001:db8:122:344::c000:221"},
	}
	for _, tc := range cases {
		t.Run(tc.prefix, func(t *testing.T) {
			t.Setenv(providerUpstreamNAT64PrefixEnv, tc.prefix)
			embedded, translated := providerUpstreamEmbeddedNAT64IPv4(net.ParseIP(tc.address))
			if !translated || embedded == nil || embedded.String() != "192.0.2.33" {
				t.Fatalf("expected %s to decode to 192.0.2.33, got %v (translated=%v)", tc.address, embedded, translated)
			}
		})
	}
}

func TestProviderUpstreamAllowlistCannotBypassHardDenials(t *testing.T) {
	allowed := mustParseCIDRs(t, "fd00::/8")
	t.Setenv(providerUpstreamNAT64PrefixEnv, "fd00:64::/64")
	for _, ip := range []string{
		"fd00:ec2::254",      // AWS IPv6 instance metadata
		"fd00:64::a:0:100:0", // NAT64 encoding of 10.0.0.1
	} {
		if err := checkProviderUpstreamLiteralDial(net.ParseIP(ip), allowed); !errors.Is(err, errProviderUpstreamDialDisallowed) {
			t.Fatalf("expected %s to stay rejected despite the allowlist, got %v", ip, err)
		}
	}
}

func TestInvalidProviderUpstreamNAT64PrefixFailsClosed(t *testing.T) {
	for _, prefix := range []string{
		"64:ff9b:1::/72",            // unsupported RFC 6052 prefix length
		"2001:db8:122:344:100::/96", // non-zero bits 64-71 violate RFC 6052
	} {
		t.Run(prefix, func(t *testing.T) {
			t.Setenv(providerUpstreamNAT64PrefixEnv, prefix)
			if block, _ := configuredProviderUpstreamNAT64Prefix(); block != nil {
				t.Fatalf("expected invalid NAT64 prefix %s to be ignored", prefix)
			}
		})
	}
	t.Setenv(providerUpstreamNAT64PrefixEnv, "64:ff9b:1::/72")
	if err := checkProviderUpstreamLiteralDial(net.ParseIP("64:ff9b:1:808:8:800::"), nil); !errors.Is(err, errProviderUpstreamDialDisallowed) {
		t.Fatalf("expected local-use NAT64 address to stay rejected with an invalid prefix, got %v", err)
	}
}
