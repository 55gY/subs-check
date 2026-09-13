package provider

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeShadowrocketVMess(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("auto:d90e4077-bc7e-414d-a650-70bf1baae4d6@example.com:443"))
	got, err := normalizeShadowrocketVMess("vmess://" + payload + "?remarks=demo")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "vmess://"))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(decoded, &value); err != nil {
		t.Fatal(err)
	}
	if value["add"] != "example.com" || value["id"] != "d90e4077-bc7e-414d-a650-70bf1baae4d6" || value["ps"] != "demo" {
		t.Fatalf("unexpected normalized payload: %#v", value)
	}
}

func TestNormalizeV2RayLinksLeavesOtherProtocolsUntouched(t *testing.T) {
	input := []byte("ss://example\nvless://example")
	if got := string(NormalizeV2RayLinks(input)); got != string(input) {
		t.Fatalf("non-vmess input changed: %q", got)
	}
}
