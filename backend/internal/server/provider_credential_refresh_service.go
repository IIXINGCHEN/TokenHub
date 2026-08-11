package server

import (
	"context"
	"log"
	"sync"
	"time"
)

const providerCredentialRefreshInterval = time.Minute

// ProviderCredentialRefreshService renews OAuth access tokens shortly before
// they expire. RefreshProviderResourceCredentials owns the cluster lease, so
// every replica may run this scheduler without rotating a token concurrently.
type ProviderCredentialRefreshService struct {
	store Store

	schedulerOnce sync.Once
	schedulerStop context.CancelFunc
	schedulerDone chan struct{}
}

func newProviderCredentialRefreshService(store Store) *ProviderCredentialRefreshService {
	return &ProviderCredentialRefreshService{store: store}
}

func (s *ProviderCredentialRefreshService) RunDue(ctx context.Context) {
	for _, resource := range s.store.ListProviderResources() {
		if ctx.Err() != nil {
			return
		}
		if !isOpenAIAccountResource(resource.ResourceType) || resource.Status != StatusActive || resource.CredentialSummary["has_refresh_token"] != "true" || resource.CredentialSummary[openAIAccountReauthorizationRequiredOption] == "true" {
			continue
		}
		if _, err := s.store.RefreshProviderResourceCredentials(ctx, resource.ID, false); err != nil {
			if AsHTTPError(err).Code == "provider_resource_reauthorization_required" {
				if _, updateErr := s.store.UpdateProviderResourceOptions(resource.ID, map[string]string{openAIAccountReauthorizationRequiredOption: "true"}); updateErr != nil {
					log.Printf("[tokenhub] could not mark provider resource %s for reauthorization: %v", resource.ID, updateErr)
				}
			}
			log.Printf("[tokenhub] OAuth token renewal failed for provider resource %s: %v", resource.ID, err)
		}
	}
}

func (s *ProviderCredentialRefreshService) StartScheduler(interval time.Duration) {
	if interval <= 0 {
		interval = providerCredentialRefreshInterval
	}
	s.schedulerOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.schedulerStop = cancel
		s.schedulerDone = make(chan struct{})
		go func() {
			defer close(s.schedulerDone)
			s.RunDue(ctx)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.RunDue(ctx)
				}
			}
		}()
	})
}

func (s *ProviderCredentialRefreshService) Shutdown(ctx context.Context) error {
	if s.schedulerStop == nil {
		return nil
	}
	s.schedulerStop()
	select {
	case <-s.schedulerDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
