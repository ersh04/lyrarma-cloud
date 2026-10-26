package config

import (
	"math"
	"slices"
	"testing"
)

func TestUnlimitedPlansCombineBeforeSystemCaps(t *testing.T) {
	plans := map[string]UserLimits{
		"storage": {maxStorageBytes: -1, maxFileBytes: 10},
		"upload":  {maxStorageBytes: 100, maxFileBytes: -1},
		"basic":   {maxStorageBytes: 100, maxFileBytes: 10},
	}
	for _, test := range []struct {
		name       string
		storage    int64
		file       int64
		wantSingle int64
	}{
		{name: "unlimited", storage: -1, file: -1, wantSingle: 100},
		{name: "system capped", storage: 1000, file: 200, wantSingle: 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			set, err := NewUserLimitSet(test.storage, test.file, "basic", plans)
			if err != nil {
				t.Fatal(err)
			}
			for _, assigned := range [][]string{
				{"basic", "storage", "upload"}, {"upload", "storage", "basic"}, {"storage", "upload", "storage"},
			} {
				limits, err := set.ForPlans(assigned)
				if err != nil || limits.MaxStorageBytes() != test.storage || limits.MaxFileBytes() != test.file {
					t.Fatalf("ForPlans(%v) = %+v, %v", assigned, limits, err)
				}
			}
			single, err := set.For("upload")
			if err != nil || single.MaxFileBytes() != test.wantSingle {
				t.Fatalf("unexpected single plan: %+v, %v", single, err)
			}
			basic, err := set.ForPlans([]string{"basic"})
			if err != nil || basic.MaxStorageBytes() != 100 || basic.MaxFileBytes() != 10 {
				t.Fatalf("unlimited system must not override finite plan: %+v, %v", basic, err)
			}
		})
	}
}

func TestAllowsFileWithUnlimitedLimit(t *testing.T) {
	unlimited, err := NewUserLimits(-1, -1)
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{0, 1, math.MaxInt64} {
		if !unlimited.AllowsFile(size) {
			t.Errorf("unlimited plan rejected size %d", size)
		}
	}
	if unlimited.AllowsFile(-1) {
		t.Error("negative file size accepted")
	}
	finite, _ := NewUserLimits(100, 10)
	if !finite.AllowsFile(10) || finite.AllowsFile(11) {
		t.Error("finite file limit not enforced")
	}
}

func TestUnlimitedRequestBodyLimit(t *testing.T) {
	set, err := NewUserLimitSet(-1, -1, "user", map[string]UserLimits{"user": {maxStorageBytes: -1, maxFileBytes: -1}})
	if err != nil {
		t.Fatal(err)
	}
	if got := set.RequestBodyLimit(); got != int(^uint(0)>>1)-1 {
		t.Fatalf("unexpected unlimited HTTP body limit: %d", got)
	}
	if set.HardMaxFileBytes() != -1 || set.HardMaxStorageBytes() != -1 {
		t.Fatal("unlimited system limits must retain their sentinel")
	}
}

func TestUserLimitsValidation(t *testing.T) {
	for _, test := range []struct {
		name      string
		storage   int64
		file      int64
		wantError bool
	}{
		{name: "finite", storage: 100, file: 10},
		{name: "unlimited storage", storage: -1, file: 10},
		{name: "unlimited file", storage: 100, file: -1},
		{name: "unlimited both", storage: -1, file: -1},
		{name: "zero storage", file: 10, wantError: true},
		{name: "negative storage", storage: -2, file: 10, wantError: true},
		{name: "zero file", storage: 100, wantError: true},
		{name: "negative file", storage: 100, file: -2, wantError: true},
		{name: "file exceeds storage", storage: 100, file: 101, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewUserLimits(test.storage, test.file)
			if (err != nil) != test.wantError {
				t.Fatalf("NewUserLimits(%d, %d): %v; want error=%t", test.storage, test.file, err, test.wantError)
			}
		})
	}
}

func TestUserLimitSetRejectsInvalidUnlimitedValues(t *testing.T) {
	for _, invalid := range []int64{0, -2} {
		for _, limits := range []UserLimits{
			{maxStorageBytes: invalid, maxFileBytes: -1},
			{maxStorageBytes: -1, maxFileBytes: invalid},
		} {
			if _, err := NewUserLimitSet(-1, -1, "user", map[string]UserLimits{"user": limits}); err == nil {
				t.Fatalf("expected invalid limits %+v to be rejected", limits)
			}
		}
	}
}

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
