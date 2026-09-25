package triage

import (
	"slices"
	"testing"
)

func TestTestPathsGenLangs(t *testing.T) {
	for p, want := range map[string]bool{
		"spec/shop/orders_spec.rb": true, "lib/shop/orders.rb": false, "app/src/test/kotlin/A.kt": true,
		"Tests/OrdersTests/ATests.swift": true, "test/order_test.dart": true, "tests/Unit/ATest.php": true,
		"src/order_test.cc": true, "src/order.cc": false, "src/Orders.Tests.ps1": true,
	} {
		if got := isTestPath(p); got != want {
			t.Errorf("isTestPath(%s) = %v", p, got)
		}
	}
	for p, want := range map[string]string{
		"lib/shop/orders.rb":                 "spec/shop/orders_spec.rb",
		"app/src/main/kotlin/acme/Svc.kt":    "app/src/test/kotlin/acme/SvcTest.kt",
		"Sources/Orders/Svc.swift":           "Tests/OrdersTests/SvcTests.swift",
		"lib/src/svc.dart":                   "test/src/svc_test.dart",
		"src/Orders/Svc.php":                 "tests/Orders/SvcTest.php",
		"src/store/repo.cc":                  "src/store/repo_test.cc",
		"src/Orders.psm1":                    "src/Orders.Tests.ps1",
		"core/src/main/scala/acme/Svc.scala": "core/src/test/scala/acme/SvcSpec.scala",
	} {
		if got := testCandidates(p); !slices.Contains(got, want) {
			t.Errorf("testCandidates(%s) = %v, want %s among them", p, got, want)
		}
	}
}
