// Package models defines the application's core domain data structures:
// users, files, and standard HTTP responses.
package models

import "time"

// ─────────────────────────────────────────────
//  User
// ─────────────────────────────────────────────

// User represents a registered cloud storage user.
type User struct {
	// ID is a cryptographically random user identifier.
	ID string `json:"id"`

	// Username is the unique account name used to sign in.
	Username string `json:"username"`

	// PasswordHash is a bcrypt password hash and is never returned to the client.
	PasswordHash string `json:"password_hash"`

	// CreatedAt is the registration date and time.
	CreatedAt time.Time `json:"created_at"`
}

// RegisterRequest is the request body for user registration.
type RegisterRequest struct {
	// Username is the requested account name and must contain at least 3 characters.
	Username string `json:"username"`

	// Password is the plaintext password and must contain at least 8 characters.
	Password string `json:"password"`
}

// LoginRequest is the sign-in request body.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is returned after successful sign-in.
type LoginResponse struct {
	// Token is the JWT used to authorize subsequent requests.
	Token string `json:"token"`

	// ExpiresAt is the token expiration time in UTC.
	ExpiresAt time.Time `json:"expires_at"`
}

// ─────────────────────────────────────────────
//  File
// ─────────────────────────────────────────────

// FileEntry contains metadata for one stored file.
// File bytes are stored on disk; this structure is serialized to JSON.
type FileEntry struct {
	// ID is a cryptographically random file identifier.
	ID string `json:"id"`

	// OwnerID identifies the user who owns the file.
	OwnerID string `json:"owner_id"`

	// IsPublic indicates whether the file is publicly accessible.
	IsPublic bool `json:"is_public"`

	// FolderID identifies the containing folder; "root" is the root folder.
	FolderID string `json:"folder_id"`

	// OriginalName is the original filename submitted by the client.
	OriginalName string `json:"original_name"`

	// Size is the file size in bytes.
	Size int64 `json:"size"`

	// ContentType is the MIME type, for example "image/png".
	ContentType string `json:"content_type"`

	// UploadedAt is the upload date and time in UTC.
	UploadedAt time.Time `json:"uploaded_at"`
}

// FileListResponse is returned when listing a user's files.
type FileListResponse struct {
	// Files contains file metadata without file content.
	Files []FileEntry `json:"files"`

	// Total is the total number of files.
	Total int `json:"total"`
}

// ─────────────────────────────────────────────
//  Folders
// ─────────────────────────────────────────────

// FolderEntry contains stored folder metadata. Folders may contain files and nested folders.
type FolderEntry struct {
	// ID is a cryptographically random folder identifier.
	ID string `json:"id"`

	// OwnerID identifies the user who owns the folder.
	OwnerID string `json:"owner_id"`

	// ParentID identifies the parent folder; "root" is used at the top level.
	ParentID string `json:"parent_id"`

	// Name is the folder name submitted by the client.
	Name string `json:"name"`

	// IsPublic indicates whether the folder is publicly accessible.
	IsPublic bool `json:"is_public"`

	// CreatedAt is the folder creation date and time in UTC.
	CreatedAt time.Time `json:"created_at"`
}

// FolderListResponse is returned when listing a user's folders.
type FolderListResponse struct {
	// Folders contains folder metadata.
	Folders []FolderEntry `json:"folders"`

	// Total is the total number of folders.
	Total int `json:"total"`
}

// ─────────────────────────────────────────────
//  Standard HTTP responses
// ─────────────────────────────────────────────

// ErrorResponse is the standard error response format.
type ErrorResponse struct {
	// Error is a short machine-readable error code.
	Error string `json:"error"`

	// Message is a human-readable explanation.
	Message string `json:"message,omitempty"`
}

// SuccessResponse is the standard success response when no data payload is needed.
type SuccessResponse struct {
	// Message is a human-readable success message.
	Message string `json:"message"`
}
