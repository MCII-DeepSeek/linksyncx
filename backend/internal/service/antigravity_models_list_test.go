//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func resetAntigravityModelsCache() {
	antigravityModelsCache.clear()
	antigravityRawModelsCache.clear()
	resetAntigravityObservedModels()
}

func TestFetchAntigravityAccountModels_ProjectsAliasesAndSkipsUnmapped(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	account := &Account{
		ID:       9101,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"public-opus": "claude-opus-4-8",
				"claude-*":    "claude-opus-4-8",
			},
		},
	}
	svc := &AccountTestService{
		antigravityModelsLoad: func(context.Context, *Account) ([]antigravity.ClaudeModel, error) {
			return []antigravity.ClaudeModel{
				{ID: "claude-opus-4-8", Type: "model", DisplayName: "Opus 4.8"},
				{ID: "unmapped-model", Type: "model", DisplayName: "Hidden"},
				{ID: "gemini-3-flash", Type: "model", DisplayName: "Gemini 3 Flash"},
			}, nil
		},
	}

	models, err := svc.FetchAntigravityAccountModels(context.Background(), account)
	require.NoError(t, err)
	byID := map[string]antigravity.ClaudeModel{}
	for _, model := range models {
		byID[model.ID] = model
	}
	require.NotContains(t, byID, "unmapped-model")
	require.NotContains(t, byID, "claude-*")
	require.Equal(t, "public-opus", byID["public-opus"].DisplayName)
	require.NotContains(t, byID, "gemini-3-flash")
	require.Equal(t, "Opus 4.8", byID["claude-opus-4-8"].DisplayName)
}

func TestFetchAntigravityAccountModels_ErrorFallsBackAndIsNotCached(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	var calls atomic.Int32
	account := &Account{ID: 9102, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
	svc := &AccountTestService{}
	svc.antigravityModelsLoad = func(context.Context, *Account) ([]antigravity.ClaudeModel, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("upstream down")
		}
		return []antigravity.ClaudeModel{{ID: "claude-sonnet-4-6", Type: "model", DisplayName: "Sonnet"}}, nil
	}

	first, err := svc.FetchAntigravityAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Greater(t, len(first), 1)

	second, err := svc.FetchAntigravityAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 2, int(calls.Load()))
	require.Equal(t, []antigravity.ClaudeModel{{
		ID:          "claude-sonnet-4-6",
		Type:        "model",
		DisplayName: "Sonnet",
	}}, second)
}

func TestFetchAntigravityAccountModels_EmptyProjectionStaysEmptyAndIsCached(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	var calls atomic.Int32
	account := &Account{ID: 9103, Platform: PlatformAntigravity, Type: AccountTypeOAuth}
	svc := &AccountTestService{
		antigravityModelsLoad: func(context.Context, *Account) ([]antigravity.ClaudeModel, error) {
			calls.Add(1)
			return []antigravity.ClaudeModel{}, nil
		},
	}

	first, err := svc.FetchAntigravityAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, first)
	second, err := svc.FetchAntigravityAccountModels(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, second)
	require.Equal(t, 1, int(calls.Load()))
}

func TestFetchAntigravityAccountModels_SingleflightAndCacheKey(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	account := &Account{
		ID:       9104,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url":     "https://models.example/antigravity",
			"access_token": "token-a",
		},
	}
	svc := &AccountTestService{
		antigravityModelsLoad: func(context.Context, *Account) ([]antigravity.ClaudeModel, error) {
			calls.Add(1)
			once.Do(func() { close(started) })
			<-release
			return []antigravity.ClaudeModel{{ID: "claude-sonnet-4-6", Type: "model", DisplayName: "Sonnet"}}, nil
		},
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			models, err := svc.FetchAntigravityAccountModels(context.Background(), account)
			if err != nil {
				errCh <- err
				return
			}
			if len(models) == 0 {
				errCh <- errors.New("unexpected models")
				return
			}
			for _, model := range models {
				target, matched := account.ResolveMappedModel(model.ID)
				if !matched || target != "claude-sonnet-4-6" {
					errCh <- errors.New("unexpected models")
					return
				}
			}
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
	require.Equal(t, 1, int(calls.Load()))

	refreshed := &Account{
		ID:       account.ID,
		Platform: account.Platform,
		Type:     account.Type,
		Credentials: map[string]any{
			"base_url":     "https://models.example/antigravity",
			"access_token": "token-b",
		},
	}
	_, err := svc.FetchAntigravityAccountModels(context.Background(), refreshed)
	require.NoError(t, err)
	require.Equal(t, 1, int(calls.Load()), "access token refresh must not miss the cache")

	moved := &Account{
		ID:       account.ID,
		Platform: account.Platform,
		Type:     account.Type,
		Credentials: map[string]any{
			"base_url": "https://other.example/antigravity",
		},
	}
	_, err = svc.FetchAntigravityAccountModels(context.Background(), moved)
	require.NoError(t, err)
	require.Equal(t, 2, int(calls.Load()), "base URL change must miss the cache")
}

func TestSynthesizeAntigravityCatalogBody_OmitsUnknownReasoning(t *testing.T) {
	thinking := true
	images := true
	maxTokens := 128000
	body, err := synthesizeAntigravityCatalogBody(&antigravity.FetchAvailableModelsResponse{
		Models: map[string]antigravity.ModelInfo{
			"claude-opus-4-8": {
				DisplayName:      "Opus",
				SupportsThinking: &thinking,
				SupportsImages:   &images,
				MaxTokens:        &maxTokens,
			},
			"gemini-3-flash": {DisplayName: "Flash"},
		},
	})
	require.NoError(t, err)

	_, metadata, err := extractUpstreamModelCatalog(body, false)
	require.NoError(t, err)
	require.False(t, upstreamModelMetadataIsComplete(metadata["claude-opus-4-8"]))
	flash, ok := metadata["gemini-3-flash"]
	require.True(t, ok)
	require.Nil(t, flash.Reasoning)
	require.Equal(t, []string{"text"}, flash.InputModalities)
	require.Equal(t, []string{"text", "image"}, metadata["claude-opus-4-8"].InputModalities)
	require.Equal(t, int64(128000), metadata["claude-opus-4-8"].ContextWindow)
}

type antigravityCatalogPersistRepoStub struct {
	AccountRepository
	savedAccountID int64
	savedUpdates   map[string]any
	byGroup        map[int64][]Account
}

func (r *antigravityCatalogPersistRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.savedAccountID = id
	r.savedUpdates = updates
	return nil
}

func (r *antigravityCatalogPersistRepoStub) ListSchedulableByGroupID(_ context.Context, groupID int64) ([]Account, error) {
	return append([]Account(nil), r.byGroup[groupID]...), nil
}

func TestSyncUpstreamModelCatalog_AntigravityPersistsCatalogAndUnlocksNewUpstreamModels(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	repo := &antigravityCatalogPersistRepoStub{}
	upstream := &recordingAntigravityModelsUpstream{}
	svc := &AccountTestService{
		accountRepo:  repo,
		httpUpstream: upstream,
		cfg:          upstreamModelSyncTestConfig(),
	}
	// Return newly released upstream models not present in DefaultAntigravityModelMapping.
	account := &Account{
		ID:       9201,
		Platform: PlatformAntigravity,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"base_url": "https://relay.example",
			"api_key":  "relay-secret",
		},
	}

	customUpstream := &antigravityStaticJSONUpstream{
		body: `{"data":[{"id":"gemini-4-carbon-high","display_name":"Gemini 4 Carbon (High)"},{"id":"gemini-4-carbon-low","display_name":"Gemini 4 Carbon (Low)"},{"id":"claude-opus-5-6","display_name":"Claude Opus 5.6"}],"deprecated_model_ids":{"claude-opus-legacy":"claude-opus-5-6"}}`,
	}
	svc.httpUpstream = customUpstream

	catalog, err := svc.SyncUpstreamModelCatalog(context.Background(), account)
	require.NoError(t, err)
	require.Empty(t, catalog.Warnings, "Antigravity sync must not emit false incomplete-metadata warnings")
	require.ElementsMatch(t, []string{"claude-opus-5-6", "gemini-4-carbon-high", "gemini-4-carbon-low"}, catalog.Models)
	require.Equal(t, int64(9201), repo.savedAccountID)
	require.Contains(t, repo.savedUpdates, AntigravityModelCatalogExtraKey)

	snapshot := account.GetAntigravityModelCatalogSnapshot()
	require.NotNil(t, snapshot)
	require.Equal(t, "Gemini 4 Carbon (High)", snapshot.Models["gemini-4-carbon-high"].DisplayName)
	require.Equal(t, "claude-opus-5-6", snapshot.DeprecatedModelIDs["claude-opus-legacy"])

	// Unmapped Antigravity account now supports newly synced upstream models AND bare Gemini thinking routing!
	gatewaySvc := &AntigravityGatewayService{}
	require.True(t, account.IsModelSupported("claude-opus-5-6"))
	require.True(t, account.IsModelSupported("gemini-4-carbon-high"))
	require.True(t, account.IsModelSupported("gemini-4-carbon"))
	require.Equal(t, "claude-opus-5-6", gatewaySvc.getMappedModel(account, "claude-opus-5-6"))
	require.Equal(t, "gemini-4-carbon-high", gatewaySvc.getMappedModel(account, "gemini-4-carbon"))
	require.Equal(t, "gemini-4-carbon-low", gatewaySvc.getMappedModelForThinkingLevel(account, "gemini-4-carbon", "low"))
	require.Equal(t, "claude-opus-5-6", gatewaySvc.getMappedModel(account, "claude-opus-legacy"))
	// Legacy curated aliases remain intact.
	require.Equal(t, "claude-opus-4-6-thinking", gatewaySvc.getMappedModel(account, "claude-opus-4-6"))
}

type antigravityStaticJSONUpstream struct {
	body   string
	status int
	err    error
}

func (u *antigravityStaticJSONUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if u.err != nil {
		return nil, u.err
	}
	status := u.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(u.body)),
		Header:     make(http.Header),
	}, nil
}

func (u *antigravityStaticJSONUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

type antigravityPerAccountUpstream struct {
	responses map[int64]*antigravityStaticJSONUpstream
}

func (u *antigravityPerAccountUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	target, ok := u.responses[accountID]
	if !ok {
		return nil, errors.New("unexpected account")
	}
	return target.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *antigravityPerAccountUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestAntigravityGatewayService_FetchGroupModels_FailoverAndSnapshotFallback(t *testing.T) {
	resetAntigravityModelsCache()
	t.Cleanup(resetAntigravityModelsCache)

	groupID := int64(77)
	repo := &antigravityCatalogPersistRepoStub{
		byGroup: map[int64][]Account{
			groupID: {
				{
					ID:          1,
					Platform:    PlatformAntigravity,
					Type:        AccountTypeUpstream,
					Priority:    1,
					Status:      StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"base_url": "https://broken.example", "api_key": "k1"},
				},
				{
					ID:          2,
					Platform:    PlatformAntigravity,
					Type:        AccountTypeUpstream,
					Priority:    2,
					Status:      StatusActive,
					Schedulable: true,
					Credentials: map[string]any{"base_url": "https://healthy.example", "api_key": "k2"},
				},
				{
					ID:          3,
					Platform:    PlatformAntigravity,
					Type:        AccountTypeUpstream,
					Priority:    3,
					Status:      StatusActive,
					Schedulable: true,
					Credentials: map[string]any{
						"base_url":      "https://mapped.example",
						"api_key":       "k3",
						"model_mapping": map[string]any{"team-sonnet-alias": "claude-sonnet-4-6"},
					},
				},
			},
		},
	}
	upstream := &antigravityPerAccountUpstream{
		responses: map[int64]*antigravityStaticJSONUpstream{
			1: {status: http.StatusBadGateway, body: `{"error":"down"}`},
			2: {status: http.StatusOK, body: `{"data":[{"id":"gemini-4-carbon-high","display_name":"Gemini 4 Carbon (High)"},{"id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6"}]}`},
		},
	}
	gw := &AntigravityGatewayService{
		accountRepo:  repo,
		httpUpstream: upstream,
	}

	models, selected, ok := gw.FetchGroupModels(context.Background(), &groupID, false, 3)
	require.True(t, ok)
	require.NotNil(t, selected)
	require.Equal(t, int64(2), selected.ID, "must fail over past broken priority-1 account")
	byID := map[string]string{}
	for _, m := range models {
		byID[m.ID] = m.DisplayName
	}
	require.Equal(t, "Gemini 4 Carbon (High)", byID["gemini-4-carbon-high"])
	require.Equal(t, "Claude Sonnet 4.6", byID["claude-sonnet-4-6"])
	require.Equal(t, "team-sonnet-alias", byID["team-sonnet-alias"])

	// Gemini v1beta projection exports both thinking variants and bare Gemini names.
	geminiModels, gOK := gw.FetchGroupGeminiModels(context.Background(), &groupID, false, 3)
	require.True(t, gOK)
	geminiNames := make([]string, 0, len(geminiModels))
	for _, gm := range geminiModels {
		geminiNames = append(geminiNames, gm.Name)
	}
	require.Contains(t, geminiNames, "models/gemini-4-carbon-high")
	require.Contains(t, geminiNames, "models/gemini-4-carbon")
	require.NotContains(t, geminiNames, "models/claude-sonnet-4-6")
}
