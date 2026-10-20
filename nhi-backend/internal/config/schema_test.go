package config

import "testing"

func TestValidateSchemaName(t *testing.T) {
	valid := []string{"tenant_a", "nhi_global", "_x", "Tenant1"}
	for _, name := range valid {
		if err := ValidateSchemaName(name); err != nil {
			t.Errorf("ValidateSchemaName(%q) = %v, want nil", name, err)
		}
	}
	invalid := []string{"", " ", "1abc", "tenant-a", `a"; DROP SCHEMA x; --`, "a.b", "a b", "tenant_a;", string(make([]byte, 70))}
	for _, name := range invalid {
		if err := ValidateSchemaName(name); err == nil {
			t.Errorf("ValidateSchemaName(%q) = nil, want error", name)
		}
	}
}

func TestValidateRequiresDatabasePassword(t *testing.T) {
	c := &Config{
		Server:   ServerConfig{Host: "0.0.0.0", Port: 8080},
		Database: DatabaseConfig{Host: "h", DatabaseName: "nhi_db", User: "u", Schema: "nhi_global"},
		JWT:      JWTConfig{Secret: "0123456789012345678901234567890123", AccessTTLMinutes: 60, RefreshTTLHours: 168, SessionMaxHours: 720},
	}
	if err := c.validate(); err == nil {
		t.Fatal("validate() = nil with empty database password, want error")
	}
	c.Database.Password = "pw"
	if err := c.validate(); err != nil {
		t.Fatalf("validate() = %v, want nil", err)
	}
}
