package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/gemini"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"golang.org/x/sync/singleflight"
)

const (
	// Align with OpenAI model discovery cache windows:
	// - within 1m (fresh): return immediately without upstream traffic
	// - 1m..5m (optimistic SWR): return cached list and refresh in background
	// - >5m (expired): wait for live upstream refresh via singleflight
	antigravityModelsFreshTTL      = 1 * time.Minute
	antigravityModelsCacheTTL      = 5 * time.Minute
	antigravityModelsRefreshWindow = 15 * time.Second
)

// antigravityModelsLoader, when set, replaces the live upstream fetch.
// Production leaves it nil. Tests use it to avoid calling Google.
type antigravityModelsLoader func(ctx context.Context, account *Account) ([]antigravity.ClaudeModel, error)

type antigravityModelsCacheEntry struct {
	models     []antigravity.ClaudeModel
	freshUntil time.Time
	expiresAt  time.Time
}

type antigravityModelsCacheStore struct {
	mu           sync.Mutex
	items        map[string]antigravityModelsCacheEntry
	group        singleflight.Group
	refreshGroup singleflight.Group
}

var antigravityModelsCache = &antigravityModelsCacheStore{
	items: map[string]antigravityModelsCacheEntry{},
}

// antigravityRawModelsCache caches raw upstream models per account (independent of model_mapping)
// so mapping edits and gateway discovery can re-project without hitting upstream again.
var antigravityRawModelsCache = &antigravityModelsCacheStore{
	items: map[string]antigravityModelsCacheEntry{},
}

type antigravityCacheLookupState int

const (
	antigravityCacheMiss antigravityCacheLookupState = iota
	antigravityCacheStale
	antigravityCacheFresh
)

func (c *antigravityModelsCacheStore) lookup(key string, now time.Time) ([]antigravity.ClaudeModel, antigravityCacheLookupState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.items[key]
	if !ok || !now.Before(entry.expiresAt) {
		return nil, antigravityCacheMiss
	}
	models := cloneAntigravityClaudeModels(entry.models)
	if !entry.freshUntil.IsZero() && !now.Before(entry.freshUntil) {
		return models, antigravityCacheStale
	}
	return models, antigravityCacheFresh
}

func (c *antigravityModelsCacheStore) get(key string, now time.Time) ([]antigravity.ClaudeModel, bool) {
	models, state := c.lookup(key, now)
	return models, state != antigravityCacheMiss
}

func (c *antigravityModelsCacheStore) set(key string, models []antigravity.ClaudeModel, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = antigravityModelsCacheEntry{
		models:     cloneAntigravityClaudeModels(models),
		freshUntil: now.Add(antigravityModelsFreshTTL),
		expiresAt:  now.Add(antigravityModelsCacheTTL),
	}
}

func (c *antigravityModelsCacheStore) clear() {
	c.mu.Lock()
	c.items = map[string]antigravityModelsCacheEntry{}
	c.mu.Unlock()
}

func cloneAntigravityClaudeModels(models []antigravity.ClaudeModel) []antigravity.ClaudeModel {
	if models == nil {
		return []antigravity.ClaudeModel{}
	}
	out := make([]antigravity.ClaudeModel, len(models))
	copy(out, models)
	return out
}

func antigravityRawModelsCacheKey(account *Account) string {
	if account == nil {
		return ""
	}
	h := sha256.New()
	var proxyID int64
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	_, _ = fmt.Fprintf(h, "%d\n%s\n%s\n%s\n%d\n",
		account.ID,
		account.Type,
		strings.TrimSpace(account.GetCredential("project_id")),
		strings.TrimSpace(account.GetCredential("base_url")),
		proxyID,
	)
	sum := sha256.Sum256([]byte(account.GetCredential("api_key")))
	_, _ = h.Write(sum[:])
	return hex.EncodeToString(h.Sum(nil))
}

// antigravityModelsCacheKey fingerprints the account without the access token.
// A token refresh must not miss the cache; a mapping, base URL, or key change must.
func antigravityModelsCacheKey(account *Account) string {
	h := sha256.New()
	_, _ = h.Write([]byte(antigravityRawModelsCacheKey(account)))
	// Fingerprint the stored mapping only. GetModelMapping injects the
	// default catalog, which would make an empty mapping look custom.
	mapping := rawAntigravityModelMapping(account)
	keys := make([]string, 0, len(mapping))
	for alias, target := range mapping {
		keys = append(keys, alias+"\x00"+target)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_, _ = h.Write([]byte{'\n'})
		_, _ = h.Write([]byte(key))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// FetchAntigravityAccountModels returns the account's public model aliases.
// Like OpenAI account discovery, fresh entries return immediately, optimistic stale
// entries trigger a background refresh, and live fetch failures fall back to any
// persisted account catalog snapshot before the static catalog without caching errors.
func (s *AccountTestService) FetchAntigravityAccountModels(ctx context.Context, account *Account) ([]antigravity.ClaudeModel, error) {
	if s == nil || account == nil {
		return antigravity.DefaultModels(), nil
	}
	load := s.antigravityModelsLoad
	isCustomLoader := load != nil
	if load == nil {
		load = s.loadAntigravityUpstreamModels
	}
	models, err := fetchCachedAntigravityProjectedModels(ctx, account, load, !isCustomLoader)
	if err != nil {
		slog.Warn("antigravity model discovery failed; using fallback catalog", "account_id", account.ID, "error", err)
		if fallback, ok := fallbackAntigravityAccountModels(account); ok {
			return fallback, nil
		}
		return antigravity.DefaultModels(), nil
	}
	return models, nil
}

func fallbackAntigravityAccountModels(account *Account) ([]antigravity.ClaudeModel, bool) {
	if account == nil {
		return nil, false
	}
	if snapshotModels, ok := account.AntigravitySnapshotClaudeModels(); ok {
		return projectAntigravityAccountModels(snapshotModels, account), true
	}
	return nil, false
}

func fetchCachedAntigravityProjectedModels(
	ctx context.Context,
	account *Account,
	load antigravityModelsLoader,
	recordObserved bool,
) ([]antigravity.ClaudeModel, error) {
	if account == nil {
		return nil, errors.New("account is required")
	}
	now := time.Now()
	key := antigravityModelsCacheKey(account)
	if cached, state := antigravityModelsCache.lookup(key, now); state != antigravityCacheMiss {
		if state == antigravityCacheStale {
			accountCopy := *account
			go func() {
				refreshCtx, cancel := context.WithTimeout(context.Background(), antigravityModelsRefreshWindow)
				defer cancel()
				_, _, _ = antigravityModelsCache.refreshGroup.Do(key, func() (any, error) {
					loaded, loadErr := load(refreshCtx, &accountCopy)
					if loadErr != nil {
						return nil, loadErr
					}
					if loaded == nil {
						loaded = []antigravity.ClaudeModel{}
					}
					if recordObserved && accountCopy.ID > 0 {
						recordAntigravityObservedClaudeModels(accountCopy.ID, loaded)
						antigravityRawModelsCache.set(antigravityRawModelsCacheKey(&accountCopy), loaded, time.Now())
					}
					projected := projectAntigravityAccountModels(loaded, &accountCopy)
					antigravityModelsCache.set(key, projected, time.Now())
					return projected, nil
				})
			}()
		}
		return cached, nil
	}

	v, err, _ := antigravityModelsCache.group.Do(key, func() (any, error) {
		if cached, ok := antigravityModelsCache.get(key, time.Now()); ok {
			return cached, nil
		}
		// Reuse unexpired raw cache when only model_mapping changed on the same account.
		if recordObserved {
			if rawCached, ok := antigravityRawModelsCache.get(antigravityRawModelsCacheKey(account), time.Now()); ok {
				projected := projectAntigravityAccountModels(rawCached, account)
				antigravityModelsCache.set(key, projected, time.Now())
				return cloneAntigravityClaudeModels(projected), nil
			}
		}
		loaded, loadErr := load(ctx, account)
		if loadErr != nil {
			return nil, loadErr
		}
		if loaded == nil {
			loaded = []antigravity.ClaudeModel{}
		}
		if recordObserved && account.ID > 0 {
			recordAntigravityObservedClaudeModels(account.ID, loaded)
			antigravityRawModelsCache.set(antigravityRawModelsCacheKey(account), loaded, time.Now())
		}
		projected := projectAntigravityAccountModels(loaded, account)
		antigravityModelsCache.set(key, projected, time.Now())
		return cloneAntigravityClaudeModels(projected), nil
	})
	if err != nil {
		return nil, err
	}
	models, ok := v.([]antigravity.ClaudeModel)
	if !ok || models == nil {
		return []antigravity.ClaudeModel{}, nil
	}
	return cloneAntigravityClaudeModels(models), nil
}

func invalidateAntigravityAccountModelsCache(account *Account, rawModels []antigravity.ClaudeModel) {
	if account == nil {
		return
	}
	now := time.Now()
	if rawModels != nil {
		if account.ID > 0 {
			recordAntigravityObservedClaudeModels(account.ID, rawModels)
		}
		antigravityRawModelsCache.set(antigravityRawModelsCacheKey(account), rawModels, now)
		projected := projectAntigravityAccountModels(rawModels, account)
		antigravityModelsCache.set(antigravityModelsCacheKey(account), projected, now)
	}
}

func (s *AccountTestService) loadAntigravityUpstreamModels(ctx context.Context, account *Account) ([]antigravity.ClaudeModel, error) {
	return loadAntigravityUpstreamModelsCommon(
		ctx,
		account,
		s.antigravityGatewayService,
		s.httpUpstream,
		s.cfg,
		s.validateUpstreamBaseURL,
		func(req *http.Request, proxyURL string, acc *Account) (*http.Response, error) {
			return s.doUpstreamModelsRequest(req, proxyURL, acc)
		},
	)
}

func loadAntigravityUpstreamModelsCommon(
	ctx context.Context,
	account *Account,
	gatewaySvc *AntigravityGatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	validateBaseURL func(string) (string, error),
	doRelay func(*http.Request, string, *Account) (*http.Response, error),
) ([]antigravity.ClaudeModel, error) {
	if account == nil {
		return nil, errors.New("account is required")
	}
	// type=upstream keeps a third-party API key. It must use that key against
	// the account's own base URL. Treating it as OAuth sends the key to Google.
	if antigravityUsesRelayModelList(account) {
		return loadAntigravityRelayModelsWithDeps(ctx, account, httpUpstream, cfg, validateBaseURL, doRelay)
	}
	return loadAntigravityOAuthModelsWithDeps(ctx, account, gatewaySvc, cfg)
}

func loadAntigravityRelayModelsWithDeps(
	ctx context.Context,
	account *Account,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	validateBaseURL func(string) (string, error),
	doRelay func(*http.Request, string, *Account) (*http.Response, error),
) ([]antigravity.ClaudeModel, error) {
	if httpUpstream == nil && doRelay == nil {
		return nil, errors.New("upstream HTTP client is not configured")
	}
	req, err := buildAntigravityRelayModelsRequest(ctx, account, validateBaseURL)
	if err != nil {
		return nil, err
	}
	proxyURL, err := upstreamModelsProxyURL(account)
	if err != nil {
		return nil, err
	}
	var resp *http.Response
	if doRelay != nil {
		resp, err = doRelay(req, proxyURL, account)
	} else {
		resp, err = httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	bodyLimit := resolveModelsListReadLimit(cfg)
	body, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > bodyLimit {
		return nil, fmt.Errorf("antigravity model list exceeds %d bytes", bodyLimit)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("antigravity model list returned HTTP %d", resp.StatusCode)
	}
	return parseAntigravityAPIKeyModels(body)
}

func loadAntigravityOAuthModelsWithDeps(
	ctx context.Context,
	account *Account,
	gatewaySvc *AntigravityGatewayService,
	cfg *config.Config,
) ([]antigravity.ClaudeModel, error) {
	if gatewaySvc == nil || gatewaySvc.GetTokenProvider() == nil {
		return nil, errors.New("antigravity token provider is not configured")
	}
	accessToken, err := gatewaySvc.GetTokenProvider().GetAccessToken(ctx, account)
	if err != nil {
		return nil, err
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, errors.New("no antigravity access token is available")
	}
	proxyURL, err := upstreamModelsProxyURL(account)
	if err != nil {
		return nil, err
	}
	ctx = withAntigravityDevice(ctx, account)
	client, err := antigravity.NewClient(proxyURL)
	if err != nil {
		return nil, err
	}
	projectID := strings.TrimSpace(account.GetCredential("project_id"))
	if projectID == "" {
		projectID = strings.TrimSpace(account.GetCredential(antigravityProjectIDFallbackCredentialKey))
	}
	if projectID == "" {
		projectID = strings.TrimSpace(account.GetExtraString(antigravityProjectIDFallbackCredentialKey))
	}
	modelsResp, _, err := client.FetchAvailableModels(
		ctx,
		accessToken,
		projectID,
		resolveModelsListReadLimit(cfg),
	)
	if err != nil {
		return nil, err
	}
	return claudeModelsFromAvailable(modelsResp), nil
}

func (s *AntigravityGatewayService) validateRelayBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty base URL")
	}
	if s != nil && s.settingService != nil && s.settingService.cfg != nil && s.settingService.cfg.Security.URLAllowlist.Enabled {
		return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
			AllowedHosts:     s.settingService.cfg.Security.URLAllowlist.UpstreamHosts,
			RequireAllowlist: true,
			AllowPrivate:     s.settingService.cfg.Security.URLAllowlist.AllowPrivateHosts,
		})
	}
	if s != nil && s.settingService != nil && s.settingService.cfg != nil && !s.settingService.cfg.Security.URLAllowlist.AllowInsecureHTTP {
		return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
			RequireAllowlist: false,
			AllowPrivate:     s.settingService.cfg.Security.URLAllowlist.AllowPrivateHosts,
		})
	}
	return urlvalidator.ValidateURLFormat(raw, true)
}

func (s *AntigravityGatewayService) gatewayConfig() *config.Config {
	if s != nil && s.settingService != nil {
		return s.settingService.cfg
	}
	return nil
}

// FetchAccountModels discovers a single Antigravity account's live upstream models
// using the shared fresh/SWR singleflight cache and projects them through the account's mapping.
func (s *AntigravityGatewayService) FetchAccountModels(ctx context.Context, account *Account) ([]antigravity.ClaudeModel, error) {
	if s == nil {
		return nil, errors.New("antigravity gateway service is not configured")
	}
	if account == nil {
		return nil, errors.New("account is required")
	}
	return fetchCachedAntigravityProjectedModels(ctx, account, func(fetchCtx context.Context, acc *Account) ([]antigravity.ClaudeModel, error) {
		return loadAntigravityUpstreamModelsCommon(
			fetchCtx,
			acc,
			s,
			s.httpUpstream,
			s.gatewayConfig(),
			s.validateRelayBaseURL,
			nil,
		)
	}, true)
}

// FetchGroupModels discovers upstream Antigravity models for a group (or the global pool
// when groupID is nil), mirroring OpenAI's scheduled upstream model discovery:
//   - selects schedulable Antigravity accounts ordered by priority and fails over on error
//   - merges public aliases from any sibling accounts with custom mappings or synced snapshots
//   - falls back to persisted account catalog snapshots when live upstream calls fail
func (s *AntigravityGatewayService) FetchGroupModels(
	ctx context.Context,
	groupID *int64,
	requireMixed bool,
	maxSwitches int,
) ([]antigravity.ClaudeModel, *Account, bool) {
	if s == nil || s.accountRepo == nil {
		return nil, nil, false
	}
	var (
		accounts []Account
		err      error
	)
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil || len(accounts) == 0 {
		return nil, nil, false
	}

	candidates := make([]Account, 0, len(accounts))
	for _, acc := range accounts {
		if acc.Platform != PlatformAntigravity {
			continue
		}
		if requireMixed && !acc.IsMixedSchedulingEnabled() {
			continue
		}
		candidates = append(candidates, acc)
	}
	if len(candidates) == 0 {
		return nil, nil, false
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})

	if maxSwitches <= 0 {
		maxSwitches = 3
	}

	byID := make(map[string]antigravity.ClaudeModel)
	addModels := func(items []antigravity.ClaudeModel) {
		for _, item := range items {
			id := strings.TrimSpace(item.ID)
			if id == "" || strings.Contains(id, "*") {
				continue
			}
			item.ID = id
			if existing, exists := byID[id]; exists {
				if (existing.DisplayName == "" || existing.DisplayName == id) && item.DisplayName != "" && item.DisplayName != id {
					byID[id] = item
				}
				continue
			}
			if strings.TrimSpace(item.DisplayName) == "" {
				item.DisplayName = id
			}
			if strings.TrimSpace(item.Type) == "" {
				item.Type = "model"
			}
			byID[id] = item
		}
	}

	var primaryAccount *Account
	attempts := 0
	for i := range candidates {
		if ctx.Err() != nil {
			break
		}
		acc := &candidates[i]
		// If primary already succeeded, only query additional accounts from warm cache,
		// persisted snapshots, or explicit custom mappings to avoid fanout storms.
		if primaryAccount != nil {
			if cached, ok := antigravityModelsCache.get(antigravityModelsCacheKey(acc), time.Now()); ok {
				addModels(cached)
				continue
			}
			if snapModels, ok := fallbackAntigravityAccountModels(acc); ok {
				addModels(snapModels)
				continue
			}
			for alias := range rawAntigravityModelMapping(acc) {
				alias = strings.TrimSpace(alias)
				if alias != "" && !strings.Contains(alias, "*") {
					addModels([]antigravity.ClaudeModel{{ID: alias, Type: "model", DisplayName: alias}})
				}
			}
			continue
		}

		if attempts > maxSwitches {
			break
		}
		attempts++
		models, fetchErr := s.FetchAccountModels(ctx, acc)
		if fetchErr != nil {
			slog.Warn("antigravity group model discovery failover", "account_id", acc.ID, "error", fetchErr)
			continue
		}
		primaryAccount = acc
		addModels(models)
	}

	if primaryAccount == nil {
		// All live upstream attempts failed: try persisted account snapshots or explicit custom mappings.
		hadSnapshot := false
		for i := range candidates {
			acc := &candidates[i]
			if snapModels, ok := fallbackAntigravityAccountModels(acc); ok {
				addModels(snapModels)
				hadSnapshot = true
				continue
			}
			raw := rawAntigravityModelMapping(acc)
			if len(raw) > 0 {
				hadSnapshot = true
				for alias := range raw {
					alias = strings.TrimSpace(alias)
					if alias != "" && !strings.Contains(alias, "*") {
						addModels([]antigravity.ClaudeModel{{ID: alias, Type: "model", DisplayName: alias}})
					}
				}
			}
		}
		if !hadSnapshot {
			return nil, nil, false
		}
	}

	out := make([]antigravity.ClaudeModel, 0, len(byID))
	for _, model := range byID {
		out = append(out, model)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, primaryAccount, true
}

// FetchGroupGeminiModels discovers Gemini-family models served by Antigravity accounts
// for /antigravity/v1beta/models and mixed Gemini /v1beta/models endpoints.
func (s *AntigravityGatewayService) FetchGroupGeminiModels(
	ctx context.Context,
	groupID *int64,
	requireMixed bool,
	maxSwitches int,
) ([]gemini.Model, bool) {
	claudeModels, _, ok := s.FetchGroupModels(ctx, groupID, requireMixed, maxSwitches)
	if !ok {
		return nil, false
	}
	methods := []string{"generateContent", "streamGenerateContent"}
	byName := make(map[string]gemini.Model)
	addGemini := func(id, display string) {
		id = strings.TrimSpace(strings.TrimPrefix(id, "models/"))
		if !isAntigravityGeminiModel(id) || strings.Contains(id, "*") {
			return
		}
		name := "models/" + id
		if _, exists := byName[name]; exists {
			return
		}
		byName[name] = gemini.Model{
			Name:                       name,
			DisplayName:                strings.TrimSpace(display),
			SupportedGenerationMethods: methods,
		}
	}
	for _, m := range claudeModels {
		addGemini(m.ID, m.DisplayName)
		if bare, hasBare := bareGeminiModelFromThinkingVariant(m.ID); hasBare {
			addGemini(bare, bare)
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]gemini.Model, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out, true
}

func parseAntigravityAPIKeyModels(body []byte) ([]antigravity.ClaudeModel, error) {
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
		Models []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
			CreatedAt   string `json:"created_at"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	entries := payload.Data
	if len(entries) == 0 {
		entries = payload.Models
	}
	out := make([]antigravity.ClaudeModel, 0, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			id = strings.TrimSpace(entry.Name)
		}
		id = strings.TrimPrefix(id, "models/")
		if id == "" {
			continue
		}
		display := strings.TrimSpace(entry.DisplayName)
		if display == "" {
			display = id
		}
		out = append(out, antigravity.ClaudeModel{
			ID:          id,
			Type:        "model",
			DisplayName: display,
			CreatedAt:   entry.CreatedAt,
		})
	}
	return out, nil
}

func claudeModelsFromAvailable(resp *antigravity.FetchAvailableModelsResponse) []antigravity.ClaudeModel {
	if resp == nil || len(resp.Models) == 0 {
		return []antigravity.ClaudeModel{}
	}
	ids := make([]string, 0, len(resp.Models))
	for id := range resp.Models {
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]antigravity.ClaudeModel, 0, len(ids))
	for _, id := range ids {
		info := resp.Models[id]
		display := strings.TrimSpace(info.DisplayName)
		if display == "" {
			display = strings.TrimSpace(id)
		}
		out = append(out, antigravity.ClaudeModel{
			ID:          strings.TrimSpace(id),
			Type:        "model",
			DisplayName: display,
		})
	}
	return out
}

// projectAntigravityAccountModels publishes names the same way the GPT model
// list does. An empty stored mapping returns this account's upstream IDs
// unchanged. A custom mapping keeps a public name only when the raw mapping
// resolves it onto a model this account actually returned. Wildcard keys are
// not names, but upstream IDs they match stay candidates. Default aliases are
// not injected here; request routing still applies those on an empty mapping.
func projectAntigravityAccountModels(upstream []antigravity.ClaudeModel, account *Account) []antigravity.ClaudeModel {
	if account == nil {
		return []antigravity.ClaudeModel{}
	}
	byID := make(map[string]antigravity.ClaudeModel, len(upstream))
	upstreamIDs := make([]string, 0, len(upstream))
	for _, model := range upstream {
		id := strings.TrimSpace(model.ID)
		if id == "" || strings.Contains(id, "*") {
			continue
		}
		if _, ok := byID[id]; ok {
			continue
		}
		model.ID = id
		byID[id] = model
		upstreamIDs = append(upstreamIDs, id)
	}
	sort.Strings(upstreamIDs)
	raw := rawAntigravityModelMapping(account)
	if len(raw) == 0 {
		out := make([]antigravity.ClaudeModel, 0, len(upstreamIDs))
		for _, id := range upstreamIDs {
			out = append(out, byID[id])
		}
		return out
	}
	aliases := make([]string, 0, len(raw))
	for alias := range raw {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	candidates := make([]string, 0, len(upstreamIDs)+len(aliases))
	candidates = append(candidates, upstreamIDs...)
	candidates = append(candidates, aliases...)
	out := make([]antigravity.ClaudeModel, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, id := range candidates {
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "*") {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		target, matched := resolveRequestedModelInMapping(raw, id)
		target = strings.TrimSpace(target)
		src, available := byID[target]
		if !matched || !available {
			continue
		}
		seen[id] = struct{}{}
		display := strings.TrimSpace(src.DisplayName)
		if id != target || display == "" {
			display = id
		}
		modelType := src.Type
		if modelType == "" {
			modelType = "model"
		}
		out = append(out, antigravity.ClaudeModel{
			ID:          id,
			Type:        modelType,
			DisplayName: display,
			CreatedAt:   src.CreatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func rawAntigravityModelMapping(account *Account) map[string]string {
	if account == nil || account.Credentials == nil {
		return nil
	}
	return stringMappingFromRaw(account.Credentials["model_mapping"])
}
