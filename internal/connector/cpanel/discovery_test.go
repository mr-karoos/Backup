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

// 9. Discovery wrapped success
func TestCPanelDatabaseDiscoverer_WrappedSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"apiversion": 3,
			"result": {
				"status": 1,
				"data": [
					{"database": "wrapped_prod", "disk_usage": 2048}
				]
			}
		}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected discovery error: %v", err)
	}
	if len(dbs) != 1 {
		t.Fatalf("expected 1 database, got %d", len(dbs))
	}
	if dbs[0].Name != "wrapped_prod" || dbs[0].SizeBytes != 2048 || dbs[0].TablesCount != nil || dbs[0].Status != connector.DatabaseStatusAccessible {
		t.Errorf("unexpected database metadata: %+v", dbs[0])
	}
}

// 10. Discovery flat success
func TestCPanelDatabaseDiscoverer_FlatSuccess(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"status": 1,
			"data": [
				{"database": "flat_db_1", "disk_usage": 5120},
				{"database": "flat_db_2", "disk_usage": 10240}
			],
			"metadata": {}
		}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected discovery error: %v", err)
	}
	if len(dbs) != 2 {
		t.Fatalf("expected 2 databases, got %d", len(dbs))
	}
	if dbs[0].Name != "flat_db_1" || dbs[0].SizeBytes != 5120 || dbs[0].TablesCount != nil {
		t.Errorf("unexpected database 0 metadata: %+v", dbs[0])
	}
	if dbs[1].Name != "flat_db_2" || dbs[1].SizeBytes != 10240 || dbs[1].TablesCount != nil {
		t.Errorf("unexpected database 1 metadata: %+v", dbs[1])
	}
}

// 11. Discovery flat empty data [] -> success with zero databases
func TestCPanelDatabaseDiscoverer_FlatEmptyData_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": 1, "data": [], "metadata": {}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error for flat empty data: %v", err)
	}
	if len(dbs) != 0 {
		t.Fatalf("expected 0 databases, got %d", len(dbs))
	}
}

// 12. Discovery wrapped empty data -> success with zero databases
func TestCPanelDatabaseDiscoverer_WrappedEmptyData_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion": 3, "result": {"status": 1, "data": []}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error for wrapped empty data: %v", err)
	}
	if len(dbs) != 0 {
		t.Fatalf("expected 0 databases, got %d", len(dbs))
	}
}

// 13 & 17. Flat status=0 failure without leaking sensitive error text
func TestCPanelDatabaseDiscoverer_FlatStatusZero_Failure(t *testing.T) {
	secretToHide := "REFLECTED-MYSQL-PASSWORD-8899"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": 0, "errors": ["Access denied with password ` + secretToHide + `"]}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err == nil {
		t.Fatalf("expected error when flat status is 0")
	}
	if strings.Contains(err.Error(), secretToHide) {
		t.Fatalf("CRITICAL: sensitive error details leaked in discoverer error: %v", err)
	}
}

// 14. Wrapped status=0 failure
func TestCPanelDatabaseDiscoverer_WrappedStatusZero_Failure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion": 3, "result": {"status": 0, "errors": ["UAPI query failed"]}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err == nil {
		t.Fatalf("expected error when wrapped status is 0")
	}
}

// 15. Negative disk_usage in flat schema returns error
func TestCPanelDatabaseDiscoverer_FlatNegativeDiskUsage_Failure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": 1, "data": [{"database": "bad_db", "disk_usage": -100}]}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err == nil {
		t.Fatalf("expected error for negative disk_usage in flat schema")
	}
}

// 1. Flat status=1 + missing data => failure
func TestCPanelDatabaseDiscoverer_FlatMissingData_Failure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": 1, "metadata": {}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err == nil {
		t.Fatalf("expected error when flat response has missing data field")
	}
}

// 2. Wrapped status=1 + missing result.data => failure
func TestCPanelDatabaseDiscoverer_WrappedMissingData_Failure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion": 3, "result": {"status": 1}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err == nil {
		t.Fatalf("expected error when wrapped response has missing result.data field")
	}
}

// 5. Flat data=null => success zero databases
func TestCPanelDatabaseDiscoverer_FlatDataNull_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": 1, "data": null, "metadata": {}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error for flat data=null: %v", err)
	}
	if len(dbs) != 0 {
		t.Fatalf("expected 0 databases for flat data=null, got %d", len(dbs))
	}
}

// 6. Wrapped data=null => success zero databases
func TestCPanelDatabaseDiscoverer_WrappedDataNull_Success(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"apiversion": 3, "result": {"status": 1, "data": null}}`))
	}))
	defer server.Close()

	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	useHTTPS := true

	target := connector.Target{
		Host:     u.Hostname(),
		Port:     port,
		Username: "myuser",
		AuthType: resDomain.AuthTypeCPanelAPIToken,
		UseHTTPS: &useHTTPS,
	}

	discoverer := NewCPanelDatabaseDiscoverer(server.Client())
	dbs, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
	if err != nil {
		t.Fatalf("unexpected error for wrapped data=null: %v", err)
	}
	if len(dbs) != 0 {
		t.Fatalf("expected 0 databases for wrapped data=null, got %d", len(dbs))
	}
}

// 7. Flat data={} or string => failure
func TestCPanelDatabaseDiscoverer_FlatDataNonArray_Failure(t *testing.T) {
	for _, rawData := range []string{
		`{"status": 1, "data": {}}`,
		`{"status": 1, "data": "not-an-array"}`,
		`{"status": 1, "data": 12345}`,
	} {
		t.Run(rawData, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(rawData))
			}))
			defer server.Close()

			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			useHTTPS := true

			target := connector.Target{
				Host:     u.Hostname(),
				Port:     port,
				Username: "myuser",
				AuthType: resDomain.AuthTypeCPanelAPIToken,
				UseHTTPS: &useHTTPS,
			}

			discoverer := NewCPanelDatabaseDiscoverer(server.Client())
			_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
			if err == nil {
				t.Fatalf("expected error for non-array flat data: %s", rawData)
			}
		})
	}
}

// 8. Wrapped data={} or string => failure
func TestCPanelDatabaseDiscoverer_WrappedDataNonArray_Failure(t *testing.T) {
	for _, rawData := range []string{
		`{"apiversion": 3, "result": {"status": 1, "data": {}}}`,
		`{"apiversion": 3, "result": {"status": 1, "data": "not-an-array"}}`,
		`{"apiversion": 3, "result": {"status": 1, "data": 12345}}`,
	} {
		t.Run(rawData, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(rawData))
			}))
			defer server.Close()

			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			useHTTPS := true

			target := connector.Target{
				Host:     u.Hostname(),
				Port:     port,
				Username: "myuser",
				AuthType: resDomain.AuthTypeCPanelAPIToken,
				UseHTTPS: &useHTTPS,
			}

			discoverer := NewCPanelDatabaseDiscoverer(server.Client())
			_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
			if err == nil {
				t.Fatalf("expected error for non-array wrapped data: %s", rawData)
			}
		})
	}
}

// 9. Wrapped result missing, null, non-object or without status => failure (never fall back to Flat)
func TestCPanelDatabaseDiscoverer_WrappedMissingOrMalformedResult_Failure(t *testing.T) {
	responses := []string{
		`{"apiversion": 3}`,
		`{"apiversion": 3, "result": null}`,
		`{"apiversion": 3, "result": null, "status": 1, "data": []}`,
		`{"apiversion": 3, "result": "not-an-object"}`,
		`{"apiversion": 3, "result": [1, 2, 3]}`,
		`{"apiversion": 3, "result": {}}`,
		`{"apiversion": 3, "result": {"data": []}}`,
	}

	for _, respBody := range responses {
		t.Run(respBody, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(respBody))
			}))
			defer server.Close()

			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			useHTTPS := true

			target := connector.Target{
				Host:     u.Hostname(),
				Port:     port,
				Username: "myuser",
				AuthType: resDomain.AuthTypeCPanelAPIToken,
				UseHTTPS: &useHTTPS,
			}

			discoverer := NewCPanelDatabaseDiscoverer(server.Client())
			_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
			if err == nil {
				t.Fatalf("expected error for malformed wrapped result: %s", respBody)
			}
		})
	}
}

// 10. Discovery remote errors/messages do not leak into returned error
func TestCPanelDatabaseDiscoverer_RemoteErrorsMessagesDoNotLeak(t *testing.T) {
	cases := []struct {
		name     string
		response string
	}{
		{
			name: "flat_schema_status_0_with_errors_and_messages",
			response: `{
				"status": 0,
				"errors": ["SUPER-SENSITIVE-REMOTE-TEXT-FLAT"],
				"messages": ["SECRET-MESSAGE-FLAT"]
			}`,
		},
		{
			name: "wrapped_schema_status_0_with_errors_and_messages",
			response: `{
				"apiversion": 3,
				"result": {
					"status": 0,
					"errors": ["SUPER-SENSITIVE-REMOTE-TEXT-WRAPPED"],
					"messages": ["SECRET-MESSAGE-WRAPPED"]
				}
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()

			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			useHTTPS := true

			target := connector.Target{
				Host:     u.Hostname(),
				Port:     port,
				Username: "myuser",
				AuthType: resDomain.AuthTypeCPanelAPIToken,
				UseHTTPS: &useHTTPS,
			}

			discoverer := NewCPanelDatabaseDiscoverer(server.Client())
			_, err := discoverer.DiscoverDatabases(context.Background(), target, &payload.PayloadV1{Secret: "token"})
			if err == nil {
				t.Fatalf("expected error for status 0 response")
			}

			errStr := err.Error()
			if strings.Contains(errStr, "SUPER-SENSITIVE-REMOTE-TEXT") {
				t.Fatalf("sensitive remote error text leaked in error string: %s", errStr)
			}
			if strings.Contains(errStr, "SECRET-MESSAGE") {
				t.Fatalf("sensitive remote message leaked in error string: %s", errStr)
			}
		})
	}
}
