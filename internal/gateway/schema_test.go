package gateway

import "testing"

// Reminders reads no installation configuration. An empty properties map is
// deliberately not the same as reporting no schema: it says the app described
// itself and has no keys.
func TestConfigurationSchemaIsEmptyObject(t *testing.T) {
	document := ConfigurationSchema()

	if document["type"] != "object" {
		t.Fatalf("root type = %v, want object", document["type"])
	}

	properties, ok := document["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties must be present, so the Console knows the app described itself")
	}
	if len(properties) != 0 {
		t.Fatalf("properties = %v, want empty", properties)
	}
	if _, hasRequired := document["required"]; hasRequired {
		t.Error("an app with no properties must not declare required keys")
	}
}
