package ewaybill

import "testing"

// customClient is any non-stub implementation; embedding the interface keeps
// the fixture tiny (only the concrete type matters to IsMockClient).
type customClient struct{ Client }

func TestIsMockClient(t *testing.T) {
	cases := []struct {
		name string
		c    Client
		want bool
	}{
		{"nil", nil, true},
		{"stub via factory", NewClient(Config{Enabled: true, UseMock: true}), true},
		{"custom real client", &customClient{}, false},
		{"real http client", &realHttpClient{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsMockClient(tc.c); got != tc.want {
				t.Fatalf("IsMockClient() = %v, want %v", got, tc.want)
			}
		})
	}
}
