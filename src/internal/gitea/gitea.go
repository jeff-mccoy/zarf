// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: 2021-Present The Zarf Authors

// Package gitea contains Gitea client specific functionality.
package gitea

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const artifactTokenName = "zarf-artifact-registry-token"

// Client is a client that communicates with the Gitea API.
type Client struct {
	httpClient *http.Client
	endpoint   *url.URL
	username   string
	password   string
}

// NewClient creates and returns a new Gitea client.
func NewClient(endpoint, username, password string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("could not get default transport")
	}
	transport = transport.Clone()
	transport.MaxIdleConnsPerHost = transport.MaxIdleConns
	httpClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
	}
	client := &Client{
		httpClient: httpClient,
		endpoint:   u,
		username:   username,
		password:   password,
	}
	return client, nil
}

// DoRequest performs a request to the Gitea API at the given path.
func (g *Client) DoRequest(ctx context.Context, method string, path string, body []byte) (_ []byte, _ int, err error) {
	u, err := g.endpoint.Parse(path)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewBuffer(body))
	if err != nil {
		return nil, 0, err
	}
	req.SetBasicAuth(g.username, g.password)
	req.Header.Add("accept", "application/json")
	req.Header.Add("content-type", "application/json")
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		errClose := resp.Body.Close()
		err = errors.Join(err, errClose)
	}()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return b, resp.StatusCode, nil
}

// CreateReadOnlyUser creates a non-admin Zarf user.
func (g *Client) CreateReadOnlyUser(ctx context.Context, username, password string) error {
	// Create the read only user
	createUserData := map[string]interface{}{
		"username":             username,
		"password":             password,
		"email":                "zarf-reader@localhost.local",
		"must_change_password": false,
	}
	body, err := json.Marshal(createUserData)
	if err != nil {
		return err
	}
	_, statusCode, err := g.DoRequest(ctx, http.MethodPost, "/api/v1/admin/users", body)
	if statusCode == 422 {
		return nil
	}
	if err != nil {
		return err
	}

	// Make sure the user can't create their own repos or orgs
	updateUserData := map[string]interface{}{
		"login_name":                username,
		"max_repo_creation":         0,
		"allow_create_organization": false,
	}
	body, err = json.Marshal(updateUserData)
	if err != nil {
		return err
	}
	_, _, err = g.DoRequest(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/admin/users/%s", username), body)
	if err != nil {
		return err
	}
	return nil
}

// UpdateGitUser updates Zarf git server users.
func (g *Client) UpdateGitUser(ctx context.Context, username string, password string) error {
	updateUserData := map[string]interface{}{
		"login_name": username,
		"password":   password,
	}
	body, err := json.Marshal(updateUserData)
	if err != nil {
		return err
	}
	_, _, err = g.DoRequest(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/admin/users/%s", username), body)
	if err != nil {
		return err
	}
	return nil
}

// CreatePackageRegistryToken creates or replaces an existing package registry token.
func (g *Client) CreatePackageRegistryToken(ctx context.Context) (string, error) {
	// Determine if the package token already exists.
	b, _, err := g.DoRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%s/tokens", g.username), nil)
	if err != nil {
		return "", err
	}
	var tokens []map[string]interface{}
	err = json.Unmarshal(b, &tokens)
	if err != nil {
		return "", err
	}
	hasPackageToken := false
	for _, token := range tokens {
		if token["name"] != artifactTokenName {
			continue
		}
		hasPackageToken = true
		break
	}

	// Delete the token if it already exists.
	if hasPackageToken {
		_, _, err := g.DoRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/users/%s/tokens/%s", g.username, artifactTokenName), nil)
		if err != nil {
			return "", err
		}
	}

	// Create the new token.
	createTokensData := map[string]interface{}{
		"name":   artifactTokenName,
		"scopes": []string{"read:user", "read:package", "write:package"},
	}
	body, err := json.Marshal(createTokensData)
	if err != nil {
		return "", err
	}
	b, _, err = g.DoRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%s/tokens", g.username), body)
	if err != nil {
		return "", err
	}
	createTokenResponse := struct {
		Sha1 string `json:"sha1"`
	}{}
	err = json.Unmarshal(b, &createTokenResponse)
	if err != nil {
		return "", err
	}
	return createTokenResponse.Sha1, nil
}

// AddReadOnlyUserToRepository adds a read only user to a repository.
func (g *Client) AddReadOnlyUserToRepository(ctx context.Context, repo, username string) error {
	addCollabData := map[string]string{
		"permission": "read",
	}
	body, err := json.Marshal(addCollabData)
	if err != nil {
		return err
	}
	_, _, err = g.DoRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/repos/%s/%s/collaborators/%s", g.username, repo, username), body)
	if err != nil {
		return err
	}
	return nil
}
