package cpanel

import (
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

type cpanelListDatabasesResponse struct {
	APIVersion int `json:"apiversion"`
	Result     struct {
		Status int `json:"status"`
		Data   []struct {
			Database  string `json:"database"`
			DiskUsage int64  `json:"disk_usage"`
		} `json:"data"`
	} `json:"result"`
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

	var uapiResp cpanelListDatabasesResponse
	if err := json.Unmarshal(body, &uapiResp); err != nil {
		return nil, errors.New("failed to parse cpanel mysql database list response")
	}
	clear(body)

	// Validate typed API version
	if uapiResp.APIVersion <= 0 {
		return nil, errors.New("invalid cpanel api version")
	}

	// Validate UAPI result status
	if uapiResp.Result.Status != 1 {
		return nil, errors.New("cpanel uapi returned non-success status")
	}

	result := make([]connector.DatabaseInfo, 0, len(uapiResp.Result.Data))
	for _, item := range uapiResp.Result.Data {
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
