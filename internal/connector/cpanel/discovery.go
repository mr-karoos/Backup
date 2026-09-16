package cpanel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"backup-platform/internal/connector"
	"backup-platform/internal/credential/payload"
)

// CPanelDatabaseDiscoverer implements MySQL database discovery over cPanel UAPI.
type CPanelDatabaseDiscoverer struct {
	client HTTPDoer
}

// NewCPanelDatabaseDiscoverer constructs a cPanel DatabaseDiscoverer.
func NewCPanelDatabaseDiscoverer(client HTTPDoer) *CPanelDatabaseDiscoverer {
	if client == nil {
		client = &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &CPanelDatabaseDiscoverer{
		client: client,
	}
}

type cpanelDatabaseItem struct {
	Database  string `json:"database"`
	DiskUsage int64  `json:"disk_usage"`
}

// DiscoverDatabases queries cPanel UAPI Mysql/list_databases over HTTPS and returns normalized DatabaseInfo.
func (d *CPanelDatabaseDiscoverer) DiscoverDatabases(
	ctx context.Context,
	target connector.Target,
	credPayload *payload.PayloadV1,
) ([]connector.DatabaseInfo, error) {
	body, _, _, _, probeRes, err := executeUAPIRequest(
		ctx,
		d.client,
		target,
		credPayload,
		"/execute/Mysql/list_databases",
	)
	if err != nil {
		return nil, err
	}
	if probeRes != nil {
		return nil, errors.New("cpanel api request failed")
	}
	defer clear(body)

	norm, err := parseNormalizedUAPIResponse(body)
	if err != nil {
		return nil, errors.New("failed to parse cpanel mysql database list response")
	}

	// Validate UAPI result status
	if norm.Status != 1 {
		return nil, errors.New("cpanel uapi returned non-success status")
	}

	// Data field must be explicitly present in response
	if !norm.DataPresent {
		return nil, errors.New("missing cpanel mysql database list data")
	}

	// data: null => success with zero databases
	trimmedData := bytes.TrimSpace(norm.Data)
	if bytes.Equal(trimmedData, []byte("null")) {
		clear(body)
		return []connector.DatabaseInfo{}, nil
	}

	// data must be a JSON array; non-array (e.g. object, string, number) must fail
	if len(trimmedData) == 0 || trimmedData[0] != '[' {
		clear(body)
		return nil, errors.New("invalid cpanel mysql database list data format")
	}

	var items []cpanelDatabaseItem
	if err := json.Unmarshal(trimmedData, &items); err != nil {
		clear(body)
		return nil, errors.New("failed to parse cpanel mysql database list items")
	}
	clear(body)

	result := make([]connector.DatabaseInfo, 0, len(items))
	for _, item := range items {
		if item.DiskUsage < 0 {
			return nil, errors.New("cpanel database disk_usage is negative")
		}

		result = append(result, connector.DatabaseInfo{
			Name:        item.Database,
			SizeBytes:   item.DiskUsage,
			TablesCount: nil, // Strictly nil for cPanel
			Status:      connector.DatabaseStatusAccessible,
		})
	}

	return result, nil
}
