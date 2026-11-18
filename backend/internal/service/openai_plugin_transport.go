package service

import "net/http"

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	return s.doOpenAIHTTPWithTLS(request, proxyURL, account)
}

// doOpenAIHTTPWithTLS 在不经过 OAuth 插件拦截的附属 HTTP 请求上统一应用账号级 TLS 指纹配置。
func (s *OpenAIGatewayService) doOpenAIHTTPWithTLS(request *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	var accountID int64
	var concurrency int
	if account != nil {
		accountID = account.ID
		concurrency = account.Concurrency
	}
	if account != nil && s != nil && s.tlsFPProfileService != nil {
		if profile := s.tlsFPProfileService.ResolveTLSProfile(account); profile != nil {
			return s.httpUpstream.DoWithTLS(request, proxyURL, accountID, concurrency, profile)
		}
	}
	return s.httpUpstream.Do(request, proxyURL, accountID, concurrency)
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (*http.Response, error) {
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if useTLSFallback {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
