package provider

import (
	"net/http"
	"strings"
	"testing"
)

func TestSetAppAttributionCategories(t *testing.T) {
	const requestCount = 32
	requests := make(chan http.Header, requestCount)
	for range requestCount {
		go func() {
			headers := make(http.Header)
			SetAppAttribution(headers)
			requests <- headers
		}()
	}

	counts := make(map[string]int)
	for range requestCount {
		headers := <-requests
		categories := strings.Split(headers.Get("X-OpenRouter-Categories"), ",")
		if len(categories) != 2 || categories[0] == categories[1] {
			t.Fatalf("categories = %v, want two distinct categories per request", categories)
		}
		for _, category := range categories {
			counts[category]++
		}
	}
	if len(counts) != 4 {
		t.Fatalf("categories = %v, want four categories across requests", counts)
	}
	for _, category := range []string{"programming-app", "personal-agent", "general-chat", "creative-writing"} {
		if counts[category] != requestCount/2 {
			t.Errorf("%s sent %d times, want %d", category, counts[category], requestCount/2)
		}
	}
}
