package config

import (
	"slices"
	"testing"
)

func TestUserLimitSetReturnsIndependentValues(t *testing.T) {
	basic, err := NewUserLimits(10_000, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	enterprise, err := NewUserLimits(30_000, 3_000)
	if err != nil {
		t.Fatal(err)
	}
	inputPlans := map[string]UserLimits{
		"basic":         basic,
		"enterprise-v2": enterprise,
	}

	limitSet, err := NewUserLimitSet(30_000, 3_000, "basic", inputPlans)
	if err != nil {
		t.Fatal(err)
	}
	inputPlans["basic"] = enterprise
	delete(inputPlans, "enterprise-v2")

	first, err := limitSet.For("basic")
	if err != nil {
		t.Fatal(err)
	}
	first.maxFileBytes = 1

	second, err := limitSet.For("basic")
	if err != nil {
		t.Fatal(err)
	}
	if second.MaxFileBytes() != 1_000 {
		t.Fatalf("mutable limit leaked into configuration: got %d", second.MaxFileBytes())
	}
	if !slices.Equal(limitSet.PlanNames(), []string{"basic", "enterprise-v2"}) {
		t.Fatalf("unexpected plan names: %v", limitSet.PlanNames())
	}
}

func TestUserLimitSetAcceptsArbitraryPlanNames(t *testing.T) {
	limits, err := NewUserLimits(10_000, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	limitSet, err := NewUserLimitSet(
		10_000,
		1_000,
		"partner-2026",
		map[string]UserLimits{"partner-2026": limits},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limitSet.For("partner-2026"); err != nil {
		t.Fatal(err)
	}
}

func TestUserLimitSetCombinesAssignedPlans(t *testing.T) {
	storagePlan, err := NewUserLimits(30_000, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	uploadPlan, err := NewUserLimits(10_000, 3_000)
	if err != nil {
		t.Fatal(err)
	}
	limitSet, err := NewUserLimitSet(
		30_000,
		3_000,
		"storage",
		map[string]UserLimits{"storage": storagePlan, "upload": uploadPlan},
	)
	if err != nil {
		t.Fatal(err)
	}

	combined, err := limitSet.ForPlans([]string{"storage", "upload", "storage"})
	if err != nil {
		t.Fatal(err)
	}
	if combined.MaxStorageBytes() != 30_000 || combined.MaxFileBytes() != 3_000 {
		t.Fatalf(
			"unexpected combined limits: storage=%d file=%d",
			combined.MaxStorageBytes(),
			combined.MaxFileBytes(),
		)
	}
}

func TestUserLimitSetRejectsEmptyPlanList(t *testing.T) {
	limits, err := NewUserLimits(10_000, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	limitSet, err := NewUserLimitSet(
		10_000,
		1_000,
		"basic",
		map[string]UserLimits{"basic": limits},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limitSet.ForPlans(nil); err == nil {
		t.Fatal("expected an empty plan list error")
	}
}

func TestUserLimitSetRejectsPlanAboveHardLimit(t *testing.T) {
	limits, err := NewUserLimits(10_000, 2_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewUserLimitSet(
		10_000,
		1_000,
		"basic",
		map[string]UserLimits{"basic": limits},
	); err == nil {
		t.Fatal("expected a validation error")
	}
}

func TestUserLimitSetRejectsMissingDefaultPlan(t *testing.T) {
	limits, err := NewUserLimits(10_000, 1_000)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewUserLimitSet(
		10_000,
		1_000,
		"missing",
		map[string]UserLimits{"basic": limits},
	); err == nil {
		t.Fatal("expected a missing default plan error")
	}
}

func TestParsePlanNameRejectsUnsafeValues(t *testing.T) {
	for _, value := range []string{"", "Admin", "has space", "../owner", "_private"} {
		if _, err := ParsePlanName(value); err == nil {
			t.Fatalf("expected plan name %q to be rejected", value)
		}
	}
}
