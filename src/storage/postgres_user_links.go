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
	selectLegacyCloudUserLinksSQL = `SELECT id, username FROM users WHERE username IS NOT NULL AND BTRIM(username) <> ''`
	selectCloudUserIDsSQL         = `SELECT id FROM users`
	dropLegacyCloudIdentitySQL    = `ALTER TABLE users DROP COLUMN IF EXISTS username, DROP COLUMN IF EXISTS password_hash`
)

type userLink struct {
	id       string
	username string
}

// ReconcileUserProfiles makes ID the only value shared by the authentication and cloud user tables.
func ReconcileUserProfiles(ctx context.Context, cloudStore *SQLStore, userStore *UserStore) error {
	legacyLinks, hasLegacyColumns, err := cloudStore.legacyUserLinks(ctx)
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
	if err := userStore.finalizeSchema(ctx); err != nil {
		return err
	}
	if !hasLegacyColumns {
		return nil
	}

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

	if _, err := cloudStore.db.Exec(ctx, dropLegacyCloudIdentitySQL); err != nil {
		return fmt.Errorf("remove legacy cloud identity columns: %w", err)
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
