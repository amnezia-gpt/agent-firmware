package dockerengine

import "testing"

func TestDockerContextSocketRejectsRemoteOrUnsafeEndpoints(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{`"unix:///tmp/engine socket"`, "/tmp/engine socket"},
		{`"tcp://remote.invalid:2375"`, ""},
		{`"unix://remote.invalid/socket"`, ""},
		{`"unix:///tmp/../engine.sock"`, ""},
		{`"unix:///"`, ""},
		{`"unix:///tmp/socket\nother"`, ""},
	} {
		got, err := parseContextSocket([]byte(test.input))
		if got != test.want || (err == nil) != (test.want != "") {
			t.Errorf("endpoint %s: got %q, error %v", test.input, got, err)
		}
	}
}
