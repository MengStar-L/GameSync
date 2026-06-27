package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const rawgBaseURL = "https://api.rawg.io/api"

var rawgHTMLTagPattern = regexp.MustCompile(`<[^>]+>`)

type RAWGClient struct {
	apiKey     string
	httpClient *http.Client
}

type RAWGSearchResult struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Slug         string   `json:"slug"`
	Released     string   `json:"released"`
	CoverPath    string   `json:"coverPath"`
	CoverOptions []string `json:"coverOptions,omitempty"`
	Rating       float64  `json:"rating"`
	Metacritic   int      `json:"metacritic"`
}

type RAWGGameDetails struct {
	RawgID       int      `json:"rawgId"`
	RawgSlug     string   `json:"rawgSlug"`
	RawgURL      string   `json:"rawgUrl"`
	Name         string   `json:"name"`
	CoverPath    string   `json:"coverPath"`
	CoverOptions []string `json:"coverOptions,omitempty"`
	Description  string   `json:"description"`
	Released     string   `json:"released"`
	Rating       float64  `json:"rating"`
	RatingTop    int      `json:"ratingTop"`
	Metacritic   int      `json:"metacritic"`
	Genres       []string `json:"genres"`
	Platforms    []string `json:"platforms"`
	Developers   []string `json:"developers"`
	Publishers   []string `json:"publishers"`
	Website      string   `json:"website"`
	RawgTags     []string `json:"rawgTags"`
}

type rawgNamedItem struct {
	Name string `json:"name"`
}

type rawgPlatformWrapper struct {
	Platform rawgNamedItem `json:"platform"`
}

type rawgScreenshot struct {
	Image string `json:"image"`
}

type rawgGamePayload struct {
	ID                        int                   `json:"id"`
	Name                      string                `json:"name"`
	Slug                      string                `json:"slug"`
	Released                  string                `json:"released"`
	BackgroundImage           string                `json:"background_image"`
	BackgroundImageAdditional string                `json:"background_image_additional"`
	Rating                    float64               `json:"rating"`
	RatingTop                 int                   `json:"rating_top"`
	Metacritic                int                   `json:"metacritic"`
	Description               string                `json:"description"`
	DescriptionRaw            string                `json:"description_raw"`
	Website                   string                `json:"website"`
	Genres                    []rawgNamedItem       `json:"genres"`
	Tags                      []rawgNamedItem       `json:"tags"`
	Developers                []rawgNamedItem       `json:"developers"`
	Publishers                []rawgNamedItem       `json:"publishers"`
	Platforms                 []rawgPlatformWrapper `json:"platforms"`
	ShortScreenshots          []rawgScreenshot      `json:"short_screenshots"`
}

type rawgListResponse struct {
	Results []rawgGamePayload `json:"results"`
}

func NewRAWGClient(apiKey string) (*RAWGClient, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New(msgRawgAPIKeyRequired)
	}

	return &RAWGClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}, nil
}

func (c *RAWGClient) SearchGames(ctx context.Context, query string) ([]RAWGSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New(msgRawgQueryRequired)
	}

	params := url.Values{}
	params.Set("search", query)
	params.Set("page_size", "8")
	params.Set("search_precise", "true")

	var payload rawgListResponse
	if err := c.getJSON(ctx, "/games", params, &payload); err != nil {
		return nil, err
	}

	results := make([]RAWGSearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, RAWGSearchResult{
			ID:           item.ID,
			Name:         strings.TrimSpace(item.Name),
			Slug:         strings.TrimSpace(item.Slug),
			Released:     strings.TrimSpace(item.Released),
			CoverPath:    coverFromRAWG(item),
			CoverOptions: rawgPortraitCoverOptions(item),
			Rating:       item.Rating,
			Metacritic:   item.Metacritic,
		})
	}

	return results, nil
}

func (c *RAWGClient) GetGameDetails(ctx context.Context, rawgID int) (RAWGGameDetails, error) {
	if rawgID <= 0 {
		return RAWGGameDetails{}, errors.New(msgInvalidRawgGameID)
	}

	var payload rawgGamePayload
	if err := c.getJSON(ctx, fmt.Sprintf("/games/%d", rawgID), nil, &payload); err != nil {
		return RAWGGameDetails{}, err
	}

	return RAWGGameDetails{
		RawgID:       payload.ID,
		RawgSlug:     strings.TrimSpace(payload.Slug),
		RawgURL:      rawgGameURL(payload.Slug),
		Name:         strings.TrimSpace(payload.Name),
		CoverPath:    coverFromRAWG(payload),
		CoverOptions: rawgPortraitCoverOptions(payload),
		Description:  rawgDescription(payload),
		Released:     strings.TrimSpace(payload.Released),
		Rating:       payload.Rating,
		RatingTop:    payload.RatingTop,
		Metacritic:   payload.Metacritic,
		Genres:       uniqueNonEmptyStrings(namedItemsToNames(payload.Genres)),
		Platforms:    uniqueNonEmptyStrings(platformWrappersToNames(payload.Platforms)),
		Developers:   uniqueNonEmptyStrings(namedItemsToNames(payload.Developers)),
		Publishers:   uniqueNonEmptyStrings(namedItemsToNames(payload.Publishers)),
		Website:      strings.TrimSpace(payload.Website),
		RawgTags:     buildRAWGTags(payload),
	}, nil
}

func (c *RAWGClient) getJSON(ctx context.Context, path string, params url.Values, target any) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("key", c.apiKey)

	endpoint := rawgBaseURL + path
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf(msgBuildRawgRequestFailed, err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf(msgVisitRawgFailed, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf(msgReadRawgResponseFailed, err)
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf(msgRawgRequestFailed, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf(msgParseRawgResponseFailed, err)
	}

	return nil
}

func rawgGameURL(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ""
	}
	return "https://rawg.io/games/" + slug
}

func coverFromRAWG(payload rawgGamePayload) string {
	if options := rawgPortraitCoverOptions(payload); len(options) > 0 {
		return options[0]
	}
	if cover := strings.TrimSpace(payload.BackgroundImage); cover != "" {
		return cover
	}
	return strings.TrimSpace(payload.BackgroundImageAdditional)
}

func rawgPortraitCoverOptions(payload rawgGamePayload) []string {
	options := make([]string, 0, 4)
	seen := make(map[string]struct{})
	appendOption := func(source string) {
		source = strings.TrimSpace(source)
		if source == "" {
			return
		}
		if _, exists := seen[source]; exists {
			return
		}
		seen[source] = struct{}{}
		options = append(options, source)
	}

	for _, screenshot := range payload.ShortScreenshots {
		appendOption(screenshot.Image)
		if len(options) >= 4 {
			break
		}
	}
	if len(options) < 4 {
		appendOption(payload.BackgroundImage)
	}
	if len(options) < 4 {
		appendOption(payload.BackgroundImageAdditional)
	}

	return options
}

func rawgDescription(payload rawgGamePayload) string {
	if description := strings.TrimSpace(payload.DescriptionRaw); description != "" {
		return description
	}
	return htmlToPlainText(payload.Description)
}

func htmlToPlainText(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}

	replacer := strings.NewReplacer(
		"<br>", "\n",
		"<br/>", "\n",
		"<br />", "\n",
		"</p>", "\n\n",
		"</div>", "\n",
		"</li>", "\n",
	)
	plain := replacer.Replace(input)
	plain = rawgHTMLTagPattern.ReplaceAllString(plain, "")
	plain = html.UnescapeString(plain)

	lines := strings.Split(plain, "\n")
	cleaned := make([]string, 0, len(lines))
	lastBlank := false
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			if lastBlank {
				continue
			}
			lastBlank = true
			cleaned = append(cleaned, "")
			continue
		}
		lastBlank = false
		cleaned = append(cleaned, line)
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func namedItemsToNames(items []rawgNamedItem) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Name)
	}
	return result
}

func platformWrappersToNames(items []rawgPlatformWrapper) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Platform.Name)
	}
	return result
}

func buildRAWGTags(payload rawgGamePayload) []string {
	return uniqueNonEmptyStrings(append(
		append(namedItemsToNames(payload.Tags), namedItemsToNames(payload.Genres)...),
		platformWrappersToNames(payload.Platforms)...,
	))
}

func uniqueNonEmptyStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
