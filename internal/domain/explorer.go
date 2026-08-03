package domain

// Tab indices to avoid magic numbers and simplify reordering
const (
	ExplorerTabRank = 0
	CompressTabRank = 1
	ExtractTabRank  = 2
	ChecksumTabRank = 3
	StatusTabRank   = 4
)

// ConflictAction enumerates user choices when resolving destination file/folder collisions
// during copy, cut, or extract operations.
type ConflictAction int

const (
	ActionCancel ConflictAction = iota
	ActionReplace
	ActionReplaceAll
	ActionRename
	ActionRenameAll
	ActionSkip
	ActionSkipAll
)

// StageItem represents an entry prepared for staging before committing changes into an archive.
type StageItem struct {
	SrcPath string // Absolute path on disk or temp location
	DstName string // Target file or folder name inside the archive
	IsDir   bool
}

// FileSystemItem represents a normalized view of a file, directory, or symlink on disk.
type FileSystemItem struct {
	Name      string
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
	Modified  string
}

// ArchiveItem represents raw entry metadata returned by 7-Zip's list output (-slt).
type ArchiveItem struct {
	Path     string // Relative path within the archive (using forward slashes)
	IsDir    bool
	Size     int64
	Modified string
}

// ArchiveLevel represents a single layer in the nested archive navigation stack.
// When an archive inside an archive is opened, a temporary working directory is created
// and pushed onto the tab's navigation stack.
type ArchiveLevel struct {
	DisplayName     string
	ArchivePath     string // Local path to the extracted target archive
	ArchiveRelPath  string // Current directory level within this archive
	ArchivePassword string
	TempDir         string // Temporary directory managing extracted files for this level
}

// ClipboardItem holds details about items copied or cut in the app's custom clipboard,
// supporting both local files and items situated inside virtual archives.
type ClipboardItem struct {
	Path        string // Full disk path or internal virtual relative path
	IsDir       bool
	Op          string // "cut" or "copy"
	IsArchive   bool   // Whether the source item is inside an archive
	ArchivePath string // Source archive disk path (if IsArchive is true)
	Password    string // Archive password (if IsArchive is true and encrypted)
}

// FavoriteItem represents a user bookmark pointing to a local directory path.
type FavoriteItem struct {
	Name string
	Path string
}

// TypeConflictInfo describes conflicts arising from mismatched filesystem entry types.
type TypeConflictInfo struct {
	Name       string
	SrcPath    string
	DstPath    string
	Resolution string
}