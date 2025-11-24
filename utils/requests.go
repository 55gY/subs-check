package utils

import (
	"net/url"
	"strings"

	"github.com/55gY/subs-check/config"
)

func WarpUrl(u string) string {
	if strings.HasPrefix(u, "http") && config.GlobalConfig.GithubProxy != "" && strings.Contains(u, "github.com") {
		if gUrl, err := url.Parse(config.GlobalConfig.GithubProxy); err == nil {
			if uUrl, err := url.Parse(u); err == nil {
				uUrl.Scheme = gUrl.Scheme
				uUrl.Host = gUrl.Host
				return uUrl.String()
			}
		}
	}
	return u
}
