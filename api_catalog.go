package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type apiRouteAction struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	Description string `json:"description"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	ObjectID    bool   `json:"objectID"`
	Unavailable string `json:"unavailable,omitempty"`
}

// This checked-in allowlist is deliberate: new routes are never automatically
// exposed to an existing credential, and no client supplies a target URL.
//
//go:embed api_action_catalog.json
var apiCatalogJSON []byte
var apiRouteActions = func() map[string]apiRouteAction {
	var items []apiRouteAction
	if err := json.Unmarshal(apiCatalogJSON, &items); err != nil {
		panic(err)
	}
	result := map[string]apiRouteAction{}
	for _, item := range items {
		if item.Name == "" || !strings.HasPrefix(item.Path, "/api/") || strings.HasPrefix(item.Path, "/api/v1") || result[item.Name].Name != "" {
			panic("invalid external API catalog")
		}
		result[item.Name] = item
	}
	return result
}()

func init() {
	var items []apiRouteAction
	_ = json.Unmarshal(apiCatalogJSON, &items)
	for _, item := range items {
		apiActions = append(apiActions, apiAction{item.Name, item.Group, item.Description, item.Unavailable})
	}
}

type externalAPIGrantKey struct{}
type externalAPIGrant struct{ Path, Method string }

func validExternalAPIGrant(r *http.Request) bool {
	grant, ok := r.Context().Value(externalAPIGrantKey{}).(externalAPIGrant)
	return ok && grant.Path == r.URL.Path && grant.Method == r.Method
}

func apiRoutePath(route apiRouteAction, objectID string) (string, error) {
	if !route.ObjectID {
		if objectID != "" {
			return "", fmt.Errorf("This action does not accept objectId")
		}
		return route.Path, nil
	}
	if objectID == "" || len(objectID) > 200 || objectID == "." || objectID == ".." || strings.ContainsAny(objectID, "/\\?#%\x00\r\n") {
		return "", fmt.Errorf("Provide one valid objectId for this action")
	}
	return route.Path + objectID, nil
}

func (a *App) dispatchAPIRoute(w http.ResponseWriter, r *http.Request, action, objectID string, query url.Values, body []byte, raw bool) {
	route, ok := apiRouteActions[action]
	if !ok {
		writeError(w, 400, "Unknown panel API action")
		return
	}
	if route.Unavailable != "" {
		writeError(w, 409, route.Unavailable)
		return
	}
	path, err := apiRoutePath(route, objectID)
	if err != nil {
		writeError(w, 400, err.Error())
		return
	}
	for key := range query {
		switch strings.ToLower(key) {
		case "secret", "identifier", "password", "token", "x-litecp-secret", "x-litecp-identifier":
			writeError(w, 400, "Credentials and secrets must not be placed in query parameters")
			return
		}
	}
	request := r.Clone(context.WithValue(r.Context(), externalAPIGrantKey{}, externalAPIGrant{path, route.Method}))
	request.Method = route.Method
	request.URL = &url.URL{Path: path, RawQuery: query.Encode()}
	request.RequestURI = request.URL.RequestURI()
	request.Header = make(http.Header)
	if raw {
		// Preserve only body/stream metadata; never forward the API secret, cookies,
		// origin, proxy headers or authorization to a management handler.
		for _, key := range []string{"Content-Type", "Content-Length", "Range", "If-Range"} {
			if value := r.Header.Get(key); value != "" {
				request.Header.Set(key, value)
			}
		}
	} else {
		request.Header.Set("Content-Type", "application/json")
		request.Body = ioNopAPI(body)
		request.ContentLength = int64(len(body))
	}
	handler, pattern := a.routeMux().Handler(request)
	if _, _, err := brokerOperation(a.routeMux(), path); err != nil {
		writeError(w, 403, err.Error())
		return
	}
	if pattern != route.Path {
		writeError(w, 500, "API action does not match its registered management route")
		return
	}
	handler.ServeHTTP(w, request)
}
