package service

import "testing"

func TestImageResourceBase(t *testing.T) {
	cases := map[string]string{
		"https://image-service.c3j1.conoha.io":     "https://image-service.c3j1.conoha.io/v2/",
		"https://image-service.c3j1.conoha.io/":    "https://image-service.c3j1.conoha.io/v2/",
		"https://image-service.c3j1.conoha.io/v2":  "https://image-service.c3j1.conoha.io/v2/",
		"https://image-service.c3j1.conoha.io/v2/": "https://image-service.c3j1.conoha.io/v2/",
	}
	for endpoint, want := range cases {
		if got := imageResourceBase(endpoint); got != want {
			t.Errorf("imageResourceBase(%q) = %q, want %q", endpoint, got, want)
		}
	}
}
