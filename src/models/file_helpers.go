package models

// FileStatus describes the current file state
type FileStatus string

const (
	FileUploading FileStatus = "uploading"
	FileScanning  FileStatus = "scanning"
	FileApproved  FileStatus = "approved"
	FileBlocked   FileStatus = "blocked"
	FileFailed    FileStatus = "scan_failed"
)

// Valid reports whether the file status can be stored
func (status FileStatus) Valid() bool {
	switch status {
	case FileUploading, FileScanning, FileApproved, FileBlocked, FileFailed:
		return true
	default:
		return false
	}
}
