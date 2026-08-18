package tools

import (
	"context"
	"testing"
)

// TestMysqlCrudToolSchema locks in parity
// zod schema (lib/ai/tools/schemas.ts mysqlCrudSchema): full descriptions
// (eino truncates jsonschema-tag descriptions at commas when the wrong tag is
// used), all three params required, and operate_type as a real enum.
func TestMysqlCrudToolSchema(t *testing.T) {
	tool, err := NewMysqlCrudTool()
	if err != nil {
		t.Fatalf("NewMysqlCrudTool: %v", err)
	}
	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	js, err := info.ParamsOneOf.ToJSONSchema()
	if err != nil {
		t.Fatalf("ToJSONSchema: %v", err)
	}
	if js.Properties == nil {
		t.Fatal("schema has no properties")
	}

	prop := func(name string) (desc string, enum []any) {
		s, ok := js.Properties.Get(name)
		if !ok {
			t.Fatalf("missing %s property", name)
		}
		return s.Description, s.Enum
	}

	dsnDesc, _ := prop("dsn")
	wantDsnDesc := "MySQL DSN, including username/password/host/port/database name, e.g., root:pass@tcp(host:3306)/db"
	if dsnDesc != wantDsnDesc {
		t.Errorf("dsn description = %q, want %q", dsnDesc, wantDsnDesc)
	}

	sqlDesc, _ := prop("sql")
	if sqlDesc != "SQL statement to execute" {
		t.Errorf("sql description = %q", sqlDesc)
	}

	opDesc, opEnum := prop("operate_type")
	if opDesc != "SQL operation type" {
		t.Errorf("operate_type description = %q", opDesc)
	}
	wantEnum := []any{"query", "insert", "update", "delete"}
	if len(opEnum) != len(wantEnum) {
		t.Fatalf("operate_type enum = %v, want %v", opEnum, wantEnum)
	}
	for i, v := range wantEnum {
		if opEnum[i] != v {
			t.Errorf("operate_type enum[%d] = %v, want %v", i, opEnum[i], v)
		}
	}

	for _, name := range []string{"dsn", "sql", "operate_type"} {
		found := false
		for _, r := range js.Required {
			if r == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s should be required (required=%v)", name, js.Required)
		}
	}
}

// TestNormalizeDsn covers the DSN formats the model may emit, mirroring the
// Next.js normalizeDsn dual-format support plus the parseTime requirement.
func TestNormalizeDsn(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"root:pass@tcp(127.0.0.1:3306)/db", "root:pass@tcp(127.0.0.1:3306)/db?parseTime=true"},
		{"mysql://root:pass@127.0.0.1:3306/db", "root:pass@tcp(127.0.0.1:3306)/db?parseTime=true"},
		{"mysql://root:pass@127.0.0.1:3306/", "root:pass@tcp(127.0.0.1:3306)/?parseTime=true"},
		{"root:pass@tcp(h:3306)/db?parseTime=true", "root:pass@tcp(h:3306)/db?parseTime=true"},
		{"root:pass@tcp(h:3306)/db?timeout=5s", "root:pass@tcp(h:3306)/db?timeout=5s&parseTime=true"},
	}
	for _, c := range cases {
		if got := normalizeDsn(c.in); got != c.want {
			t.Errorf("normalizeDsn(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
