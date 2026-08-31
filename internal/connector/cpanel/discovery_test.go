package cpanel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
	resDomain "backup-platform/internal/resource/domain"
)

func TestCPanelDatabaseDiscoverer_APITokenAuth_Success(t *testing.T) {
	username := "mycpaneluser"
	apiToken := "secret-cpanel-api-token"
	authHeaderReceived := ""

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute/Mysql/list_databases" {
			http.NotFound(w, r)
			return
		}
		authHeaderReceived = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"apiversion": 3,
			"result": {
				"status": 1,
				"data": [
					{
						"database": "mycpaneluser_shop",
						"disk_usage": 4161,
						"users": ["mycpaneluser_dbuser"]
					},
					{
						"database": "mycpaneluser_blog",
						"disk_usage": 1048576,
						"users": []
					}
				]
			}
		}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true
	timeout := 5

	target := connector.Target{
		Host:              u.Hostname(),
		Port:              port,
		Username:          username,
		AuthType:          resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS:          &useHTTPS,
		ConnectionTimeout: &timeout,
	}
	credPayload := &payload.PayloadV1{Secret: apiToken}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected discovery error: %v", err)
	}

	expectedAuth := fmt.Sprintf("cpanel %s:%s", username, apiToken)
	if authHeaderReceived != expectedAuth {
		t.Errorf("expected Authorization header %q, got %q", expectedAuth, authHeaderReceived)
	}

	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}

	if dbs[0].Name != "mycpaneluser_shop" || dbs[0].SizeBytes != 4161 || dbs[0].TablesCount != nil || dbs[0].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected database 0 metadata: %+v", dbs[0])
	}
	if dbs[1].Name != "mycpaneluser_blog" || dbs[1].SizeBytes != 1048576 || dbs[1].TablesCount != nil || dbs[1].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected database 1 metadata: %+v", dbs[1])
	}
}

func TestCPanelDatabaseDiscoverer_PasswordAuth_Success(t *testing.T) {
	username := "mycpaneluser"
	password := "mypassword123"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1,"data":[{"database":"user_app","disk_usage":500}]}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: username,
		AuthType: resDomain.AuthTypeCPanelPassword,
		UseHTTPS: &useHTTPS,
	}
	credPayload := &payload.PayloadV1{Secret: password}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
	if err != nil {
		t.Fatalf("unexpected discovery error: %v", err)
	}

	if len(dbs) != 1 || dbs[0].Name != "user_app" || dbs[0].SizeBytes != 500 || dbs[0].TablesCount != nil {
		t.Errorf("unexpected discovered database: %+v", dbs)
	}
}

func TestCPanelDatabaseDiscoverer_ErrorConditions(t *testing.T) {
	username := "mycpaneluser"
	token := "token"
	useHTTPS := true

	t.Run("Status 0 returns error without secret leak", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":0,"errors":["Access denied for token=SUPERSECRET_TOKEN"]}}`))
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		target := connector.Target{
			Host:     u.Hostname(),
			Port:     port,
			Username: username,
			AuthType: resDomain.AuthTypeCPanelAPIToken,
			UseHTTPS: &useHTTPS,
		}
		credPayload := &payload.PayloadV1{Secret: token}

		discoverer := NewCPanelDatabaseDiscoverer(server.Client())
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected error on status 0")
		}
		if strings.Contains(err.Error(), "SUPERSECRET_TOKEN") {
			t.Errorf("SECURITY FLAW: secret leaked in error message: %v", err)
		}
	})

	t.Run("Invalid APIVersion returns error", func(t *testing.T) {
		invalidVersions := []string{
			`{"apiversion":0,"result":{"status":1,"data":[]}}`,
			`{"apiversion":-1,"result":{"status":1,"data":[]}}`,
			`{"apiversion":"3","result":{"status":1,"data":[]}}`,
		}
		for _, respStr := range invalidVersions {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(respStr))
			}))

			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			target := connector.Target{
				Host:     u.Hostname(),
				Port:     port,
				Username: username,
				AuthType: resDomain.AuthTypeCPanelAPIToken,
				UseHTTPS: &useHTTPS,
			}
			credPayload := &payload.PayloadV1{Secret: token}

			discoverer := NewCPanelDatabaseDiscoverer(server.Client())
			_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
			server.Close()

			if err == nil {
				t.Errorf("expected error for invalid apiversion in response: %s", respStr)
			}
		}
	})

	t.Run("Negative disk_usage returns error", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"apiversion":3,"result":{"status":1,"data":[{"database":"bad_db","disk_usage":-50}]}}`))
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		target := connector.Target{
			Host:     u.Hostname(),
			Port:     port,
			Username: username,
			AuthType: resDomain.AuthTypeCPanelAPIToken,
			UseHTTPS: &useHTTPS,
		}
		credPayload := &payload.PayloadV1{Secret: token}

		discoverer := NewCPanelDatabaseDiscoverer(server.Client())
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected error for negative disk_usage")
		}
	})

	t.Run("Malformed JSON returns error", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{not valid json`))
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		target := connector.Target{
			Host:     u.Hostname(),
			Port:     port,
			Username: username,
			AuthType: resDomain.AuthTypeCPanelAPIToken,
			UseHTTPS: &useHTTPS,
		}
		credPayload := &payload.PayloadV1{Secret: token}

		discoverer := NewCPanelDatabaseDiscoverer(server.Client())
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected error for malformed json")
		}
	})

	t.Run("Strict TLS verification rejects untrusted cert with default client", func(t *testing.T) {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		target := connector.Target{
			Host:     u.Hostname(),
			Port:     port,
			Username: username,
			AuthType: resDomain.AuthTypeCPanelAPIToken,
			UseHTTPS: &useHTTPS,
		}
		credPayload := &payload.PayloadV1{Secret: token}

		// Production client without test CA cert
		discoverer := NewCPanelDatabaseDiscoverer(nil)
		_, err := discoverer.DiscoverDatabases(context.Background(), target, credPayload)
		if err == nil {
			t.Fatalf("expected TLS verification failure on untrusted server")
		}
	})
}
