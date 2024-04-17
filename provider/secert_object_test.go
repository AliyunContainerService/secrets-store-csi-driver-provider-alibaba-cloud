package provider

import "testing"

func TestSecretObject_validateSecretObject(t *testing.T) {
	type fields struct {
		ObjectName         string
		ObjectAlias        string
		ObjectVersion      string
		ObjectVersionLabel string
		JMESPath           []JMESPathObject
		translate          string
		mountDir           string
	}
	f1 := fields{
		ObjectName:  "MySecret",
		ObjectAlias: "test",
	}
	f2 := fields{
		ObjectName:  "MySecret",
		ObjectAlias: "a/../bad",
	}
	f3 := fields{
		ObjectName: "../Bad",
	}
	f4 := fields{
		ObjectName: "test/..",
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		// TODO: Add test cases.
		{"validate-secret-obj-1", f1, false},
		{"validate-secret-obj-2", f2, true},
		{"validate-secret-obj-3", f3, true},
		{"validate-secret-obj-4", f4, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SecretObject{
				ObjectName:         tt.fields.ObjectName,
				ObjectAlias:        tt.fields.ObjectAlias,
				ObjectVersion:      tt.fields.ObjectVersion,
				ObjectVersionLabel: tt.fields.ObjectVersionLabel,
				JMESPath:           tt.fields.JMESPath,
				translate:          tt.fields.translate,
				mountDir:           tt.fields.mountDir,
			}
			if err := s.validateSecretObject(); (err != nil) != tt.wantErr {
				t.Errorf("validateSecretObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
