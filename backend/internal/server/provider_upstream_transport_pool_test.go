package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderUpstreamTransportRotationUsesNewPoolWithoutInterruptingInflightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/slow" {
			close(started)
			<-release
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok"))
	}))
	defer upstream.Close()

	var dials atomic.Int32
	factory := func() *http.Transport {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		dialer := &net.Dialer{Timeout: time.Second}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			dials.Add(1)
			return dialer.DialContext(ctx, network, address)
		}
		return transport
	}
	store := NewMemoryStore()
	store.CreateResource("settings", syntheticDNSSettings(false, defaultSyntheticDNSCIDRs))
	policy := newProviderSyntheticDNSPolicy(store)
	pool := newProviderUpstreamTransportPool(policy, factory)
	client := &http.Client{Transport: pool}

	firstDone := make(chan error, 1)
	go func() {
		response, err := client.Get(upstream.URL + "/slow")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			err = response.Body.Close()
		}
		firstDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the in-flight request")
	}

	enabled := syntheticDNSSettings(true, defaultSyntheticDNSCIDRs)
	policy.applySetting(&enabled)
	response, err := client.Get(upstream.URL + "/fast")
	if err != nil {
		t.Fatalf("new generation request failed: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("rotation interrupted the in-flight request: %v", err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("expected each transport generation to dial once, got %d dials", got)
	}
}
