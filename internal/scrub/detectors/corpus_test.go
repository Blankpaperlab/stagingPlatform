package detectors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type corpusCase struct {
	Name string       `json:"name"`
	Text string       `json:"text"`
	Want []corpusWant `json:"want"`
}

type corpusWant struct {
	Kind  Kind   `json:"kind"`
	Value string `json:"value"`
}

func TestDefaultLibraryCorpus(t *testing.T) {
	cases := loadCorpus(t)
	library := DefaultLibrary()

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			text := resolveFixtureToken(tc.Text)
			gotMatches := library.Scan(text)
			got := make([]corpusWant, len(gotMatches))
			for idx, match := range gotMatches {
				got[idx] = corpusWant{
					Kind:  match.Kind,
					Value: match.Value,
				}
			}
			want := make([]corpusWant, len(tc.Want))
			for idx, item := range tc.Want {
				want[idx] = corpusWant{
					Kind:  item.Kind,
					Value: resolveFixtureToken(item.Value),
				}
			}

			if !slices.Equal(got, want) {
				t.Fatalf("Scan(%q) = %#v, want %#v", text, got, want)
			}
		})
	}
}

func TestDefaultLibrarySortsMatchesByOffset(t *testing.T) {
	library := DefaultLibrary()
	text := "Email alice@example.com then call +1 (415) 555-2671 and use " + fakeOpenAIProjectKey()

	matches := library.Scan(text)
	if len(matches) != 3 {
		t.Fatalf("Scan() matches = %d, want 3", len(matches))
	}

	if matches[0].Kind != KindEmail || matches[1].Kind != KindPhone || matches[2].Kind != KindAPIKey {
		t.Fatalf("Scan() kinds = %#v, want email, phone, api_key in order", []Kind{matches[0].Kind, matches[1].Kind, matches[2].Kind})
	}

	for idx := 1; idx < len(matches); idx++ {
		if matches[idx-1].Start > matches[idx].Start {
			t.Fatalf("matches out of order: %#v", matches)
		}
	}
}

func TestDefaultLibraryDeduplicatesAPIPrefixOverlaps(t *testing.T) {
	library := DefaultLibrary()
	text := "Token " + fakeStripeKey() + " appears once."

	matches := library.Scan(text)
	if len(matches) != 1 {
		t.Fatalf("Scan() matches = %#v, want 1 api_key match", matches)
	}

	if matches[0].Kind != KindAPIKey {
		t.Fatalf("Scan() kind = %q, want %q", matches[0].Kind, KindAPIKey)
	}
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()

	path := filepath.Join("testdata", "corpus.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	var cases []corpusCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", path, err)
	}

	return cases
}

func resolveFixtureToken(value string) string {
	replacements := map[string]string{
		"__STRIPE_KEY__":         fakeStripeKey(),
		"__OPENAI_PROJECT_KEY__": fakeOpenAIProjectKey(),
		"__GITHUB_TOKEN__":       fakeGitHubToken(),
	}

	resolved := value
	for placeholder, actual := range replacements {
		resolved = strings.ReplaceAll(resolved, placeholder, actual)
	}

	return resolved
}

func fakeStripeKey() string {
	return "sk" + "_live_" + "1234567890abcdef1234567890abcdef"
}

func fakeOpenAIProjectKey() string {
	return "sk" + "-proj-" + "abcdefghijklmnopQRST_uvwx"
}

func fakeGitHubToken() string {
	return "gh" + "p_" + "abcdefghijklmnopqrstuv123456"
}
