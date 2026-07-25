package tokenhub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"tokenhub/backend/internal/server"
)

type AdminAPIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewAdminAPIClient(baseURL string, token string, httpClient *http.Client) *AdminAPIClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &AdminAPIClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		token:      token,
		httpClient: httpClient,
	}
}

func (c *AdminAPIClient) endpoint(parts ...string) string {
	base, _ := url.Parse(c.baseURL)
	encoded := make([]string, 0, len(parts)+8)
	for _, segment := range strings.Split(strings.Trim(base.Path, "/"), "/") {
		if strings.TrimSpace(segment) != "" {
			encoded = append(encoded, segment)
		}
	}
	for _, part := range parts {
		for _, segment := range strings.Split(strings.TrimSpace(strings.Trim(part, "/")), "/") {
			if segment == "" {
				continue
			}
			encoded = append(encoded, url.PathEscape(segment))
		}
	}
	base.Path = path.Join(encoded...)
	return base.String()
}

func (c *AdminAPIClient) doJSON(ctx context.Context, method string, endpoint string, reqBody any, out any) error {
	var body io.Reader
	if reqBody != nil {
		payload, err := json.Marshal(reqBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.token) != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= 400 {
		return fmt.Errorf("admin api %s %s failed: status=%d body=%s", method, endpoint, response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if out == nil || len(responseBody) == 0 {
		return nil
	}
	return json.Unmarshal(responseBody, out)
}

type listResponse[T any] struct {
	Data []T `json:"data"`
}

type providerCreateResult struct {
	Provider server.Provider `json:"provider"`
}

type apiKeyCreateResult struct {
	ID                   string `json:"id"`
	APIKey               string `json:"api_key"`
	Name                 string `json:"name"`
	ProjectID            string `json:"project_id"`
	PlainTextVisibleOnce bool   `json:"plain_text_visible_once"`
}

func (c *AdminAPIClient) ListProviders(ctx context.Context) ([]server.Provider, error) {
	var resp listResponse[server.Provider]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/providers"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateProvider(ctx context.Context, req server.Provider) (server.Provider, error) {
	var resp providerCreateResult
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/providers"), req, &resp); err != nil {
		return server.Provider{}, err
	}
	return resp.Provider, nil
}

func (c *AdminAPIClient) UpdateProvider(ctx context.Context, id string, req server.Provider) (server.Provider, error) {
	var resp providerCreateResult
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/providers", id), req, &resp); err != nil {
		return server.Provider{}, err
	}
	if resp.Provider.ID != "" {
		return resp.Provider, nil
	}
	return req, nil
}

func (c *AdminAPIClient) DeleteProvider(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/providers", id), nil, nil)
}

func (c *AdminAPIClient) ListProviderResources(ctx context.Context) ([]server.ProviderResource, error) {
	var resp listResponse[server.ProviderResource]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/provider-resources"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateProviderResource(ctx context.Context, req server.ProviderResource) (server.ProviderResource, error) {
	var resp server.ProviderResource
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/provider-resources"), req, &resp); err != nil {
		return server.ProviderResource{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) UpdateProviderResource(ctx context.Context, id string, req server.ProviderResource) (server.ProviderResource, error) {
	var resp server.ProviderResource
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/provider-resources", id), req, &resp); err != nil {
		return server.ProviderResource{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) DeleteProviderResource(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/provider-resources", id), nil, nil)
}

func (c *AdminAPIClient) ListModels(ctx context.Context) ([]server.Model, error) {
	var resp listResponse[server.Model]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/models"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateModel(ctx context.Context, req server.Model) (server.Model, error) {
	var resp server.Model
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/models"), req, &resp); err != nil {
		return server.Model{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) UpdateModel(ctx context.Context, name string, req server.Model) (server.Model, error) {
	var resp server.Model
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/models", name), req, &resp); err != nil {
		return server.Model{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) DeleteModel(ctx context.Context, name string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/models", name), nil, nil)
}

func (c *AdminAPIClient) ListRoutes(ctx context.Context) ([]server.ModelRoute, error) {
	var resp listResponse[server.ModelRoute]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/routing-rules"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateRoute(ctx context.Context, req server.ModelRoute) (server.ModelRoute, error) {
	var resp server.ModelRoute
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/routing-rules"), req, &resp); err != nil {
		return server.ModelRoute{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) UpdateRoute(ctx context.Context, id string, req server.ModelRoute) (server.ModelRoute, error) {
	var resp server.ModelRoute
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/routing-rules", id), req, &resp); err != nil {
		return server.ModelRoute{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) DeleteRoute(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/routing-rules", id), nil, nil)
}

func (c *AdminAPIClient) ListUsers(ctx context.Context) ([]server.AdminUser, error) {
	var resp listResponse[server.AdminUser]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/users"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) ImportUsersCSV(ctx context.Context, content string) error {
	payload := map[string]any{
		"source":  "migration",
		"format":  "csv",
		"content": content,
	}
	return c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/users/import"), payload, &map[string]any{})
}

func (c *AdminAPIClient) ListProjects(ctx context.Context) ([]server.Project, error) {
	var resp listResponse[server.Project]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/projects"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateProject(ctx context.Context, req server.Project) (server.Project, error) {
	var resp server.Project
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/projects"), req, &resp); err != nil {
		return server.Project{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) UpdateProject(ctx context.Context, id string, req server.Project) (server.Project, error) {
	var resp server.Project
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/projects", id), req, &resp); err != nil {
		return server.Project{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) DeleteProject(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/projects", id), nil, nil)
}

func (c *AdminAPIClient) ListAPIKeys(ctx context.Context) ([]server.APIKey, error) {
	var resp listResponse[server.APIKey]
	if err := c.doJSON(ctx, http.MethodGet, c.endpoint("/api/admin/api-keys"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *AdminAPIClient) CreateProjectKey(ctx context.Context, projectID string, req map[string]any) (apiKeyCreateResult, error) {
	var resp apiKeyCreateResult
	if err := c.doJSON(ctx, http.MethodPost, c.endpoint("/api/admin/projects", projectID, "keys"), req, &resp); err != nil {
		return apiKeyCreateResult{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) UpdateAPIKey(ctx context.Context, id string, req server.APIKey) (server.APIKey, error) {
	var resp server.APIKey
	if err := c.doJSON(ctx, http.MethodPatch, c.endpoint("/api/admin/api-keys", id), req, &resp); err != nil {
		return server.APIKey{}, err
	}
	return resp, nil
}

func (c *AdminAPIClient) DeleteAPIKey(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, c.endpoint("/api/admin/api-keys", id), nil, nil)
}
