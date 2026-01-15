package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFile(t *testing.T) {
	t.Run("parses simple key=value", func(t *testing.T) {
		content := `DB_HOST=localhost
DB_PORT=5432
API_KEY=secret123`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["DB_HOST"] != "localhost" {
			t.Errorf("expected DB_HOST=localhost, got %q", result["DB_HOST"])
		}
		if result["DB_PORT"] != "5432" {
			t.Errorf("expected DB_PORT=5432, got %q", result["DB_PORT"])
		}
		if result["API_KEY"] != "secret123" {
			t.Errorf("expected API_KEY=secret123, got %q", result["API_KEY"])
		}
	})

	t.Run("handles quoted values", func(t *testing.T) {
		content := `DOUBLE="hello world"
SINGLE='foo bar'
MIXED=unquoted value`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["DOUBLE"] != "hello world" {
			t.Errorf("expected DOUBLE='hello world', got %q", result["DOUBLE"])
		}
		if result["SINGLE"] != "foo bar" {
			t.Errorf("expected SINGLE='foo bar', got %q", result["SINGLE"])
		}
		if result["MIXED"] != "unquoted value" {
			t.Errorf("expected MIXED='unquoted value', got %q", result["MIXED"])
		}
	})

	t.Run("ignores comments and empty lines", func(t *testing.T) {
		content := `# This is a comment
KEY1=value1

# Another comment
KEY2=value2
`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(result) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result))
		}
		if result["KEY1"] != "value1" {
			t.Errorf("expected KEY1=value1, got %q", result["KEY1"])
		}
	})

	t.Run("handles empty values", func(t *testing.T) {
		content := `EMPTY=
ALSO_EMPTY=""`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["EMPTY"] != "" {
			t.Errorf("expected EMPTY='', got %q", result["EMPTY"])
		}
		if result["ALSO_EMPTY"] != "" {
			t.Errorf("expected ALSO_EMPTY='', got %q", result["ALSO_EMPTY"])
		}
	})

	t.Run("handles values with equals sign", func(t *testing.T) {
		content := `EQUATION=1+1=2
URL=https://example.com?foo=bar`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["EQUATION"] != "1+1=2" {
			t.Errorf("expected EQUATION='1+1=2', got %q", result["EQUATION"])
		}
		if result["URL"] != "https://example.com?foo=bar" {
			t.Errorf("expected full URL, got %q", result["URL"])
		}
	})

	t.Run("trims whitespace", func(t *testing.T) {
		content := `  KEY1  =  value1
KEY2=  value2`

		path := writeTestFile(t, content)
		result, err := LoadEnvFile(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if result["KEY1"] != "value1" {
			t.Errorf("expected KEY1='value1', got %q", result["KEY1"])
		}
		if result["KEY2"] != "value2" {
			t.Errorf("expected KEY2='value2', got %q", result["KEY2"])
		}
	})

	t.Run("rejects invalid key names", func(t *testing.T) {
		content := `1INVALID=value`

		path := writeTestFile(t, content)
		_, err := LoadEnvFile(path)
		if err == nil {
			t.Error("expected error for invalid key starting with number")
		}
	})

	t.Run("rejects lines without equals", func(t *testing.T) {
		content := `KEY1=value1
INVALID_LINE
KEY2=value2`

		path := writeTestFile(t, content)
		_, err := LoadEnvFile(path)
		if err == nil {
			t.Error("expected error for line without equals")
		}
	})
}

func TestIsValidEnvKey(t *testing.T) {
	valid := []string{
		"KEY",
		"MY_KEY",
		"_PRIVATE",
		"KEY1",
		"MY_KEY_2",
		"ABC123",
	}

	for _, k := range valid {
		if !isValidEnvKey(k) {
			t.Errorf("expected %q to be valid", k)
		}
	}

	invalid := []string{
		"",
		"1KEY",      // starts with number
		"MY-KEY",    // contains hyphen
		"MY KEY",    // contains space
		"KEY.NAME",  // contains dot
	}

	for _, k := range invalid {
		if isValidEnvKey(k) {
			t.Errorf("expected %q to be invalid", k)
		}
	}
}

func TestCreateSecret(t *testing.T) {
	data := map[string]string{
		"DB_PASSWORD": "secret123",
		"API_KEY":     "key456",
	}
	labels := map[string]string{
		"app": "myapp",
	}

	secret := CreateSecret("myapp-secrets", "default", data, labels)

	if secret.Name != "myapp-secrets" {
		t.Errorf("expected name myapp-secrets, got %q", secret.Name)
	}
	if secret.Namespace != "default" {
		t.Errorf("expected namespace default, got %q", secret.Namespace)
	}
	if string(secret.Data["DB_PASSWORD"]) != "secret123" {
		t.Errorf("expected DB_PASSWORD=secret123")
	}
	if secret.Labels["app"] != "myapp" {
		t.Errorf("expected app label myapp")
	}
}

func TestRedactSecret(t *testing.T) {
	secret := CreateSecret("test", "default", map[string]string{
		"PASSWORD": "supersecret",
		"API_KEY":  "key123",
	}, nil)

	redacted := RedactSecret(secret)

	if redacted["PASSWORD"] != "***REDACTED***" {
		t.Errorf("expected PASSWORD to be redacted")
	}
	if redacted["API_KEY"] != "***REDACTED***" {
		t.Errorf("expected API_KEY to be redacted")
	}
}

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	return path
}
