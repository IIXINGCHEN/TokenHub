//go:build integration

package server

import (
	"testing"

	"gorm.io/gorm"
)

func testPostgresRouteLookupFailureFallback(t *testing.T, store *GormStore, target string) {
	t.Helper()
	suffix := NewID("route-lookup-fallback")
	model := store.AddModel(Model{
		ID: "model_" + suffix, Name: "model-" + suffix, Modality: "chat", Status: StatusActive,
	})
	providers := make([]Provider, 0, 2)
	resources := make([]ProviderResource, 0, 2)
	routes := make([]ModelRoute, 0, 2)
	for index, label := range []string{"failing", "healthy"} {
		provider := store.AddProvider(Provider{
			ID: "prv_" + label + "_" + suffix, Name: "Route lookup " + label,
			Type: ProviderMock, Status: StatusActive, Healthy: true,
		})
		resource, err := store.AddProviderResource(ProviderResource{
			ID: "rsrc_" + label + "_" + suffix, ProviderID: provider.ID,
			Name: "Route lookup " + label, Status: StatusActive, Healthy: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		route := store.AddRoute(ModelRoute{
			ID: "route_" + label + "_" + suffix, ModelName: model.Name,
			ProviderID: provider.ID, ProviderResourceID: resource.ID, ProviderModel: model.Name,
			Priority: index + 1, Weight: 100, Status: StatusActive,
		})
		providers = append(providers, provider)
		resources = append(resources, resource)
		routes = append(routes, route)
	}
	t.Cleanup(func() {
		for _, route := range routes {
			_ = store.db.Where("id = ?", route.ID).Delete(&ModelRoute{}).Error
		}
		for _, resource := range resources {
			_ = store.db.Where("id = ?", resource.ID).Delete(&ProviderResource{}).Error
		}
		for _, provider := range providers {
			_ = store.db.Where("id = ?", provider.ID).Delete(&Provider{}).Error
		}
		_ = store.db.Where("name = ?", model.Name).Delete(&Model{}).Error
	})

	attempts := 0
	databaseErrors := 0
	callbackName := "test:fail-route-candidate-lookup:" + target + ":" + suffix
	resultCallbackName := callbackName + ":result"
	if err := store.db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != target {
			return
		}
		attempts++
		if attempts == 1 {
			tx.Statement.Table = "tokenhub_missing_" + target + "_" + suffix
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Callback().Query().After("gorm:query").Register(resultCallbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == target && tx.Error != nil {
			databaseErrors++
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = store.db.Callback().Query().Remove(callbackName)
		_ = store.db.Callback().Query().Remove(resultCallbackName)
	})

	selections, err := store.SelectRouteCandidates(model.Name)
	if err != nil {
		t.Fatalf("lookup failure rejected unaffected candidates: %v", err)
	}
	if attempts != 2 || databaseErrors != 1 {
		t.Fatalf("lookup attempts/errors = %d/%d, want 2/1", attempts, databaseErrors)
	}
	if len(selections) != 1 || selections[0].Route.ID != routes[1].ID {
		t.Fatalf("fallback selections = %+v, want route %s", selections, routes[1].ID)
	}
}
