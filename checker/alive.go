package checker

import (
	"net/http"

	"github.com/55gY/subs-check/config"
)

func CheckAlive(httpClient *http.Client) (bool, error) {
	resp, err := httpClient.Get(config.GlobalConfig.AliveTestUrl)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 2xx
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, nil
}
