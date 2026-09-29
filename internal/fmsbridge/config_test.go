package fmsbridge

import "testing"

func TestConfigFromLookupKeepsBridgeDisabledWithoutBaseURL(t *testing.T) {
	config, enabled, err := configFromLookup(func(string) string { return "" })
	if err != nil || enabled || config.BaseURL != "" {
		t.Fatalf("config=%#v enabled=%v err=%v", config, enabled, err)
	}
}

func TestConfigFromLookupAcceptsBoundedDeploymentSettings(t *testing.T) {
	values := map[string]string{
		envFMSBaseURL:      "https://fms.example.test/",
		envFMSServiceToken: "service-token",
		envFMSPageSize:     "50",
		envFMSTimeout:      "15s",
	}
	config, enabled, err := configFromLookup(func(key string) string { return values[key] })
	if err != nil || !enabled {
		t.Fatalf("config=%#v enabled=%v err=%v", config, enabled, err)
	}
	if config.BaseURL != "https://fms.example.test" || config.PageSize != 50 || config.Timeout.String() != "15s" {
		t.Fatalf("config=%#v", config)
	}
}

func TestConfigFromLookupRejectsMissingServiceToken(t *testing.T) {
	_, enabled, err := configFromLookup(func(key string) string {
		if key == envFMSBaseURL {
			return "https://fms.example.test"
		}
		return ""
	})
	if err == nil || enabled {
		t.Fatalf("missing token result = enabled:%v err:%v", enabled, err)
	}
}
