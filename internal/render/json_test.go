package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cdescobedo/trenvy/internal/render"
)

func TestJSONGoldenMatchesTheFrontendShape(t *testing.T) {
	got, err := render.JSON(resolved(map[string]string{
		"auth0ClientID":  "example-client-id",
		"backendAPI":     "/",
		"auth0Audience":  "http://app.example.test/api",
		"auth0Domain":    "dev-demo.us.auth0.test",
		"mapboxAPIToken": "pk.example.token",
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
// configuration the app reads — no additional keys.
func TestJSONCarriesNoBookkeepingKey(t *testing.T) {
	input := map[string]string{"a": "1"}
	got, err := render.JSON(resolved(input))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal(got, &out); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, got)
	}
	// Assert the key set is exactly what went in. Any added key fails.
	if len(out) != len(input) {
		t.Errorf("output has %d keys, input has %d; extra keys added:\n%s", len(out), len(input), got)
	}
	for k := range input {
		if _, ok := out[k]; !ok {
			t.Errorf("input key %q missing from output:\n%s", k, got)
		}
	}
	for k := range out {
		if _, ok := input[k]; !ok {
			t.Errorf("output has unexpected key %q not in input:\n%s", k, got)
		}
	}
}

func TestJSONAcceptsFreeFormNamesDotenvWouldReject(t *testing.T) {
	// A json key need not be a shell identifier.
	if _, err := render.JSON(resolved(map[string]string{"auth0ClientID": "x", "a-b": "y"})); err != nil {
		t.Errorf("JSON rejected a free-form key: %v", err)
	}
}

func TestJSONEscapesCorrectly(t *testing.T) {
	t.Run("standard JSON escapes round trip", func(t *testing.T) {
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
	})

	t.Run("SetEscapeHTML(false) preserves ampersands", func(t *testing.T) {
		// JSON calls SetEscapeHTML(false). Left on, Go's encoder writes an
		// ampersand as the six characters &, so a URL like ?a=1&b=2
		// arrives in the rendered file unreadable.
		got, err := render.JSON(resolved(map[string]string{"url": "?a=1&b=2"}))
		if err != nil {
			t.Fatal(err)
		}
		// Check raw bytes: the literal & must appear, not the escape sequence.
		if !strings.Contains(string(got), "&") {
			t.Errorf("output does not contain literal &:\n%s", got)
		}
		if strings.Contains(string(got), "\\u0026") {
			t.Errorf("output contains HTML-escaped ampersand (SetEscapeHTML(false) not applied):\n%s", got)
		}
	})
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
