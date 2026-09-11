package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cdescobedo/envctl/internal/render"
)

func TestJSONGoldenMatchesTheFrontendShape(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{
		"auth0ClientID":  "demoClientId00000000000000000000",
		"backendAPI":     "/",
		"auth0Audience":  "http://app.example.test/api",
		"auth0Domain":    "dev-example.us.auth0.com",
		"mapboxAPIToken": "pk.demo.token.00000000000000000000000",
	}))
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	golden(t, "frontend.json", got)
}

func TestJSONIsValidAndFlat(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{"a": "1", "b": "2"}))
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, got)
	}
	for k, v := range out {
		if _, ok := v.(string); !ok {
			t.Errorf("key %q has non-string value %T; output must be flat", k, v)
		}
	}
}

// public/env.json is served to the browser, so it must carry nothing but the
// configuration the app reads — no envctl bookkeeping key.
func TestJSONCarriesNoBookkeepingKey(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{"a": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "envctl") {
		t.Errorf("output mentions envctl; the file is served to browsers:\n%s", got)
	}
}

func TestJSONAcceptsFreeFormNamesDotenvWouldReject(t *testing.T) {
	// A json key need not be a shell identifier.
	if _, err := render.JSON(resolved(map[string]string{"auth0ClientID": "x", "a-b": "y"})); err != nil {
		t.Errorf("JSON rejected a free-form key: %v", err)
	}
}

func TestJSONEscapesCorrectly(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{"k": "a\"b\\c\nd"}))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	if out["k"] != "a\"b\\c\nd" {
		t.Errorf("round trip = %q", out["k"])
	}
}

func TestJSONEndsWithNewline(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{"a": "1"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "\n") {
		t.Error("output does not end with a newline")
	}
}
