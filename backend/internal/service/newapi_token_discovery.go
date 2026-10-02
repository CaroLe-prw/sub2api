package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	newAPITokenListPageSize  = 100
	newAPITokenListMaxPages  = 5
	newAPITokenMaxKeyLookups = 10
)

// findTokenInUserList uses the authenticated user's own token list. It never
// selects by name, masked key, or list position: the complete key must match.
func (c *NewAPIClient) findTokenInUserList(ctx context.Context, connection NewAPIConnection) (*newAPIToken, error) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = newAPIRequestTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var matched *newAPIToken
	seen := 0
	keyLookups := 0
	for pageNumber := 1; pageNumber <= newAPITokenListMaxPages; pageNumber++ {
		query := url.Values{"p": {strconv.Itoa(pageNumber)}, "size": {strconv.Itoa(newAPITokenListPageSize)}}
		status, body, err := c.get(ctx, connection.BaseURL, "/api/token/", query, connection.UserAccessToken, connection.UserID)
		if err != nil {
			return nil, err
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return nil, newAPIHTTPError("token_list", status)
		}
		var envelope newAPIEnvelope
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, newAPIClientError("token_list_invalid_response")
		}
		if !envelope.Success {
			return nil, newAPIClientError("token_list_rejected")
		}
		var page newAPITokenSearchPage
		if err := json.Unmarshal(envelope.Data, &page); err != nil || page.Items == nil ||
			page.Total < len(page.Items) || (page.Page != 0 && page.Page != pageNumber) {
			return nil, newAPIClientError("token_list_invalid_response")
		}
		if page.Total > newAPITokenListPageSize*newAPITokenListMaxPages {
			return nil, newAPIClientError("token_discovery_limit")
		}
		for i := range page.Items {
			token := &page.Items[i]
			if !newAPITokenCouldMatch(token, connection.APIKey) {
				continue
			}
			if token.Key == "" || strings.Contains(token.Key, "*") {
				keyLookups++
				if keyLookups > newAPITokenMaxKeyLookups {
					return nil, newAPIClientError("token_discovery_limit")
				}
			}
			isMatch, err := c.verifyTokenKey(ctx, connection, token)
			if err != nil {
				return nil, err
			}
			if !isMatch {
				continue
			}
			if matched != nil {
				return nil, newAPIClientError("token_search_not_unique")
			}
			matched = token
		}
		seen += len(page.Items)
		if seen >= page.Total {
			if matched == nil {
				return nil, newAPIClientError("token_key_not_matched")
			}
			return matched, nil
		}
		if len(page.Items) == 0 {
			return nil, newAPIClientError("token_list_invalid_response")
		}
	}
	return nil, newAPIClientError("token_discovery_limit")
}

func normalizeNewAPITokenKey(key string) string {
	return strings.TrimPrefix(strings.TrimSpace(key), "sk-")
}

func newAPITokenKeysEqual(left, right string) bool {
	left, right = normalizeNewAPITokenKey(left), normalizeNewAPITokenKey(right)
	return left != "" && !strings.Contains(left, "*") &&
		subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func newAPITokenCouldMatch(token *newAPIToken, key string) bool {
	if token.Kind != "" && token.Kind != "api_key" {
		return false
	}
	masked := normalizeNewAPITokenKey(token.Key)
	if masked == "" {
		return true
	}
	first, last := strings.IndexByte(masked, '*'), strings.LastIndexByte(masked, '*')
	if first < 0 {
		return newAPITokenKeysEqual(masked, key)
	}
	if strings.Trim(masked[first:last+1], "*") != "" {
		return false
	}
	key = normalizeNewAPITokenKey(key)
	return strings.HasPrefix(key, masked[:first]) && strings.HasSuffix(key, masked[last+1:])
}

func (c *NewAPIClient) verifyTokenKey(ctx context.Context, connection NewAPIConnection, token *newAPIToken) (bool, error) {
	if !newAPITokenCouldMatch(token, connection.APIKey) {
		return false, nil
	}
	key := token.Key
	if key == "" || strings.Contains(key, "*") {
		if token.ID <= 0 {
			return false, newAPIClientError("token_key_unavailable")
		}
		// This read operation is POST in forks that hide token keys from lists.
		// Use their key-reveal endpoint; never log or persist its response.
		path := "/api/token/" + strconv.FormatInt(token.ID, 10) + "/key"
		status, body, err := c.request(ctx, http.MethodPost, connection.BaseURL, path, nil, connection.UserAccessToken, connection.UserID)
		if err != nil {
			return false, err
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return false, newAPIHTTPError("token_key", status)
		}
		var data struct {
			Key string `json:"key"`
		}
		if err := decodeNewAPIEnvelope(body, &data); err != nil || data.Key == "" || strings.Contains(data.Key, "*") {
			return false, newAPIClientError("token_key_unavailable")
		}
		key = data.Key
	}
	if !newAPITokenKeysEqual(key, connection.APIKey) {
		return false, nil
	}
	if token.UserID != "" && token.UserID != connection.UserID {
		return false, newAPIClientError("token_user_mismatch")
	}
	token.keyVerified = true
	token.Key = ""
	return true, nil
}
