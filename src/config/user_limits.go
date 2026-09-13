package config

import (
	"fmt"
	"sort"
)

const (
	multipartOverheadBytes int64 = 1 << 20
	maxPlanNameBytes             = 32
)

// ParsePlanName validates and returns a canonical service plan name.
func ParsePlanName(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("service plan name cannot be empty")
	}
	if len(value) > maxPlanNameBytes {
		return "", fmt.Errorf("service plan name %q exceeds %d bytes", value, maxPlanNameBytes)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if index == 0 {
			if character < 'a' || character > 'z' {
				return "", fmt.Errorf("service plan name %q must start with a lowercase ASCII letter", value)
			}
			continue
		}
		isLetter := character >= 'a' && character <= 'z'
		isNumber := character >= '0' && character <= '9'
		if !isLetter && !isNumber && character != '-' && character != '_' {
			return "", fmt.Errorf("service plan name %q contains an unsupported character", value)
		}
	}
	return value, nil
}

// UserLimits contains validated limits for one service plan.
type UserLimits struct {
	maxStorageBytes int64
	maxFileBytes    int64
}

// NewUserLimits creates validated limits for one service plan.
func NewUserLimits(maxStorageBytes, maxFileBytes int64) (UserLimits, error) {
	if maxStorageBytes <= 0 {
		return UserLimits{}, fmt.Errorf("storage limit must be positive")
	}
	if maxFileBytes <= 0 {
		return UserLimits{}, fmt.Errorf("file limit must be positive")
	}
	if maxFileBytes > maxStorageBytes {
		return UserLimits{}, fmt.Errorf("file limit cannot exceed storage limit")
	}
	return UserLimits{maxStorageBytes: maxStorageBytes, maxFileBytes: maxFileBytes}, nil
}

func (l UserLimits) MaxStorageBytes() int64 { return l.maxStorageBytes }
func (l UserLimits) MaxFileBytes() int64    { return l.maxFileBytes }

// AllowsFile reports whether a file size fits the service plan limit.
func (l UserLimits) AllowsFile(size int64) bool {
	return size >= 0 && size <= l.maxFileBytes
}

// UserLimitSet stores immutable system limits and a dynamic service plan catalog.
type UserLimitSet struct {
	hardMaxStorageBytes int64
	hardMaxFileBytes    int64
	defaultPlan         string
	plans               map[string]UserLimits
}

// NewUserLimitSet creates a validated immutable limit set.
func NewUserLimitSet(
	hardMaxStorageBytes int64,
	hardMaxFileBytes int64,
	defaultPlan string,
	plans map[string]UserLimits,
) (UserLimitSet, error) {
	if hardMaxStorageBytes <= 0 || hardMaxFileBytes <= 0 {
		return UserLimitSet{}, fmt.Errorf("system limits must be positive")
	}
	if hardMaxFileBytes > hardMaxStorageBytes {
		return UserLimitSet{}, fmt.Errorf("system file limit cannot exceed storage limit")
	}
	maxInt := int64(^uint(0) >> 1)
	if hardMaxFileBytes > maxInt-multipartOverheadBytes {
		return UserLimitSet{}, fmt.Errorf("system file limit is too large for the HTTP server")
	}

	defaultPlan, err := ParsePlanName(defaultPlan)
	if err != nil {
		return UserLimitSet{}, fmt.Errorf("invalid default service plan: %w", err)
	}
	if len(plans) == 0 {
		return UserLimitSet{}, fmt.Errorf("at least one service plan must be configured")
	}

	copiedPlans := make(map[string]UserLimits, len(plans))
	for rawName, limits := range plans {
		name, err := ParsePlanName(rawName)
		if err != nil {
			return UserLimitSet{}, err
		}
		if limits.maxStorageBytes <= 0 || limits.maxFileBytes <= 0 {
			return UserLimitSet{}, fmt.Errorf("limits for service plan %q are not configured", name)
		}
		if limits.maxStorageBytes > hardMaxStorageBytes {
			return UserLimitSet{}, fmt.Errorf("storage limit for service plan %q exceeds the system limit", name)
		}
		if limits.maxFileBytes > hardMaxFileBytes {
			return UserLimitSet{}, fmt.Errorf("file limit for service plan %q exceeds the system limit", name)
		}
		copiedPlans[name] = limits
	}
	if _, exists := copiedPlans[defaultPlan]; !exists {
		return UserLimitSet{}, fmt.Errorf("default service plan %q is not configured", defaultPlan)
	}

	return UserLimitSet{
		hardMaxStorageBytes: hardMaxStorageBytes,
		hardMaxFileBytes:    hardMaxFileBytes,
		defaultPlan:         defaultPlan,
		plans:               copiedPlans,
	}, nil
}

// For returns an independent copy of the requested service plan limits.
func (s UserLimitSet) For(plan string) (UserLimits, error) {
	plan, err := ParsePlanName(plan)
	if err != nil {
		return UserLimits{}, err
	}
	limits, exists := s.plans[plan]
	if !exists {
		return UserLimits{}, fmt.Errorf("service plan %q is not configured", plan)
	}
	return limits, nil
}

// DefaultPlan returns the plan assigned to newly registered users.
func (s UserLimitSet) DefaultPlan() string {
	return s.defaultPlan
}

// PlanNames returns a sorted independent copy of configured plan names.
func (s UserLimitSet) PlanNames() []string {
	names := make([]string, 0, len(s.plans))
	for name := range s.plans {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (s UserLimitSet) HardMaxStorageBytes() int64 { return s.hardMaxStorageBytes }
func (s UserLimitSet) HardMaxFileBytes() int64    { return s.hardMaxFileBytes }

// RequestBodyLimit returns a safe Fiber multipart request limit.
func (s UserLimitSet) RequestBodyLimit() int {
	return int(s.hardMaxFileBytes + multipartOverheadBytes)
}
