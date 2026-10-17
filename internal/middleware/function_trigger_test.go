package middleware

import "testing"

func TestFunctionTriggerAuthenticationExemptionIsNarrow(t *testing.T) {
	valid := "/api/function-triggers/a/12345678-1234-4234-8234-123456789abc"
	if !IsFunctionTriggerPath(valid) {
		t.Fatal("valid trigger rejected")
	}
	for _, path := range []string{valid + "/runs", valid + "/", valid + "/../http-token", "/api/servers/a/functions/12345678-1234-4234-8234-123456789abc", "/api/function-triggers/a/not-uuid", "/api/function-triggers/a%2fb/12345678-1234-4234-8234-123456789abc", "//api/function-triggers/a/12345678-1234-4234-8234-123456789abc"} {
		if IsFunctionTriggerPath(path) {
			t.Fatal("unsafe exemption", path)
		}
	}
}
