package server

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestProviderResourceMutationPreservesConcurrentProtectedOptions(t *testing.T) {
	databaseURL := "sqlite://" + filepath.Join(t.TempDir(), "provider-resource-options.db")
	storeA, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	storeB, err := NewSQLiteStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, store := range []*GormStore{storeA, storeB} {
			if sqlDB, dbErr := store.db.DB(); dbErr == nil {
				_ = sqlDB.Close()
			}
		}
	})

	provider := storeA.AddProvider(Provider{
		Name: "Concurrent Protected Options", Type: ProviderOpenAICodex, Status: StatusActive, Healthy: true,
	})
	resource, err := storeA.AddProviderResource(ProviderResource{
		ProviderID: provider.ID, Name: "Before Concurrent Edit", ResourceType: ProviderResourceOpenAISubscription,
		Status: StatusActive, Healthy: true,
		Credentials: &ProviderResourceCredentials{
			AccessToken: "concurrent-options-access", RefreshToken: "concurrent-options-refresh",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	loaded := make(chan struct{})
	release := make(chan struct{})
	var loadOnce sync.Once
	var releaseOnce sync.Once
	releasePatch := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releasePatch)
	callbackName := "test:block-provider-resource-protected-options"
	if err := storeA.db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement == nil || tx.Statement.Schema == nil || tx.Statement.Schema.Table != "provider_resources" {
			return
		}
		loadOnce.Do(func() {
			close(loaded)
			<-release
		})
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storeA.db.Callback().Query().Remove(callbackName); err != nil {
			t.Errorf("remove query callback: %v", err)
		}
	})

	patchDone := make(chan error, 1)
	go func() {
		_, updateErr := storeA.UpdateProviderResource(resource.ID, ProviderResource{
			ProviderID: provider.ID, Name: "After Concurrent Edit", ResourceType: resource.ResourceType,
			BaseURL: resource.BaseURL, Status: StatusActive, Healthy: true, Weight: resource.Weight,
			Options: map[string]string{"auth_type": "oauth", "operator_note": "retained"},
		})
		patchDone <- updateErr
	}()
	select {
	case <-loaded:
	case <-time.After(time.Second):
		t.Fatal("resource patch did not load the provider resource")
	}

	optionsStarted := make(chan struct{})
	optionsDone := make(chan error, 1)
	go func() {
		close(optionsStarted)
		_, updateErr := storeB.UpdateProviderResourceOptions(resource.ID, map[string]string{
			codexImageCapabilityOption:                 codexImageCapabilitySupported,
			codexImageCapabilityCheckedAtOption:        time.Now().UTC().Format(time.RFC3339Nano),
			openAIAccountReauthorizationRequiredOption: "true",
		})
		optionsDone <- updateErr
	}()
	<-optionsStarted
	select {
	case err := <-optionsDone:
		t.Fatalf("protected option update bypassed the shared resource mutation lease: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	releasePatch()
	if err := <-patchDone; err != nil {
		t.Fatal(err)
	}
	if err := <-optionsDone; err != nil {
		t.Fatal(err)
	}
	updated, ok := storeA.GetProviderResource(resource.ID)
	if !ok {
		t.Fatal("provider resource disappeared after concurrent updates")
	}
	if updated.Name != "After Concurrent Edit" || updated.Options["operator_note"] != "retained" ||
		updated.Options[codexImageCapabilityOption] != codexImageCapabilitySupported ||
		updated.Options[codexImageCapabilityCheckedAtOption] == "" ||
		updated.Options[openAIAccountReauthorizationRequiredOption] != "true" {
		t.Fatalf("concurrent update lost resource or protected option state: %+v", updated)
	}
}
