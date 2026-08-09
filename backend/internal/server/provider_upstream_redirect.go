package server

import (
	"fmt"
	"net/http"
	"strings"
)

// strictProviderUpstreamRedirect permits only same-origin redirects. Provider
// requests can carry credentials in non-standard headers that net/http may
// copy across hosts, so validating only the destination IP class is not
// sufficient.
func strictProviderUpstreamRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if err := validateProviderUpstreamBaseURL(req.URL, nil, false); err != nil {
		return err
	}
	if len(via) == 0 || via[0] == nil || via[0].URL == nil {
		return fmt.Errorf("redirect has no original request")
	}
	original := via[0].URL
	if !strings.EqualFold(req.URL.Scheme, original.Scheme) || !strings.EqualFold(req.URL.Host, original.Host) {
		return fmt.Errorf("provider upstream redirect changed origin")
	}
	return nil
}
