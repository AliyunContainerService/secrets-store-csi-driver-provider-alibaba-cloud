package utils

import (
	"reflect"
	"testing"
)

var (
	arn1 = "acs:kms:cn-hongkong:12345678:secret/test"
	arn2 = "arn:acs:kms:cn-hongkong:12345678:secret/test"
	arn3 = "test"
	arn4 = ""
)

func TestParseARN(t *testing.T) {
	type args struct {
		arn string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{"mock-parse-arn1", args{arn1}, "acs:kms:cn-hongkong:12345678:secret/test", false},
		{"mock-parse-arn1", args{arn2}, "", true},
		{"mock-parse-arn1", args{arn3}, "", true},
		{"mock-parse-arn1", args{arn4}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseARN(tt.args.arn)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseARN() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got.string(), tt.want) {
				t.Errorf("ParseARN() got = %v, want %v", got, tt.want)
			}
		})
	}
}
