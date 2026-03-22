package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"log/slog"

	"github.com/55gY/subs-check/config"
	"github.com/metacubex/mihomo/common/convert"
)

func doRequestAndReadBody(httpClient *http.Client, req *http.Request) (*http.Response, []byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		if closeErr != nil {
			return nil, nil, closeErr
		}
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}

	return resp, body, nil
}

func newGetRequestWithUserAgent(ctx context.Context, url, userAgent string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	return req, nil
}

func parseCFTraceResponse(bodyText string) (loc string, ip string) {
	for _, line := range strings.Split(bodyText, "\n") {
		if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
		if strings.HasPrefix(line, "ip=") {
			ip = strings.TrimPrefix(line, "ip=")
		}
	}

	return loc, ip
}

func debugRequestError(err error) {
	slog.Debug("创建请求失败", "error", err)
}

func debugNonOKStatus(providerName string, statusCode int) {
	slog.Debug(providerName+"返回非200状态码", "statusCode", statusCode)
}

func debugFetchError(providerName string, err error) {
	slog.Debug(providerName+"获取节点位置失败", "error", err)
}

func debugUnmarshalError(providerName string, err error) {
	slog.Debug("解析"+providerName+" JSON 失败", "error", err)
}

func hasOKStatusCode(resp *http.Response) bool {
	return resp.StatusCode == http.StatusOK
}

func GetProxyCountry(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	for i := 0; i < config.GlobalConfig.SubUrlsReTry; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}
		loc, ip, fraudScore = GetIPPure(ctx, httpClient)
		if loc != "" && ip != "" {
			return
		}
		loc, ip, _ = GetMe(ctx, httpClient)
		if loc != "" && ip != "" {
			return
		}
		loc, ip, _ = GetIPLark(ctx, httpClient)
		if loc != "" && ip != "" {
			return
		}
		loc, ip, _ = GetCFProxy(ctx, httpClient)
		if loc != "" && ip != "" {
			return
		}
		// 不准
		loc, ip, _ = GetEdgeOneProxy(ctx, httpClient)
		if loc != "" && ip != "" {
			return
		}
	}
	return
}

func GetIPPure(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	type IPPureResponse struct {
		IP          string `json:"ip"`
		CountryCode string `json:"countryCode"`
		FraudScore  int    `json:"fraudScore"`
	}

	url := "https://my.ippure.com/v1/info"
	req, err := newGetRequestWithUserAgent(ctx, url, convert.RandUserAgent())
	if err != nil {
		debugRequestError(err)
		return
	}
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		debugFetchError("ippure", err)
		return
	}

	if !hasOKStatusCode(resp) {
		debugNonOKStatus("ippure", resp.StatusCode)
		return
	}

	var data IPPureResponse
	if err := json.Unmarshal(body, &data); err != nil {
		debugUnmarshalError("ippure", err)
		return
	}

	return data.CountryCode, data.IP, data.FraudScore
}

func GetEdgeOneProxy(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	type GeoResponse struct {
		Eo struct {
			Geo struct {
				CountryCodeAlpha2 string `json:"countryCodeAlpha2"`
			} `json:"geo"`
			ClientIp string `json:"clientIp"`
		} `json:"eo"`
	}

	url := "https://functions-geolocation.edgeone.app/geo"
	req, err := newGetRequestWithUserAgent(ctx, url, convert.RandUserAgent())
	if err != nil {
		debugRequestError(err)
		return
	}
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		debugFetchError("edgeone", err)
		return
	}

	if !hasOKStatusCode(resp) {
		debugNonOKStatus("edgeone", resp.StatusCode)
		return
	}

	var eo GeoResponse
	if err := json.Unmarshal(body, &eo); err != nil {
		debugUnmarshalError("edgeone", err)
		return
	}

	return eo.Eo.Geo.CountryCodeAlpha2, eo.Eo.ClientIp, fraudScore
}

func GetCFProxy(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	url := "https://www.cloudflare.com/cdn-cgi/trace"
	req, err := newGetRequestWithUserAgent(ctx, url, convert.RandUserAgent())
	if err != nil {
		debugRequestError(err)
		return
	}
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		debugFetchError("cf", err)
		return
	}

	if !hasOKStatusCode(resp) {
		debugNonOKStatus("cf", resp.StatusCode)
		return
	}
	loc, ip = parseCFTraceResponse(string(body))
	return loc, ip, fraudScore
}

func GetIPLark(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := string([]byte{104, 116, 116, 112, 115, 58, 47, 47, 102, 51, 98, 99, 97, 48, 101, 50, 56, 101, 54, 98, 46, 97, 97, 112, 113, 46, 110, 101, 116, 47, 105, 112, 97, 112, 105, 47, 105, 112, 99, 97, 116})
	req, err := newGetRequestWithUserAgent(ctx, url, "curl/8.7.1")
	if err != nil {
		debugRequestError(err)
		return
	}
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		debugFetchError("iplark", err)
		return
	}

	if !hasOKStatusCode(resp) {
		debugNonOKStatus("iplark", resp.StatusCode)
		return
	}

	var geo GeoIPData
	if err := json.Unmarshal(body, &geo); err != nil {
		debugUnmarshalError("iplark", err)
		return
	}

	return geo.Country, geo.IP, fraudScore
}

func GetMe(ctx context.Context, httpClient *http.Client) (loc string, ip string, fraudScore int) {
	type GeoIPData struct {
		IP      string `json:"ip"`
		Country string `json:"country_code"`
	}

	url := "https://ip.122911.xyz/api/ipinfo"
	req, err := newGetRequestWithUserAgent(ctx, url, "subs-check (https://github.com/55gY/subs-check)")
	if err != nil {
		debugRequestError(err)
		return
	}
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		debugFetchError("me", err)
		return
	}

	if !hasOKStatusCode(resp) {
		debugNonOKStatus("me", resp.StatusCode)
		return
	}

	var geo GeoIPData
	if err := json.Unmarshal(body, &geo); err != nil {
		debugUnmarshalError("me", err)
		return
	}

	return geo.Country, geo.IP, fraudScore
}
