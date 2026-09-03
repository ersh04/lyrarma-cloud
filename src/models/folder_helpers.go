package models

import "strings"

const RootFolderID = "root"

// NormalizeFolderID returns the root folder identifier for an empty value.
func NormalizeFolderID(folderID string) string {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return RootFolderID
	}
	return folderID
}
