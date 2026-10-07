package main

import (
	"reflect"
	"testing"
)

func TestExpectedTunnelResourceIDs(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      []string
		fail      bool
	}{
		{"all entries including inactive", `{"k8s_api_enabled":true,"ports":[{"resource_id":"b","local_port":0},{"resource_id":"a"}]}`, []string{"a", "b", "k8s-api"}, false},
		{"disabled k8s", `{"ports":[{"resource_id":"a"}]}`, []string{"a"}, false},
		{"duplicate", `{"ports":[{"resource_id":"a"},{"resource_id":"a"}]}`, nil, true},
		{"empty", `{"ports":[{}]}`, nil, true},
		{"reserved", `{"ports":[{"resource_id":"k8s-api"}]}`, nil, true},
		{"malformed", `{`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expectedTunnelResourceIDs(tc.raw)
			if (err != nil) != tc.fail || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}
