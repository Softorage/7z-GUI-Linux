package domain

// TODO: allow user to configure % of RAM to be used when working with nested archives. to be done after implementing a config file to store user preferences.
// TODO: allow disabling update check (after config file is there)

// Canonical identifier and folder constants for XDG Base Directory resolution
const (
	AppID            = "com.softorage.7gl"
	AppDirName       = "7z-gui-linux"
	LegacyAppDirName = "7-zip-gui"
	TmpfsDefaultDir  = "/dev/shm/" + AppDirName
)

// CompressionConfig holds default options for compression operations.
type CompressionConfig struct {
	DefaultFormat          string `mapstructure:"default_format" json:"default_format" yaml:"default_format"`
	DefaultLevel           string    `mapstructure:"default_level" json:"default_level" yaml:"default_level"`
	DefaultMethod          string `mapstructure:"default_method" json:"default_method" yaml:"default_method"`
	DictionarySize         string `mapstructure:"dictionary_size" json:"dictionary_size" yaml:"dictionary_size"`
	WordSize               string `mapstructure:"word_size" json:"word_size" yaml:"word_size"`
	SolidBlockSize         string `mapstructure:"solid_block_size" json:"solid_block_size" yaml:"solid_block_size"`
	MultithreadingThreads  int    `mapstructure:"multithreading_threads" json:"multithreading_threads" yaml:"multithreading_threads"`
	EnableEncryptionHeader bool   `mapstructure:"enable_encryption_header" json:"enable_encryption_header" yaml:"enable_encryption_header"`
}

// UpdateConfig holds preferences regarding update notifications and checks.
type UpdateConfig struct {
	CheckOnStartup     bool     `mapstructure:"check_on_startup" json:"check_on_startup" yaml:"check_on_startup"`
	IncludePrereleases bool     `mapstructure:"include_prereleases" json:"include_prereleases" yaml:"include_prereleases"`
	LastCheckTimestamp int64    `mapstructure:"last_check_timestamp" json:"last_check_timestamp" yaml:"last_check_timestamp"`
	IgnoredVersions    []string `mapstructure:"ignored_versions" json:"ignored_versions" yaml:"ignored_versions"`
}

// SystemConfig manages RAM limits, clipboard behavior, and temporary workspace staging.
type SystemConfig struct {
	RAMUsagePercent         int    `mapstructure:"ram_usage_percent" json:"ram_usage_percent" yaml:"ram_usage_percent"`
	RAMLimitMB              int64  `mapstructure:"ram_limit_mb" json:"ram_limit_mb" yaml:"ram_limit_mb"`
	TmpfsPath               string `mapstructure:"tmpfs_path" json:"tmpfs_path" yaml:"tmpfs_path"`
	AutoCleanupTempSec      int    `mapstructure:"auto_cleanup_temp_sec" json:"auto_cleanup_temp_sec" yaml:"auto_cleanup_temp_sec"`
	ClipboardClearOnSuccess bool   `mapstructure:"clipboard_clear_on_success" json:"clipboard_clear_on_success" yaml:"clipboard_clear_on_success"`
}

// AppConfig holds all application user configuration preferences for 7GL.
type AppConfig struct {
	Version     int               `mapstructure:"version" json:"version" yaml:"version"`
	Favorites   []FavoriteItem    `mapstructure:"favorites" json:"favorites" yaml:"favorites"`
	Compression CompressionConfig `mapstructure:"compression" json:"compression" yaml:"compression"`
	Updates     UpdateConfig      `mapstructure:"updates" json:"updates" yaml:"updates"`
	System      SystemConfig      `mapstructure:"system" json:"system" yaml:"system"`
}

// DefaultConfig returns safe, optimal initial configuration settings.
func DefaultConfig() AppConfig {
	return AppConfig{
		Version:   1,
		Favorites: []FavoriteItem{},
		Compression: CompressionConfig{
			DefaultFormat:          "7z",
			DefaultLevel:           "Normal",
			DefaultMethod:          "LZMA2",
			DictionarySize:         "32 MB",
			WordSize:               "64",
			SolidBlockSize:         "64 MB",
			MultithreadingThreads:  0,
			EnableEncryptionHeader: true,
		},
		Updates: UpdateConfig{
			CheckOnStartup:     true,
			LastCheckTimestamp: 0,
			IgnoredVersions:    []string{},
		},
		System: SystemConfig{
			RAMUsagePercent:         49,
			RAMLimitMB:              8192,
			TmpfsPath:               TmpfsDefaultDir,
			AutoCleanupTempSec:      3600,
			ClipboardClearOnSuccess: true,
		},
	}
}
