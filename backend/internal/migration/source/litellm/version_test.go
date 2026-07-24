package litellm

import "testing"

func TestValidateSupportedVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		wantErr bool
	}{
		{name: "min supported", version: "1.52.0"},
		{name: "mid supported", version: "v1.68.3"},
		{name: "too old", version: "1.51.9", wantErr: true},
		{name: "too new", version: "1.70.0", wantErr: true},
		{name: "invalid", version: "main", wantErr: true},
	}
	for _, test := range tests {
		err := ValidateSupportedVersion(test.version)
		if test.wantErr && err == nil {
			t.Fatalf("%s: expected error", test.name)
		}
		if !test.wantErr && err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
	}
}
