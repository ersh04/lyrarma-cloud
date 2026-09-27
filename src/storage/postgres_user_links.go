package storage

import (
	"context"
	"fmt"
)

const (
	hasLegacyCloudUsernameSQL = `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
				AND table_name = 'users'
				AND column_name = 'username'
		)
	`
	hasLegacyCloudPlanSQL = `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = current_schema()
				AND table_name = 'users'
				AND column_name = 'plan'
		)
	`
	selectLegacyCloudUserLinksSQL = `SELECT id, username FROM users WHERE username IS NOT NULL AND BTRIM(username) <> ''`
	selectLegacyCloudPlansSQL     = `SELECT id, plan FROM users WHERE plan IS NOT NULL AND BTRIM(plan) <> ''`
	selectCloudUserIDsSQL         = `SELECT id FROM users`
	dropLegacyCloudUserColumnsSQL = `
		ALTER TABLE users
		DROP COLUMN IF EXISTS username,
		DROP COLUMN IF EXISTS password_hash,
		DROP COLUMN IF EXISTS plan
	`
	mergeAuthUserPlanSQL = `
		UPDATE users
		SET plan = CASE
			WHEN plan IS NULL OR cardinality(plan) = 0 THEN ARRAY[$2]::text[]
			WHEN $2 = ANY(plan) THEN plan
			ELSE array_append(plan, $2)
		END
		WHERE id = $1
	`
)

type userLink struct {
	id       string
	username string
}

type userPlan struct {
	id   string
	plan string
}

// ReconcileUserProfiles makes ID the only value shared by the authentication and cloud user tables.
func ReconcileUserProfiles(ctx context.Context, cloudStore *SQLStore, userStore *UserStore) error {
	legacyLinks, hasLegacyIdentity, err := cloudStore.legacyUserLinks(ctx)
	if err != nil {
		return err
	}
	for _, link := range legacyLinks {
		found, err := userStore.setUserID(ctx, link.username, link.id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf(
				"cloud user %q has no account in lyrarma-services-users; migrate the account before removing the legacy username",
				link.username,
			)
		}
	}

	if err := userStore.assignMissingIDs(ctx); err != nil {
		return err
	}

	legacyPlans, hasLegacyPlan, err := cloudStore.legacyUserPlans(ctx)
	if err != nil {
		return err
	}
	for _, assignment := range legacyPlans {
		if err := userStore.validateAssignedPlans([]string{assignment.plan}); err != nil {
			return fmt.Errorf("validate cloud plan for user %q: %w", assignment.id, err)
		}
		found, err := userStore.mergeUserPlan(ctx, assignment.id, assignment.plan)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf(
				"cloud profile %q has no matching account in lyrarma-services-users; migrate the account before moving its plan",
				assignment.id,
			)
		}
	}

	if hasLegacyIdentity || hasLegacyPlan {
		authLinks, err := userStore.userLinks(ctx)
		if err != nil {
			return err
		}
		authIDs := make(map[string]struct{}, len(authLinks))
		for _, link := range authLinks {
			authIDs[link.id] = struct{}{}
		}

		cloudIDs, err := cloudStore.userIDs(ctx)
		if err != nil {
			return err
		}
		for _, id := range cloudIDs {
			if _, exists := authIDs[id]; !exists {
				return fmt.Errorf("cloud profile %q has no matching account in lyrarma-services-users", id)
			}
		}
	}

	if err := userStore.backfillAndValidatePlans(ctx); err != nil {
		return err
	}
	if err := userStore.finalizeSchema(ctx); err != nil {
		return err
	}

	if hasLegacyIdentity || hasLegacyPlan {
		if _, err := cloudStore.db.Exec(ctx, dropLegacyCloudUserColumnsSQL); err != nil {
			return fmt.Errorf("remove legacy cloud user columns: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) legacyUserLinks(ctx context.Context) ([]userLink, bool, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, hasLegacyCloudUsernameSQL).Scan(&exists); err != nil {
		return nil, false, fmt.Errorf("check legacy cloud username column: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	rows, err := s.db.Query(ctx, selectLegacyCloudUserLinksSQL)
	if err != nil {
		return nil, true, fmt.Errorf("read legacy cloud user links: %w", err)
	}
	defer rows.Close()

	var links []userLink
	for rows.Next() {
		var link userLink
		if err := rows.Scan(&link.id, &link.username); err != nil {
			return nil, true, fmt.Errorf("scan legacy cloud user link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("iterate legacy cloud user links: %w", err)
	}
	return links, true, nil
}

func (s *SQLStore) legacyUserPlans(ctx context.Context) ([]userPlan, bool, error) {
	var exists bool
	if err := s.db.QueryRow(ctx, hasLegacyCloudPlanSQL).Scan(&exists); err != nil {
		return nil, false, fmt.Errorf("check legacy cloud plan column: %w", err)
	}
	if !exists {
		return nil, false, nil
	}

	rows, err := s.db.Query(ctx, selectLegacyCloudPlansSQL)
	if err != nil {
		return nil, true, fmt.Errorf("read legacy cloud user plans: %w", err)
	}
	defer rows.Close()

	var plans []userPlan
	for rows.Next() {
		var plan userPlan
		if err := rows.Scan(&plan.id, &plan.plan); err != nil {
			return nil, true, fmt.Errorf("scan legacy cloud user plan: %w", err)
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return nil, true, fmt.Errorf("iterate legacy cloud user plans: %w", err)
	}
	return plans, true, nil
}

func (s *SQLStore) userIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, selectCloudUserIDsSQL)
	if err != nil {
		return nil, fmt.Errorf("read cloud user IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan cloud user ID: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cloud user IDs: %w", err)
	}
	return ids, nil
}

func (s *UserStore) mergeUserPlan(ctx context.Context, id, plan string) (bool, error) {
	result, err := s.db.Exec(ctx, mergeAuthUserPlanSQL, id, plan)
	if err != nil {
		return false, fmt.Errorf("move cloud plan %q for user %q: %w", plan, id, err)
	}
	return result.RowsAffected() > 0, nil
}
