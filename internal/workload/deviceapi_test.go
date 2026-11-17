package workload

import "testing"

func TestDeviceAPIProfilePreservesInstallationAndLimitsGatewayAccess(t *testing.T) {
	stack := testStack(t)
	if stack.Installation != "718b4bcec873094b25c27059bbf48cae" {
		t.Fatal("namespace changed before manifest cutover")
	}
	if stack.Specs[0].Env["FIRMWARE_DEVICE_API_MODE"] != "required" || len(stack.Specs[0].Bindings) != 2 {
		t.Fatal("Gateway API setup missing")
	}
	if len(stack.Specs[1].Bindings) != 0 || len(stack.Specs[2].Bindings) != 0 {
		t.Fatal("API exposed outside Gateway")
	}
	for _, binding := range stack.Specs[0].Bindings {
		if !binding.ReadOnly {
			t.Fatal("API mount writable")
		}
	}
}
