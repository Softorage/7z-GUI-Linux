package domain

// TODO: allow user to configure % of RAM to be used when working with nested archives. to be done after implementing a config file to store user preferences.
// TODO: allow disabling update check (after config file is there)

// AppConfig holds user configuration preferences for 7GL.
type AppConfig struct {
	// AutoCheckUpdates determines if the app automatically checks GitHub for updates at launch.
	AutoCheckUpdates bool `json:"auto_check_updates"`

	// RAMUsagePercent specifies the maximum percentage of available system RAM used for temp staging (e.g. 49).
	RAMUsagePercent int `json:"ram_usage_percent"`

	// ClipboardClearOnSuccess determines whether clipboard entries are cleared upon successful paste.
	ClipboardClearOnSuccess bool `json:"clipboard_clear_on_success"`
}

// DefaultConfig returns the default configuration settings.
func DefaultConfig() AppConfig {
	return AppConfig{
		AutoCheckUpdates:        true,
		RAMUsagePercent:         49,
		ClipboardClearOnSuccess: true,
	}
}