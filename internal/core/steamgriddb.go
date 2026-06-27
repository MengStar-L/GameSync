package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const steamGridDBBaseURL = "https://www.steamgriddb.com/api/v2"

type SteamGridDBClient struct {
	apiKey     string
	httpClient *http.Client
}

type SteamGridDBSearchResult struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Types        []string `json:"types,omitempty"`
	Verified     bool     `json:"verified"`
	CoverPath    string   `json:"coverPath"`
	CoverOptions []string `json:"coverOptions,omitempty"`
}

type steamGridDBGame struct {
	ID       int      `json:"id"`
	Name     string   `json:"name"`
	Types    []string `json:"types"`
	Verified bool     `json:"verified"`
}

type steamGridDBSearchResponse struct {
	Success bool              `json:"success"`
	Data    []steamGridDBGame `json:"data"`
}

type steamGridDBGrid struct {
	URL   string `json:"url"`
	Thumb string `json:"thumb"`
}

type steamGridDBGridResponse struct {
	Success bool              `json:"success"`
	Data    []steamGridDBGrid `json:"data"`
}

func NewSteamGridDBClient(apiKey string) (*SteamGridDBClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New(msgSteamGridDBAPIKeyRequired)
	}

	return &SteamGridDBClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (c *SteamGridDBClient) SearchGames(ctx context.Context, query string) ([]SteamGridDBSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New(msgSteamGridDBQueryRequired)
	}

	var payload steamGridDBSearchResponse
	if err := c.getJSON(ctx, "/search/autocomplete/"+url.PathEscape(query), nil, &payload); err != nil {
		return nil, err
	}

	limit := len(payload.Data)
	if limit > 8 {
		limit = 8
	}
	results := make([]SteamGridDBSearchResult, 0, limit)
	for _, item := range payload.Data[:limit] {
		covers, err := c.GetCoverOptions(ctx, item.ID)
		if err != nil {
			return nil, err
		}
		results = append(results, SteamGridDBSearchResult{
			ID:           item.ID,
			Name:         strings.TrimSpace(item.Name),
			Types:        uniqueNonEmptyStrings(item.Types),
			Verified:     item.Verified,
			CoverPath:    firstString(covers),
			CoverOptions: covers,
		})
	}

	return results, nil
}

func (c *SteamGridDBClient) GetCoverOptions(ctx context.Context, gameID int) ([]string, error) {
	if gameID <= 0 {
		return nil, errors.New(msgInvalidSteamGridDBGameID)
	}

	params := url.Values{}
	params.Set("dimensions", "600x900,342x482,660x930")
	params.Set("types", "static")
	params.Set("nsfw", "false")
	params.Set("humor", "false")
	params.Set("epilepsy", "false")
	params.Set("limit", "6")

	var payload steamGridDBGridResponse
	if err := c.getJSON(ctx, fmt.Sprintf("/grids/game/%d", gameID), params, &payload); err != nil {
		return nil, err
	}

	options := make([]string, 0, len(payload.Data))
	seen := make(map[string]struct{})
	for _, grid := range payload.Data {
		source := strings.TrimSpace(grid.URL)
		if source == "" {
			source = strings.TrimSpace(grid.Thumb)
		}
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		options = append(options, source)
		if len(options) >= 4 {
			break
		}
	}

	return options, nil
}

func (c *SteamGridDBClient) getJSON(ctx context.Context, path string, params url.Values, target any) error {
	endpoint := steamGridDBBaseURL + path
	if params != nil {
		if encoded := params.Encode(); encoded != "" {
			endpoint += "?" + encoded
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf(msgBuildSteamGridDBRequestFailed, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf(msgVisitSteamGridDBFailed, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf(msgReadSteamGridDBResponseFailed, err)
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf(msgSteamGridDBRequestFailed, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf(msgParseSteamGridDBResponseFailed, err)
	}

	return nil
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
