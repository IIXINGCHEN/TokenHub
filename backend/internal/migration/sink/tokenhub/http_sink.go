package tokenhub

import (
	"context"
	"fmt"
	"strings"

	"tokenhub/backend/internal/migration/bundle"
	"tokenhub/backend/internal/server"
)

type HTTPSink struct {
	client   *AdminAPIClient
	secrets  bundle.SecretResolver
	newKeys  map[string]string
	refIndex refIndex
}

func NewHTTPSink(client *AdminAPIClient, secrets bundle.SecretResolver) *HTTPSink {
	return &HTTPSink{
		client:  client,
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

func (s *HTTPSink) Apply(ctx context.Context, migrationBundle *bundle.CanonicalMigrationBundle) (*ApplyResult, error) {
	if err := bundle.Validate(migrationBundle); err != nil {
		return nil, err
	}
	providers, err := s.client.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := s.client.ListProviderResources(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.client.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.client.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.client.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.client.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}

	result := &ApplyResult{}
	for _, team := range migrationBundle.Teams {
		s.refIndex.teams[team.ExternalRef.ID] = team.ID
		change := Change{Resource: "team", ID: team.ID, Action: ActionSkip}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.Providers {
		change, created, err := s.applyProviderHTTP(ctx, providers, item)
		if err != nil {
			return nil, err
		}
		if created.ID != "" {
			providers = append(providers, created)
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.ProviderResources {
		change, created, err := s.applyProviderResourceHTTP(ctx, resources, item)
		if err != nil {
			return nil, err
		}
		if created.ID != "" {
			resources = append(resources, created)
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.Models {
		change, created, err := s.applyModelHTTP(ctx, models, item)
		if err != nil {
			return nil, err
		}
		if created.Name != "" {
			models = append(models, created)
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.Users {
		change, err := s.applyUserHTTP(ctx, users, item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	users, _ = s.client.ListUsers(ctx)
	for _, item := range migrationBundle.Projects {
		change, created, err := s.applyProjectHTTP(ctx, projects, users, item)
		if err != nil {
			return nil, err
		}
		if created.ID != "" {
			projects = append(projects, created)
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.APIKeys {
		change, err := s.applyAPIKeyHTTP(ctx, keys, item)
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	for _, item := range migrationBundle.Routes {
		change, created, err := s.applyRouteHTTP(ctx, routes, item)
		if err != nil {
			return nil, err
		}
		if created.ID != "" {
			routes = append(routes, created)
		}
		result.Changes = append(result.Changes, change)
		result.Checkpoint.Changes = append(result.Checkpoint.Changes, change)
	}
	result.NewKeys = s.newKeys
	result.Report = buildReportFromChanges(result.Changes, result.NewKeys)
	return result, nil
}

func (s *HTTPSink) Plan(ctx context.Context, migrationBundle *bundle.CanonicalMigrationBundle) (*MigrationReport, error) {
	if err := bundle.Validate(migrationBundle); err != nil {
		return nil, err
	}
	providers, err := s.client.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := s.client.ListProviderResources(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.client.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.client.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.client.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.client.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, team := range migrationBundle.Teams {
		s.refIndex.teams[team.ExternalRef.ID] = team.ID
		changes = append(changes, Change{Resource: "team", ID: team.ID, Action: ActionSkip})
	}
	for _, item := range migrationBundle.Providers {
		if existing, found := findProviderByBusinessKey(providers, item.Spec.Name, item.Spec.Type); found {
			s.refIndex.providers[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "provider", ID: existing.ID, Action: chooseActionProvider(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "provider", ID: item.Spec.ID, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.ProviderResources {
		providerID := s.refIndex.providers[item.ProviderRef]
		if existing, found := findProviderResourceByBusinessKey(resources, providerID, item.Spec.Name); found {
			s.refIndex.resources[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "provider_resource", ID: existing.ID, Action: chooseActionResource(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "provider_resource", ID: item.Spec.ID, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.Models {
		if existing, found := findModelByName(models, item.Spec.Name); found {
			s.refIndex.models[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "model", ID: existing.Name, Action: chooseActionModel(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "model", ID: item.Spec.Name, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.Users {
		if existing, found := findAdminUserByIdentity(users, item.Spec); found {
			s.refIndex.users[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "user", ID: existing.ID, Action: chooseActionUser(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "user", ID: item.Spec.Email, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.Projects {
		teamID := s.refIndex.teams[item.TeamRef]
		if existing, found := findProjectByBusinessKey(projects, item.Spec.Name, teamID); found {
			s.refIndex.projects[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "project", ID: existing.ID, Action: chooseActionProject(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "project", ID: item.Spec.Name, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.APIKeys {
		projectID := s.refIndex.projects[item.ProjectRef]
		if existing, found := findAPIKeyByBusinessKey(keys, projectID, item.Spec.Name); found {
			s.refIndex.apiKeys[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "api_key", ID: existing.ID, Action: chooseActionAPIKey(existing, item.Spec)})
		} else {
			changes = append(changes, Change{Resource: "api_key", ID: item.Spec.Name, Action: ActionCreate})
		}
	}
	for _, item := range migrationBundle.Routes {
		resourceID := s.refIndex.resources[item.ProviderResourceRef]
		modelName := item.Spec.ModelName
		if strings.TrimSpace(modelName) == "" {
			if modelID := s.refIndex.models[item.ModelRef]; modelID != "" {
				modelName = modelID
			}
		}
		if existing, found := findRouteByBusinessKey(routes, modelName, resourceID, item.Spec.ProviderModel); found {
			s.refIndex.routes[item.ExternalRef.ID] = existing.ID
			changes = append(changes, Change{Resource: "route", ID: existing.ID, Action: chooseActionRoute(existing, item.Spec, resourceID, modelName)})
		} else {
			changes = append(changes, Change{Resource: "route", ID: item.Spec.ID, Action: ActionCreate})
		}
	}
	report := buildReportFromChanges(changes, nil)
	return &report, nil
}

func (s *HTTPSink) Verify(ctx context.Context, migrationBundle *bundle.CanonicalMigrationBundle) (*VerifyResult, error) {
	if err := bundle.Validate(migrationBundle); err != nil {
		return nil, err
	}
	providers, err := s.client.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := s.client.ListProviderResources(ctx)
	if err != nil {
		return nil, err
	}
	models, err := s.client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	users, err := s.client.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	projects, err := s.client.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := s.client.ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	routes, err := s.client.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}

	resolved := refIndex{
		providers: map[string]string{},
		resources: map[string]string{},
		projects:  map[string]string{},
		users:     map[string]string{},
		apiKeys:   map[string]string{},
		models:    map[string]string{},
		routes:    map[string]string{},
		teams:     map[string]string{},
	}
	for _, item := range migrationBundle.Teams {
		resolved.teams[item.ExternalRef.ID] = item.ID
	}

	result := &VerifyResult{OK: true}

	for _, item := range migrationBundle.Providers {
		existing, found := findProviderByBusinessKey(providers, item.Spec.Name, item.Spec.Type)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider", Ref: item.ExternalRef.ID, Message: "provider not found"})
			continue
		}
		resolved.providers[item.ExternalRef.ID] = existing.ID
		if !sameProvider(existing, item.Spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider", Ref: item.ExternalRef.ID, Message: "provider differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.ProviderResources {
		providerID := resolved.providers[item.ProviderRef]
		spec := item.Spec
		spec.ProviderID = providerID
		existing, found := findProviderResourceByBusinessKey(resources, providerID, item.Spec.Name)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider_resource", Ref: item.ExternalRef.ID, Message: "provider resource not found"})
			continue
		}
		resolved.resources[item.ExternalRef.ID] = existing.ID
		if !sameProviderResource(existing, spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "provider_resource", Ref: item.ExternalRef.ID, Message: "provider resource differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.Models {
		existing, found := findModelByName(models, item.Spec.Name)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "model", Ref: item.ExternalRef.ID, Message: "model not found"})
			continue
		}
		resolved.models[item.ExternalRef.ID] = existing.Name
		if !sameModel(existing, item.Spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "model", Ref: item.ExternalRef.ID, Message: "model differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.Users {
		existing, found := findAdminUserByIdentity(users, item.Spec)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "user", Ref: item.ExternalRef.ID, Message: "user not found"})
			continue
		}
		resolved.users[item.ExternalRef.ID] = existing.ID
		if !sameAdminUser(existing, item.Spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "user", Ref: item.ExternalRef.ID, Message: "user differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.Projects {
		teamID := resolved.teams[item.TeamRef]
		spec := item.Spec
		spec.TeamID = teamID
		if ownerRef := strings.TrimSpace(spec.OwnerUserID); ownerRef != "" {
			if resolvedOwner := resolved.users[ownerRef]; resolvedOwner != "" {
				spec.OwnerUserID = resolvedOwner
			}
		}
		existing, found := findProjectByBusinessKey(projects, item.Spec.Name, teamID)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "project", Ref: item.ExternalRef.ID, Message: "project not found"})
			continue
		}
		resolved.projects[item.ExternalRef.ID] = existing.ID
		if !sameProject(existing, spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "project", Ref: item.ExternalRef.ID, Message: "project differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.APIKeys {
		projectID := resolved.projects[item.ProjectRef]
		spec := item.Spec
		spec.ProjectID = projectID
		existing, found := findAPIKeyByBusinessKey(keys, projectID, item.Spec.Name)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "api_key", Ref: item.ExternalRef.ID, Message: "api key not found"})
			continue
		}
		resolved.apiKeys[item.ExternalRef.ID] = existing.ID
		if !sameAPIKey(existing, spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "api_key", Ref: item.ExternalRef.ID, Message: "api key differs from expected spec"})
		}
	}
	for _, item := range migrationBundle.Routes {
		modelName := item.Spec.ModelName
		if modelName == "" {
			modelName = resolved.models[item.ModelRef]
		}
		resourceID := resolved.resources[item.ProviderResourceRef]
		if resourceID == "" {
			resourceID = item.Spec.ProviderResourceID
		}
		spec := item.Spec
		spec.ModelName = modelName
		spec.ProviderResourceID = resourceID
		if spec.ProviderID == "" {
			spec.ProviderID = resolved.providers[item.ProviderRef]
		}
		existing, found := findRouteByBusinessKey(routes, modelName, resourceID, spec.ProviderModel)
		if !found {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "route", Ref: item.ExternalRef.ID, Message: "route not found"})
			continue
		}
		resolved.routes[item.ExternalRef.ID] = existing.ID
		if !sameRoute(existing, spec) {
			result.OK = false
			result.Issues = append(result.Issues, VerifyIssue{Resource: "route", Ref: item.ExternalRef.ID, Message: "route differs from expected spec"})
		}
	}
	return result, nil
}

func (s *HTTPSink) Rollback(ctx context.Context, checkpoint Checkpoint) (*RollbackResult, error) {
	result := &RollbackResult{}
	for index := len(checkpoint.Changes) - 1; index >= 0; index-- {
		change := checkpoint.Changes[index]
		if change.Action != ActionCreate {
			continue
		}
		var err error
		switch change.Resource {
		case "provider":
			err = s.client.DeleteProvider(ctx, change.ID)
		case "provider_resource":
			err = s.client.DeleteProviderResource(ctx, change.ID)
		case "model":
			err = s.client.DeleteModel(ctx, change.ID)
		case "route":
			err = s.client.DeleteRoute(ctx, change.ID)
		case "project":
			err = s.client.DeleteProject(ctx, change.ID)
		case "api_key":
			err = s.client.DeleteAPIKey(ctx, change.ID)
		default:
			continue
		}
		if err != nil {
			return nil, err
		}
		result.Changes = append(result.Changes, Change{Resource: change.Resource, ID: change.ID, Action: ActionDelete})
	}
	return result, nil
}

func buildReportFromChanges(changes []Change, newKeys map[string]string) MigrationReport {
	var report MigrationReport
	for _, change := range changes {
		switch change.Action {
		case ActionCreate:
			report.Created++
		case ActionUpdate:
			report.Updated++
		case ActionSkip:
			report.Skipped++
		}
	}
	if len(newKeys) > 0 {
		report.NewKeys = newKeys
	}
	return report
}

func chooseActionProvider(existing server.Provider, desired server.Provider) Action {
	if sameProvider(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionResource(existing server.ProviderResource, desired server.ProviderResource) Action {
	if sameProviderResource(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionModel(existing server.Model, desired server.Model) Action {
	if sameModel(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionUser(existing server.AdminUser, desired server.AdminUser) Action {
	if sameAdminUser(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionProject(existing server.Project, desired server.Project) Action {
	if sameProject(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionAPIKey(existing server.APIKey, desired server.APIKey) Action {
	if sameAPIKey(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func chooseActionRoute(existing server.ModelRoute, desired server.ModelRoute, resourceID string, modelName string) Action {
	desired.ProviderResourceID = resourceID
	desired.ModelName = modelName
	if sameRoute(existing, desired) {
		return ActionSkip
	}
	return ActionUpdate
}

func (s *HTTPSink) applyProviderHTTP(ctx context.Context, existing []server.Provider, item bundle.ProviderRef) (Change, server.Provider, error) {
	spec := item.Spec
	if item.APIKeySecret != nil && !item.APIKeySecret.IsZero() {
		resolved, err := s.secrets.Resolve(*item.APIKeySecret)
		if err != nil {
			return Change{}, server.Provider{}, err
		}
		spec.APIKey = resolved
	}
	if current, found := findProviderByBusinessKey(existing, spec.Name, spec.Type); found {
		s.refIndex.providers[item.ExternalRef.ID] = current.ID
		action := chooseActionProvider(current, spec)
		if action == ActionSkip {
			return Change{Resource: "provider", ID: current.ID, Action: ActionSkip}, server.Provider{}, nil
		}
		updated, err := s.client.UpdateProvider(ctx, current.ID, spec)
		return Change{Resource: "provider", ID: current.ID, Action: ActionUpdate}, updated, err
	}
	created, err := s.client.CreateProvider(ctx, spec)
	if err != nil {
		return Change{}, server.Provider{}, err
	}
	s.refIndex.providers[item.ExternalRef.ID] = created.ID
	return Change{Resource: "provider", ID: created.ID, Action: ActionCreate}, created, nil
}

func (s *HTTPSink) applyProviderResourceHTTP(ctx context.Context, existing []server.ProviderResource, item bundle.ProviderResourceRef) (Change, server.ProviderResource, error) {
	spec := item.Spec
	spec.ProviderID = s.refIndex.providers[item.ProviderRef]
	if spec.ProviderID == "" {
		return Change{}, server.ProviderResource{}, fmt.Errorf("missing provider ref for %s", item.ExternalRef.ID)
	}
	if item.APIKeySecret != nil && !item.APIKeySecret.IsZero() {
		resolved, err := s.secrets.Resolve(*item.APIKeySecret)
		if err != nil {
			return Change{}, server.ProviderResource{}, err
		}
		spec.APIKey = resolved
	}
	if current, found := findProviderResourceByBusinessKey(existing, spec.ProviderID, spec.Name); found {
		s.refIndex.resources[item.ExternalRef.ID] = current.ID
		action := chooseActionResource(current, spec)
		if action == ActionSkip {
			return Change{Resource: "provider_resource", ID: current.ID, Action: ActionSkip}, server.ProviderResource{}, nil
		}
		updated, err := s.client.UpdateProviderResource(ctx, current.ID, spec)
		return Change{Resource: "provider_resource", ID: current.ID, Action: ActionUpdate}, updated, err
	}
	created, err := s.client.CreateProviderResource(ctx, spec)
	if err != nil {
		return Change{}, server.ProviderResource{}, err
	}
	s.refIndex.resources[item.ExternalRef.ID] = created.ID
	return Change{Resource: "provider_resource", ID: created.ID, Action: ActionCreate}, created, nil
}

func (s *HTTPSink) applyModelHTTP(ctx context.Context, existing []server.Model, item bundle.ModelRef) (Change, server.Model, error) {
	spec := item.Spec
	if current, found := findModelByName(existing, spec.Name); found {
		s.refIndex.models[item.ExternalRef.ID] = current.Name
		action := chooseActionModel(current, spec)
		if action == ActionSkip {
			return Change{Resource: "model", ID: current.Name, Action: ActionSkip}, server.Model{}, nil
		}
		updated, err := s.client.UpdateModel(ctx, current.Name, spec)
		return Change{Resource: "model", ID: current.Name, Action: ActionUpdate}, updated, err
	}
	created, err := s.client.CreateModel(ctx, spec)
	if err != nil {
		return Change{}, server.Model{}, err
	}
	s.refIndex.models[item.ExternalRef.ID] = created.Name
	return Change{Resource: "model", ID: created.Name, Action: ActionCreate}, created, nil
}

func (s *HTTPSink) applyUserHTTP(ctx context.Context, existing []server.AdminUser, item bundle.UserRef) (Change, error) {
	if current, found := findAdminUserByIdentity(existing, item.Spec); found {
		s.refIndex.users[item.ExternalRef.ID] = current.ID
		action := chooseActionUser(current, item.Spec)
		return Change{Resource: "user", ID: current.ID, Action: action}, nil
	}
	csv := fmt.Sprintf("username,name,email,role,team_id,status\n%s,%s,%s,%s,%s,%s\n", item.Spec.Username, item.Spec.Name, item.Spec.Email, item.Spec.Role, item.Spec.TeamID, item.Spec.Status)
	if err := s.client.ImportUsersCSV(ctx, csv); err != nil {
		return Change{}, err
	}
	return Change{Resource: "user", ID: item.Spec.Email, Action: ActionCreate}, nil
}

func (s *HTTPSink) applyProjectHTTP(ctx context.Context, existingProjects []server.Project, existingUsers []server.AdminUser, item bundle.ProjectRef) (Change, server.Project, error) {
	spec := item.Spec
	spec.TeamID = s.refIndex.teams[item.TeamRef]
	if ownerRef := strings.TrimSpace(spec.OwnerUserID); ownerRef != "" {
		if ownerID := s.refIndex.users[ownerRef]; ownerID != "" {
			spec.OwnerUserID = ownerID
		} else {
			for _, user := range existingUsers {
				if user.Email == ownerRef || user.ID == ownerRef || user.Username == ownerRef {
					spec.OwnerUserID = user.ID
					break
				}
			}
		}
	}
	if current, found := findProjectByBusinessKey(existingProjects, spec.Name, spec.TeamID); found {
		s.refIndex.projects[item.ExternalRef.ID] = current.ID
		action := chooseActionProject(current, spec)
		if action == ActionSkip {
			return Change{Resource: "project", ID: current.ID, Action: ActionSkip}, server.Project{}, nil
		}
		updated, err := s.client.UpdateProject(ctx, current.ID, spec)
		return Change{Resource: "project", ID: current.ID, Action: ActionUpdate}, updated, err
	}
	created, err := s.client.CreateProject(ctx, spec)
	if err != nil {
		return Change{}, server.Project{}, err
	}
	s.refIndex.projects[item.ExternalRef.ID] = created.ID
	return Change{Resource: "project", ID: created.ID, Action: ActionCreate}, created, nil
}

func (s *HTTPSink) applyAPIKeyHTTP(ctx context.Context, existingKeys []server.APIKey, item bundle.APIKeyRef) (Change, error) {
	projectID := s.refIndex.projects[item.ProjectRef]
	if projectID == "" {
		return Change{}, fmt.Errorf("missing project ref for %s", item.ExternalRef.ID)
	}
	if current, found := findAPIKeyByBusinessKey(existingKeys, projectID, item.Spec.Name); found {
		s.refIndex.apiKeys[item.ExternalRef.ID] = current.ID
		action := chooseActionAPIKey(current, item.Spec)
		if action == ActionSkip {
			return Change{Resource: "api_key", ID: current.ID, Action: ActionSkip}, nil
		}
		_, err := s.client.UpdateAPIKey(ctx, current.ID, item.Spec)
		return Change{Resource: "api_key", ID: current.ID, Action: ActionUpdate}, err
	}
	payload := map[string]any{
		"name":           item.Spec.Name,
		"group":          item.Spec.Group,
		"allowed_models": item.Spec.Allowed,
		"ip_allowlist":   item.Spec.IPAllowlist,
		"limits":         item.Spec.Limits,
		"expires_at":     item.Spec.ExpiresAt,
	}
	created, err := s.client.CreateProjectKey(ctx, projectID, payload)
	if err != nil {
		return Change{}, err
	}
	s.refIndex.apiKeys[item.ExternalRef.ID] = created.ID
	if created.APIKey != "" {
		s.newKeys[item.ExternalRef.ID] = created.APIKey
	}
	return Change{Resource: "api_key", ID: created.ID, Action: ActionCreate}, nil
}

func (s *HTTPSink) applyRouteHTTP(ctx context.Context, existing []server.ModelRoute, item bundle.RouteRef) (Change, server.ModelRoute, error) {
	spec := item.Spec
	if spec.ModelName == "" {
		spec.ModelName = s.refIndex.models[item.ModelRef]
	}
	if item.ProviderResourceRef != "" {
		spec.ProviderResourceID = s.refIndex.resources[item.ProviderResourceRef]
	}
	if spec.ProviderID == "" {
		spec.ProviderID = s.refIndex.providers[item.ProviderRef]
	}
	if current, found := findRouteByBusinessKey(existing, spec.ModelName, spec.ProviderResourceID, spec.ProviderModel); found {
		s.refIndex.routes[item.ExternalRef.ID] = current.ID
		action := chooseActionRoute(current, spec, spec.ProviderResourceID, spec.ModelName)
		if action == ActionSkip {
			return Change{Resource: "route", ID: current.ID, Action: ActionSkip}, server.ModelRoute{}, nil
		}
		updated, err := s.client.UpdateRoute(ctx, current.ID, spec)
		return Change{Resource: "route", ID: current.ID, Action: ActionUpdate}, updated, err
	}
	created, err := s.client.CreateRoute(ctx, spec)
	if err != nil {
		return Change{}, server.ModelRoute{}, err
	}
	s.refIndex.routes[item.ExternalRef.ID] = created.ID
	return Change{Resource: "route", ID: created.ID, Action: ActionCreate}, created, nil
}
