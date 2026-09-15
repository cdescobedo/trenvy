package sample_test

import (
	"reflect"
	"testing"

	"github.com/cdescobedo/trenvy/internal/sample"
)

func TestParseDotenv(t *testing.T) {
	src := `
# Application environment. Copy to .env.

APP_DB_CONNECTION="Server=localhost;Port=8675;Password=password;"

APP_ENVIRONMENT=Development
export APP_URL=http://+:8080

#APP_TLS_CERT_PATH=/https/app-tls.pfx
ALPHA_TOKEN=your-secret-key-here-replace-with-actual-secret
`
	got, err := sample.Parse(".env_example", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := []string{
		"APP_DB_CONNECTION",
		"APP_ENVIRONMENT",
		"APP_URL",
		"ALPHA_TOKEN",
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v (commented lines must be skipped)", names, want)
	}

	if got[0].Value != "Server=localhost;Port=8675;Password=password;" {
		t.Errorf("quoted value = %q", got[0].Value)
	}
	if got[2].Value != "http://+:8080" {
		t.Errorf("exported value = %q", got[2].Value)
	}
}

func TestParseJSON(t *testing.T) {
	src := `{
  "auth0ClientID": "demoClientId00000000000000000000",
  "backendAPI": "/",
  "mapboxAPIToken": "pk.eyJ1IjoiZGVtbyJ9.sig"
}`
	got, err := sample.Parse("env.json.example", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3", len(got))
	}
	// Sorted, so the result is stable regardless of the file's key order.
	if got[0].Name != "auth0ClientID" {
		t.Errorf("first name = %q", got[0].Name)
	}
}

func TestParseRejectsNestedJSON(t *testing.T) {
	_, err := sample.Parse("c.json", []byte(`{"a": {"b": "c"}}`))
	if err == nil {
		t.Fatal("Parse accepted nested JSON")
	}
}

// The sample file itself encodes which variables are secret: real values are
// spelled out, secrets are placeholders. That is a far better signal than the
// variable name alone.
func TestGuessSecretFromPlaceholderText(t *testing.T) {
	secretish := map[string]string{
		"ALPHA_TOKEN":        "your-secret-key-here-replace-with-actual-secret",
		"BETA_TOKEN": "changeme",
		"SOME_VALUE":             "<your value here>",
		"ANOTHER":                "xxxxx",
		"THIRD":                  "TODO",
	}
	for name, value := range secretish {
		t.Run(name, func(t *testing.T) {
			if !sample.GuessSecret(name, value) {
				t.Errorf("GuessSecret(%q, %q) = false, want true", name, value)
			}
		})
	}
}

func TestGuessSecretFromNameSuffix(t *testing.T) {
	for _, name := range []string{"X_KEY", "X_SECRET", "X_TOKEN", "X_PASSWORD", "MY_APIKEY"} {
		t.Run(name, func(t *testing.T) {
			if !sample.GuessSecret(name, "") {
				t.Errorf("GuessSecret(%q, \"\") = false, want true", name)
			}
		})
	}
}

func TestGuessSecretFalseForRealLookingValues(t *testing.T) {
	plain := map[string]string{
		"APP_ENVIRONMENT":     "Development",
		"APP_URL":            "http://+:8080",
		"APP_DB_CONNECTION": "Server=localhost;Port=8675;Password=password;",
		"backendAPI":                 "/",
		"auth0Domain":                "dev-example.us.auth0.com",
	}
	for name, value := range plain {
		t.Run(name, func(t *testing.T) {
			if sample.GuessSecret(name, value) {
				t.Errorf("GuessSecret(%q, %q) = true, want false", name, value)
			}
		})
	}
}

// These markers only mean "placeholder" when they describe the entire value.
// As bare substrings they collide with real data, and the consequence of a
// false positive here is worse than a mislabel: a flagged variable has its
// value discarded on import, so a false positive silently loses real data.
func TestGuessSecretDoesNotMatchTheseMarkersAsSubstrings(t *testing.T) {
	cases := map[string]struct {
		name, value string
	}{
		"open angle bracket with no close": {"MAX_TEMP", "<100"},
		"todo inside a longer word":        {"TASK_SERVICE_URL", "https://todoist.com/api"},
		"xxxx inside a longer identifier":  {"TRACKING_ID", "GA-xxxx1234"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			if sample.GuessSecret(c.name, c.value) {
				t.Errorf("GuessSecret(%q, %q) = true, want false", c.name, c.value)
			}
		})
	}
}
