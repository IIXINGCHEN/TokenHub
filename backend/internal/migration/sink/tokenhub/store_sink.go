package tokenhub

import (
	"fmt"
	"reflect"
	"strings"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/server"
)

type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
	ActionDelete Action = "delete"
)

// MigrationReport is a structured summary of an apply or plan operation.
type MigrationReport struct {
	Created int               `json:"created"`
	Updated int               `json:"updated"`
	Skipped int               `json:"skipped"`
	Errors  []string          `json:"errors,omitempty"`
	NewKeys map[string]string `json:"new_keys,omitempty"`
}

type Change struct {
	Resource string `json:"resource"`
	ID       string `json:"id"`
	Action   Action `json:"action"`
}

type Checkpoint struct {
	Changes []Change `json:"changes"`
}

type ApplyResult struct {
	Changes     []Change          `json:"changes"`
	Checkpoint  Checkpoint        `json:"checkpoint"`
	Report      MigrationReport   `json:"report"`
	NewKeys     map[string]string `json:"new_keys,omitempty"`
	Unsupported []string          `json:"unsupported,omitempty"`
}

type VerifyIssue struct {
	Resource string `json:"resource"`
	Ref      string `json:"ref"`
	Message  string `json:"message"`
}

type VerifyResult struct {
	OK     bool          `json:"ok"`
	Issues []VerifyIssue `json:"issues,omitempty"`
}

type RollbackResult struct {
	Changes []Change `json:"changes"`
}

type StoreSink struct {
	store    server.Store
	secrets  bundle.SecretResolver
	newKeys  map[string]string
	refIndex refIndex
}

type refIndex struct {
	providers map[string]string
	resources map[string]string
	projects  map[string]string
	users     map[string]string
	apiKeys   map[string]string
	models    map[string]string
	routes    map[string]string
	teams     map[string]string
}

func NewStoreSink(store server.Store, secrets bundle.SecretResolver) *StoreSink {
	return &StoreSink{
		store:   store,
		secrets: secrets,
		newKeys: map[string]string{},
		refIndex: refIndex{
			providers: map[string]string{},
			resources: map[string]string{},
			projects:  map[string]string{},
			users:     map[string]string{},
			apiKeys:   map[string]string{},
			models:    map[string]string{},
			routes:    map[string]string{},
			teams:     map[string]string{},
		},
	}
}

func (s *StoreSink) Apply(b *bundle.CanonicalMigrationBundle) (*ApplyResult, error) {
	if err := bundle.Validate(b); err != nil {
		return nil, err
	}
	s.newKeys = map[string]string{}
	s.refIndex = refIndex{
		providers: map[string]string{},
		resources: map[string]string{},
		projects:  map[string]string{},
		users:     map[string]string{},
		apiKeys:   map[string]string{},
		models:    map[string]string{},
		routes:    map[string]string{},
		teams:     map[string]string{},
	}
	if len(b.QuotaPolicies) > 0 {
		return nil, fmt.Errorf("quota_policies are not implemented by the TokenHub sink foundation")
	}
	result := &ApplyResult{}

	for _, item := range b.Teams {
		s.refIndex.teams[item.ExternalRef.ID] = item.ID
		change := Change{Resource: "team", ID: item.ID, Action: ActionSkip}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.Providers {
		change, err := s.applyProvider(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.ProviderResources {
		change, err := s.applyProviderResource(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.Models {
		change, err := s.applyModel(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.Users {
		change, err := s.applyUser(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.Projects {
		change, err := s.applyProject(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.APIKeys {
		change, err := s.applyAPIKey(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range b.Routes {
		change, err := s.applyRoute(item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	result.NewKeys = s.NewKeys()
	result.Report = s.buildReport(result.Changes)

	return result, nil
}

func (s *StoreSink) Verify(b *bundle.CanonicalMigrationBundle) (*VerifyResult, error) {
	if err := bundle.Validate(b); err != nil {
		return nil, err
	}
	result := &VerifyResult{OK: true}
	resolved := s.buildResolvedIndex(b)

	for _, item := range b.Providers {
		if _, found := findProviderByBusinessKey(s.store.ListProviders(), item.Spec.Name, item.Spec.Type); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider", Ref: item.ExternalRef.ID, Message: "provider not found"})
		}
	}
	for _, item := range b.ProviderResources {
		providerID := resolved.providers[item.ProviderRef]
		if _, found := findProviderResourceByBusinessKey(s.store.ListProviderResources(), providerID, item.Spec.Name); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider_resource", Ref: item.ExternalRef.ID, Message: "provider resource not found"})
		}
	}
	for _, item := range b.Models {
		if _, found := findModelByName(s.store.ListModels(), item.Spec.Name); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "model", Ref: item.ExternalRef.ID, Message: "model not found"})
		}
	}
	for _, item := range b.Users {
		if _, found := findAdminUserByIdentity(s.store.ListAdminUsers(), item.Spec); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "user", Ref: item.ExternalRef.ID, Message: "user not found"})
		}
	}
	for _, item := range b.Projects {
		teamID := resolved.teams[item.TeamRef]
		if _, found := findProjectByBusinessKey(s.store.ListProjects(), item.Spec.Name, teamID); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "project", Ref: item.ExternalRef.ID, Message: "project not found"})
		}
	}
	for _, item := range b.APIKeys {
		projectID := resolved.projects[item.ProjectRef]
		if _, found := findAPIKeyByBusinessKey(s.store.ListAPIKeys(), projectID, item.Spec.Name); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "api_key", Ref: item.ExternalRef.ID, Message: "api key not found"})
		}
	}
	for _, item := range b.Routes {
		modelName := resolved.models[item.ModelRef]
		resourceID := resolved.resources[item.ProviderResourceRef]
		if _, found := findRouteByBusinessKey(s.store.ListRoutes(), modelName, resourceID, item.Spec.ProviderModel); !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "route", Ref: item.ExternalRef.ID, Message: "route not found"})
		}
	}

	return result, nil
}

func (s *StoreSink) buildResolvedIndex(b *bundle.CanonicalMigrationBundle) refIndex {
	index := refIndex{
		providers: map[string]string{},
		resources: map[string]string{},
		projects:  map[string]string{},
		users:     map[string]string{},
		apiKeys:   map[string]string{},
		models:    map[string]string{},
		routes:    map[string]string{},
		teams:     map[string]string{},
	}
	for _, item := range b.Teams {
		index.teams[item.ExternalRef.ID] = item.ID
	}
	for _, item := range b.Providers {
		if existing, found := findProviderByBusinessKey(s.store.ListProviders(), item.Spec.Name, item.Spec.Type); found {
			index.providers[item.ExternalRef.ID] = existing.ID
		}
	}
	for _, item := range b.ProviderResources {
		providerID := item.Spec.ProviderID
		if mapped := index.providers[item.ProviderRef]; mapped != "" {
			providerID = mapped
		}
		if existing, found := findProviderResourceByBusinessKey(s.store.ListProviderResources(), providerID, item.Spec.Name); found {
			index.resources[item.ExternalRef.ID] = existing.ID
		}
	}
	for _, item := range b.Models {
		if existing, found := findModelByName(s.store.ListModels(), item.Spec.Name); found {
			index.models[item.ExternalRef.ID] = existing.Name
		}
	}
	for _, item := range b.Users {
		if existing, found := findAdminUserByIdentity(s.store.ListAdminUsers(), item.Spec); found {
			index.users[item.ExternalRef.ID] = existing.ID
		}
	}
	for _, item := range b.Projects {
		teamID := item.Spec.TeamID
		if mapped := index.teams[item.TeamRef]; mapped != "" {
			teamID = mapped
		}
		if existing, found := findProjectByBusinessKey(s.store.ListProjects(), item.Spec.Name, teamID); found {
			index.projects[item.ExternalRef.ID] = existing.ID
		}
	}
	for _, item := range b.APIKeys {
		projectID := item.Spec.ProjectID
		if mapped := index.projects[item.ProjectRef]; mapped != "" {
			projectID = mapped
		}
		if existing, found := findAPIKeyByBusinessKey(s.store.ListAPIKeys(), projectID, item.Spec.Name); found {
			index.apiKeys[item.ExternalRef.ID] = existing.ID
		}
	}
	for _, item := range b.Routes {
		modelName := item.Spec.ModelName
		if mapped := index.models[item.ModelRef]; mapped != "" {
			modelName = mapped
		}
		resourceID := item.Spec.ProviderResourceID
		if mapped := index.resources[item.ProviderResourceRef]; mapped != "" {
			resourceID = mapped
		}
		if existing, found := findRouteByBusinessKey(s.store.ListRoutes(), modelName, resourceID, item.Spec.ProviderModel); found {
			index.routes[item.ExternalRef.ID] = existing.ID
		}
	}
	return index
}

func (s *StoreSink) Rollback(checkpoint Checkpoint) (*RollbackResult, error) {
	result := &RollbackResult{}
	for index := len(checkpoint.Changes) - 1; index >= 0; index-- {
		change := checkpoint.Changes[index]
		if change.Action != ActionCreate {
			continue
		}
		var err error
		switch change.Resource {
		case "api_key":
			err = s.store.DeleteAPIKey(change.ID)
		case "project":
			err = s.store.DeleteProject(change.ID)
		case "user":
			err = s.store.DeleteAdminUser(change.ID)
		case "route":
			err = s.store.DeleteRoute(change.ID)
		case "model":
			err = s.store.DeleteModel(change.ID)
		case "provider_resource":
			err = s.store.DeleteProviderResource(change.ID)
		case "provider":
			err = s.store.DeleteProvider(change.ID)
		}
		if err != nil {
			return nil, fmt.Errorf("rollback %s %s: %w", change.Resource, change.ID, err)
		}
		result.Changes = append(result.Changes, Change{Resource: change.Resource, ID: change.ID, Action: ActionDelete})
	}
	return result, nil
}

func (s *StoreSink) resolveTeamRef(ref string) string {
	if ref == "" {
		return ""
	}
	if mapped := s.refIndex.teams[ref]; mapped != "" {
		return mapped
	}
	return ref
}

func (s *StoreSink) NewKeys() map[string]string {
	copyMap := make(map[string]string, len(s.newKeys))
	for key, value := range s.newKeys {
		copyMap[key] = value
	}
	return copyMap
}

// buildReport creates a MigrationReport from a list of changes.
func (s *StoreSink) buildReport(changes []Change) MigrationReport {
	var report MigrationReport
	for _, c := range changes {
		switch c.Action {
		case ActionCreate:
			report.Created++
		case ActionUpdate:
			report.Updated++
		case ActionSkip:
			report.Skipped++
		}
	}
	report.NewKeys = s.NewKeys()
	return report
}

// Plan performs a dry-run apply and returns a MigrationReport without
// writing any resources to the store.
func (s *StoreSink) Plan(b *bundle.CanonicalMigrationBundle) (*MigrationReport, error) {
	if err := bundle.Validate(b); err != nil {
		return nil, err
	}
	// Build a temporary ref index so we can resolve project refs for API keys.
	// This mirrors the ref index built during Apply.
	planIndex := refIndex{
		providers: map[string]string{},
		resources: map[string]string{},
		projects:  map[string]string{},
		users:     map[string]string{},
		apiKeys:   map[string]string{},
		models:    map[string]string{},
		routes:    map[string]string{},
		teams:     map[string]string{},
	}
	for _, item := range b.Teams {
		planIndex.teams[item.ExternalRef.ID] = item.ID
	}
	// Resolve project refs for Plan
	projects := s.store.ListProjects()
	for _, item := range b.Projects {
		teamID := planIndex.teams[item.TeamRef]
		_, found := findProjectByBusinessKey(projects, item.Spec.Name, teamID)
		if found {
			planIndex.projects[item.ExternalRef.ID] = "existing"
		}
	}

	var report MigrationReport
	for range b.Teams {
		report.Skipped++
	}
	providers := s.store.ListProviders()
	for _, item := range b.Providers {
		_, found := findProviderByBusinessKey(providers, item.Spec.Name, item.Spec.Type)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	resources := s.store.ListProviderResources()
	for _, item := range b.ProviderResources {
		providerID := planIndex.providers[item.ProviderRef]
		_, found := findProviderResourceByBusinessKey(resources, providerID, item.Spec.Name)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	models := s.store.ListModels()
	for _, item := range b.Models {
		_, found := findModelByName(models, item.Spec.Name)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	users := s.store.ListAdminUsers()
	for _, item := range b.Users {
		_, found := findAdminUserByIdentity(users, item.Spec)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	for _, item := range b.Projects {
		teamID := planIndex.teams[item.TeamRef]
		_, found := findProjectByBusinessKey(projects, item.Spec.Name, teamID)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	keys := s.store.ListAPIKeys()
	for _, item := range b.APIKeys {
		projectID := planIndex.projects[item.ProjectRef]
		_, found := findAPIKeyByBusinessKey(keys, projectID, item.Spec.Name)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	routes := s.store.ListRoutes()
	for _, item := range b.Routes {
		_, found := findRouteByBusinessKey(routes, item.Spec.ModelName, item.Spec.ProviderResourceID, item.Spec.ProviderModel)
		if found {
			report.Updated++
		} else {
			report.Created++
		}
	}
	return &report, nil
}

func (s *StoreSink) applyProvider(item bundle.ProviderRef) (Change, error) {
	spec := item.Spec
	if item.APIKeySecret != nil && !item.APIKeySecret.IsZero() {
		secret, err := s.secrets.Resolve(*item.APIKeySecret)
		if err != nil {
			return Change{}, fmt.Errorf("resolve provider secret %s: %w", item.ExternalRef.ID, err)
		}
		spec.APIKey = secret
	}

	for _, existing := range s.store.ListProviders() {
		if existing.Name == spec.Name && existing.Type == spec.Type {
			if sameProvider(existing, spec) {
				s.refIndex.providers[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "provider", ID: existing.ID, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateProvider(existing.ID, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.providers[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "provider", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}
	created := s.store.AddProvider(spec)
	s.refIndex.providers[item.ExternalRef.ID] = created.ID
	return Change{Resource: "provider", ID: created.ID, Action: ActionCreate}, nil
}

func (s *StoreSink) applyProviderResource(item bundle.ProviderResourceRef) (Change, error) {
	spec := item.Spec
	if providerID := s.refIndex.providers[item.ProviderRef]; providerID != "" {
		spec.ProviderID = providerID
	}
	if item.APIKeySecret != nil && !item.APIKeySecret.IsZero() {
		secret, err := s.secrets.Resolve(*item.APIKeySecret)
		if err != nil {
			return Change{}, fmt.Errorf("resolve provider resource secret %s: %w", item.ExternalRef.ID, err)
		}
		spec.APIKey = secret
	}

	for _, existing := range s.store.ListProviderResources() {
		if existing.ProviderID == spec.ProviderID && existing.Name == spec.Name {
			if sameProviderResource(existing, spec) {
				s.refIndex.resources[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "provider_resource", ID: existing.ID, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateProviderResource(existing.ID, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.resources[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "provider_resource", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}
	created, err := s.store.AddProviderResource(spec)
	if err != nil {
		return Change{}, err
	}
	s.refIndex.resources[item.ExternalRef.ID] = created.ID
	return Change{Resource: "provider_resource", ID: created.ID, Action: ActionCreate}, nil
}

func (s *StoreSink) applyModel(item bundle.ModelRef) (Change, error) {
	spec := item.Spec
	for _, existing := range s.store.ListModels() {
		if existing.Name == spec.Name {
			if sameModel(existing, spec) {
				s.refIndex.models[item.ExternalRef.ID] = existing.Name
				return Change{Resource: "model", ID: existing.Name, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateModel(existing.Name, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.models[item.ExternalRef.ID] = updated.Name
			return Change{Resource: "model", ID: updated.Name, Action: ActionUpdate}, nil
		}
	}
	created := s.store.AddModel(spec)
	s.refIndex.models[item.ExternalRef.ID] = created.Name
	return Change{Resource: "model", ID: created.Name, Action: ActionCreate}, nil
}

func (s *StoreSink) applyRoute(item bundle.RouteRef) (Change, error) {
	spec := item.Spec
	if modelName := s.refIndex.models[item.ModelRef]; modelName != "" {
		spec.ModelName = modelName
	}
	if providerID := s.refIndex.providers[item.ProviderRef]; providerID != "" {
		spec.ProviderID = providerID
	}
	if resourceID := s.refIndex.resources[item.ProviderResourceRef]; resourceID != "" {
		spec.ProviderResourceID = resourceID
	}
	for _, existing := range s.store.ListRoutes() {
		if existing.ModelName == spec.ModelName && existing.ProviderResourceID == spec.ProviderResourceID && existing.ProviderModel == spec.ProviderModel {
			if sameRoute(existing, spec) {
				s.refIndex.routes[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "route", ID: existing.ID, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateRoute(existing.ID, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.routes[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "route", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}
	created := s.store.AddRoute(spec)
	s.refIndex.routes[item.ExternalRef.ID] = created.ID
	return Change{Resource: "route", ID: created.ID, Action: ActionCreate}, nil
}

func (s *StoreSink) applyUser(item bundle.UserRef) (Change, error) {
	spec := item.Spec
	spec.TeamID = s.resolveTeamRef(item.TeamRef)
	for _, existing := range s.store.ListAdminUsers() {
		if sameUser(existing, spec) {
			if sameAdminUser(existing, spec) {
				s.refIndex.users[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "user", ID: existing.ID, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateAdminUser(existing.ID, spec, "")
			if err != nil {
				return Change{}, err
			}
			s.refIndex.users[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "user", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}
	created, err := s.store.CreateAdminUser(spec, "migration-password-placeholder")
	if err != nil {
		return Change{}, err
	}
	s.refIndex.users[item.ExternalRef.ID] = created.ID
	return Change{Resource: "user", ID: created.ID, Action: ActionCreate}, nil
}

func (s *StoreSink) applyProject(item bundle.ProjectRef) (Change, error) {
	spec := item.Spec
	spec.TeamID = s.resolveTeamRef(item.TeamRef)
	if userID := s.refIndex.users[spec.OwnerUserID]; userID != "" {
		spec.OwnerUserID = userID
	}
	for _, existing := range s.store.ListProjects() {
		if existing.Name == spec.Name && existing.TeamID == spec.TeamID {
			if sameProject(existing, spec) {
				s.refIndex.projects[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "project", ID: existing.ID, Action: ActionSkip}, nil
			}
			updated, err := s.store.UpdateProject(existing.ID, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.projects[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "project", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}
	created := s.store.CreateProject(spec)
	s.refIndex.projects[item.ExternalRef.ID] = created.ID
	return Change{Resource: "project", ID: created.ID, Action: ActionCreate}, nil
}

func (s *StoreSink) applyAPIKey(item bundle.APIKeyRef) (Change, error) {
	spec := item.Spec
	projectID := spec.ProjectID
	if mapped := s.refIndex.projects[item.ProjectRef]; mapped != "" {
		projectID = mapped
	}
	spec.ProjectID = projectID
	for _, existing := range s.store.ListAPIKeys() {
		if existing.ProjectID == projectID && existing.Name == spec.Name {
			if sameAPIKey(existing, spec) {
				s.refIndex.apiKeys[item.ExternalRef.ID] = existing.ID
				return Change{Resource: "api_key", ID: existing.ID, Action: ActionSkip}, nil
			}
			if item.KeySecret != nil && !item.KeySecret.IsZero() {
				return Change{}, fmt.Errorf("api key %s already exists with different metadata; secret-backed updates are not supported", item.ExternalRef.ID)
			}
			updated, err := s.store.UpdateAPIKey(existing.ID, spec)
			if err != nil {
				return Change{}, err
			}
			s.refIndex.apiKeys[item.ExternalRef.ID] = updated.ID
			return Change{Resource: "api_key", ID: updated.ID, Action: ActionUpdate}, nil
		}
	}

	rawSecret := ""
	if item.KeySecret != nil && !item.KeySecret.IsZero() {
		resolved, err := s.secrets.Resolve(*item.KeySecret)
		if err != nil {
			return Change{}, fmt.Errorf("resolve api key secret %s: %w", item.ExternalRef.ID, err)
		}
		rawSecret = resolved
	}
	spec.ProjectID = projectID
	created, _, err := s.store.CreateAPIKey(projectID, spec, rawSecret)
	if err != nil {
		return Change{}, err
	}
	if rawSecret != "" {
		s.newKeys[item.ExternalRef.ID] = rawSecret
	}
	s.refIndex.apiKeys[item.ExternalRef.ID] = created.ID
	return Change{Resource: "api_key", ID: created.ID, Action: ActionCreate}, nil
}

func sameUser(left server.AdminUser, right server.AdminUser) bool {
	if strings.EqualFold(strings.TrimSpace(left.Email), strings.TrimSpace(right.Email)) && strings.TrimSpace(right.Email) != "" {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(left.Username), strings.TrimSpace(right.Username)) && strings.TrimSpace(right.Username) != "" {
		return true
	}
	return false
}

func findProviderByBusinessKey(items []server.Provider, name string, providerType string) (server.Provider, bool) {
	for _, item := range items {
		if item.Name == name && item.Type == providerType {
			return item, true
		}
	}
	return server.Provider{}, false
}

func findProviderResourceByBusinessKey(items []server.ProviderResource, providerID string, name string) (server.ProviderResource, bool) {
	for _, item := range items {
		if item.ProviderID == providerID && item.Name == name {
			return item, true
		}
	}
	return server.ProviderResource{}, false
}

func findModelByName(items []server.Model, name string) (server.Model, bool) {
	for _, item := range items {
		if item.Name == name {
			return item, true
		}
	}
	return server.Model{}, false
}

func findAdminUserByIdentity(items []server.AdminUser, spec server.AdminUser) (server.AdminUser, bool) {
	for _, item := range items {
		if sameUser(item, spec) {
			return item, true
		}
	}
	return server.AdminUser{}, false
}

func findProjectByBusinessKey(items []server.Project, name string, teamID string) (server.Project, bool) {
	for _, item := range items {
		if item.Name == name && item.TeamID == teamID {
			return item, true
		}
	}
	return server.Project{}, false
}

func findAPIKeyByBusinessKey(items []server.APIKey, projectID string, name string) (server.APIKey, bool) {
	for _, item := range items {
		if item.ProjectID == projectID && item.Name == name {
			return item, true
		}
	}
	return server.APIKey{}, false
}

func findRouteByBusinessKey(items []server.ModelRoute, modelName string, resourceID string, providerModel string) (server.ModelRoute, bool) {
	for _, item := range items {
		if item.ModelName == modelName && item.ProviderResourceID == resourceID && item.ProviderModel == providerModel {
			return item, true
		}
	}
	return server.ModelRoute{}, false
}

func sameProvider(left server.Provider, right server.Provider) bool {
	left.APIKey = ""
	right.APIKey = ""
	left.CreatedAt = right.CreatedAt
	left.Headers = normalizeStringMap(left.Headers)
	right.Headers = normalizeStringMap(right.Headers)
	left.Options = normalizeStringMap(left.Options)
	right.Options = normalizeStringMap(right.Options)
	return reflect.DeepEqual(left, right)
}

func sameProviderResource(left server.ProviderResource, right server.ProviderResource) bool {
	left.APIKey = ""
	right.APIKey = ""
	left.CredentialBlob = ""
	right.CredentialBlob = ""
	left.Credentials = nil
	right.Credentials = nil
	left.CredentialSummary = nil
	right.CredentialSummary = nil
	left.CreatedAt = right.CreatedAt
	left.UpdatedAt = right.UpdatedAt
	left.LastUsedAt = right.LastUsedAt
	left.LastCheckedAt = right.LastCheckedAt
	left.CooldownUntil = right.CooldownUntil
	left.Headers = normalizeStringMap(left.Headers)
	right.Headers = normalizeStringMap(right.Headers)
	left.Options = normalizeStringMap(left.Options)
	right.Options = normalizeStringMap(right.Options)
	left.CredentialSummary = normalizeStringMap(left.CredentialSummary)
	right.CredentialSummary = normalizeStringMap(right.CredentialSummary)
	return reflect.DeepEqual(left, right)
}

func sameModel(left server.Model, right server.Model) bool {
	if right.ID == "" {
		right.ID = right.Name
	}
	left.CreatedAt = right.CreatedAt
	left.InputModalities = normalizeStringSlice(left.InputModalities)
	right.InputModalities = normalizeStringSlice(right.InputModalities)
	left.OutputModalities = normalizeStringSlice(left.OutputModalities)
	right.OutputModalities = normalizeStringSlice(right.OutputModalities)
	left.Capabilities = normalizeStringSlice(left.Capabilities)
	right.Capabilities = normalizeStringSlice(right.Capabilities)
	left.SupportedParameters = normalizeStringSlice(left.SupportedParameters)
	right.SupportedParameters = normalizeStringSlice(right.SupportedParameters)
	left.Metadata = normalizeStringMap(left.Metadata)
	right.Metadata = normalizeStringMap(right.Metadata)
	return reflect.DeepEqual(left, right)
}

func sameRoute(left server.ModelRoute, right server.ModelRoute) bool {
	if right.Strategy == "" {
		right.Strategy = server.RouteStrategyBalanced
	}
	if right.Weight <= 0 {
		right.Weight = 1
	}
	left.CreatedAt = right.CreatedAt
	left.LastUsedAt = right.LastUsedAt
	return reflect.DeepEqual(left, right)
}

func sameAdminUser(left server.AdminUser, right server.AdminUser) bool {
	return strings.TrimSpace(left.Username) == strings.TrimSpace(right.Username) &&
		strings.TrimSpace(left.Name) == strings.TrimSpace(right.Name) &&
		strings.TrimSpace(left.Email) == strings.TrimSpace(right.Email) &&
		strings.TrimSpace(left.Role) == strings.TrimSpace(right.Role) &&
		strings.TrimSpace(left.TeamID) == strings.TrimSpace(right.TeamID) &&
		strings.TrimSpace(left.Status) == strings.TrimSpace(right.Status)
}

func sameProject(left server.Project, right server.Project) bool {
	return strings.TrimSpace(left.Name) == strings.TrimSpace(right.Name) &&
		strings.TrimSpace(left.TeamID) == strings.TrimSpace(right.TeamID) &&
		strings.TrimSpace(left.OwnerUserID) == strings.TrimSpace(right.OwnerUserID) &&
		strings.TrimSpace(left.CostCenter) == strings.TrimSpace(right.CostCenter) &&
		strings.TrimSpace(left.Status) == strings.TrimSpace(right.Status) &&
		strings.TrimSpace(left.DefaultQuotaRef) == strings.TrimSpace(right.DefaultQuotaRef)
}

func sameAPIKey(left server.APIKey, right server.APIKey) bool {
	if right.Group == "" {
		right.Group = "default"
	}
	if right.Status == "" {
		right.Status = server.StatusActive
	}
	return strings.TrimSpace(left.ProjectID) == strings.TrimSpace(right.ProjectID) &&
		strings.TrimSpace(left.Name) == strings.TrimSpace(right.Name) &&
		strings.TrimSpace(left.Group) == strings.TrimSpace(right.Group) &&
		reflect.DeepEqual(normalizeStringSlice(left.Allowed), normalizeStringSlice(right.Allowed)) &&
		reflect.DeepEqual(normalizeStringSlice(left.IPAllowlist), normalizeStringSlice(right.IPAllowlist)) &&
		reflect.DeepEqual(left.Limits, right.Limits) &&
		strings.TrimSpace(left.Status) == strings.TrimSpace(right.Status) &&
		reflect.DeepEqual(left.ExpiresAt, right.ExpiresAt) &&
		reflect.DeepEqual(normalizeStringMap(left.Metadata), normalizeStringMap(right.Metadata))
}

func normalizeStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	return input
}

func normalizeStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	return input
}
