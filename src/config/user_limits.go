package config

import (
	"fmt"
	"sort"
)

const (
	multipartOverheadBytes int64 = 1 << 20
	maxPlanNameBytes             = 32
)

// ParsePlanName validates and returns a canonical service plan name
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

// UserLimits contains validated limits for one service plan
type UserLimits struct {
	maxStorageBytes int64
	maxFileBytes    int64
}

// NewUserLimits creates validated limits for one service plan
func NewUserLimits(maxStorageBytes, maxFileBytes int64) (UserLimits, error) {
	if maxStorageBytes <= 0 && maxStorageBytes != -1 {
		return UserLimits{}, fmt.Errorf("storage limit must be positive or -1")
	}
	if maxFileBytes <= 0 && maxFileBytes != -1 {
		return UserLimits{}, fmt.Errorf("file limit must be positive or -1")
	}
	if maxStorageBytes != -1 && maxFileBytes > maxStorageBytes {
		return UserLimits{}, fmt.Errorf("file limit cannot exceed storage limit")
	}
	return UserLimits{maxStorageBytes: maxStorageBytes, maxFileBytes: maxFileBytes}, nil
}

// MaxStorageBytes возвращает квоту хранилища в байтах или -1 при отсутствии ограничения
func (l UserLimits) MaxStorageBytes() int64 {
	return l.maxStorageBytes
}

// MaxFileBytes возвращает максимальный размер файла в байтах или -1 при отсутствии ограничения
func (l UserLimits) MaxFileBytes() int64 {
	return l.maxFileBytes
}

// AllowsFile reports whether a file size fits the service plan limit
func (l UserLimits) AllowsFile(size int64) bool {
	return size >= 0 && (l.maxFileBytes == -1 || size <= l.maxFileBytes)
}

// UserLimitSet stores immutable system limits and a dynamic service plan catalog
type UserLimitSet struct {
	hardMaxStorageBytes int64
	hardMaxFileBytes    int64
	defaultPlan         string
	plans               map[string]UserLimits
}

// NewUserLimitSet creates a validated immutable limit set
func NewUserLimitSet(
	hardMaxStorageBytes int64,
	hardMaxFileBytes int64,
	defaultPlan string,
	plans map[string]UserLimits,
) (UserLimitSet, error) {
	if (hardMaxStorageBytes <= 0 && hardMaxStorageBytes != -1) || (hardMaxFileBytes <= 0 && hardMaxFileBytes != -1) {
		return UserLimitSet{}, fmt.Errorf("system limits must be positive or -1")
	}
	if hardMaxStorageBytes != -1 && hardMaxFileBytes > hardMaxStorageBytes {
		return UserLimitSet{}, fmt.Errorf("system file limit cannot exceed storage limit")
	}
	maxInt := int64(^uint(0) >> 1)
	if hardMaxFileBytes > maxInt-multipartOverheadBytes-1 {
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
		if (limits.maxStorageBytes <= 0 && limits.maxStorageBytes != -1) || (limits.maxFileBytes <= 0 && limits.maxFileBytes != -1) {
			return UserLimitSet{}, fmt.Errorf("limits for service plan %q are not configured", name)
		}
		if hardMaxStorageBytes != -1 && limits.maxStorageBytes > hardMaxStorageBytes {
			return UserLimitSet{}, fmt.Errorf("storage limit for service plan %q exceeds the system limit", name)
		}
		if hardMaxFileBytes != -1 && limits.maxFileBytes > hardMaxFileBytes {
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

// For returns an independent copy of the requested service plan limits
func (s UserLimitSet) For(plan string) (UserLimits, error) {
	plan, err := ParsePlanName(plan)
	if err != nil {
		return UserLimits{}, err
	}
	limits, exists := s.plans[plan]
	if !exists {
		return UserLimits{}, fmt.Errorf("service plan %q is not configured", plan)
	}
	return s.effectiveLimits(limits), nil
}

// ForPlans combines assigned plans by taking the highest value for each limit
func (s UserLimitSet) ForPlans(plans []string) (UserLimits, error) {
	if len(plans) == 0 {
		return UserLimits{}, fmt.Errorf("at least one service plan must be assigned")
	}

	var combined UserLimits
	seen := make(map[string]struct{}, len(plans))
	for _, plan := range plans {
		if _, exists := seen[plan]; exists {
			continue
		}
		seen[plan] = struct{}{}

		name, err := ParsePlanName(plan)
		if err != nil {
			return UserLimits{}, err
		}
		limits, exists := s.plans[name]
		if !exists {
			return UserLimits{}, fmt.Errorf("service plan %q is not configured", name)
		}
		if limits.maxStorageBytes == -1 || (combined.maxStorageBytes != -1 && limits.maxStorageBytes > combined.maxStorageBytes) {
			combined.maxStorageBytes = limits.maxStorageBytes
		}
		if limits.maxFileBytes == -1 || (combined.maxFileBytes != -1 && limits.maxFileBytes > combined.maxFileBytes) {
			combined.maxFileBytes = limits.maxFileBytes
		}
	}
	return s.effectiveLimits(combined), nil
}

func (s UserLimitSet) effectiveLimits(limits UserLimits) UserLimits {
	limits.maxStorageBytes = minimumLimit(limits.maxStorageBytes, s.hardMaxStorageBytes)
	limits.maxFileBytes = minimumLimit(limits.maxFileBytes, s.hardMaxFileBytes)
	limits.maxFileBytes = minimumLimit(limits.maxFileBytes, limits.maxStorageBytes)
	return limits
}

func minimumLimit(first, second int64) int64 {
	if first == -1 {
		return second
	}
	if second == -1 || first < second {
		return first
	}
	return second
}

// DefaultPlan returns the plan assigned to newly registered users
func (s UserLimitSet) DefaultPlan() string {
	return s.defaultPlan
}

// PlanNames returns a sorted independent copy of configured plan names
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

// RequestBodyLimit возвращает безопасный лимит тела multipart-запроса для Fiber
func (s UserLimitSet) RequestBodyLimit() int {
	if s.hardMaxFileBytes == -1 {
		// Оставляем запас для дополнительного байта при проверке размера multipart-запроса
		return int(^uint(0)>>1) - 1
	}
	return int(s.hardMaxFileBytes + multipartOverheadBytes)
}
