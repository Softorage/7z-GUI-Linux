package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"github.com/spf13/viper"

	"github.com/Softorage/7z-GUI-Linux/internal/domain"
)

var (
	UserConfig   domain.AppConfig
	UserConfigMu sync.RWMutex
	v            *viper.Viper
)

// GetConfigDir returns the resolved absolute configuration directory for the active user.
func GetConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(configDir, domain.AppDirName)
}

// GetConfigFilePath returns the resolved absolute path to config.yaml.
func GetConfigFilePath() string {
	return filepath.Join(GetConfigDir(), "config.yaml")
}

// InitConfig initializes Viper, locates or creates ~/.config/7z-gui-linux/config.yaml,
// loads saved preferences into memory, and sets up defaults.
func InitConfig() error {
	v = viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	appConfigDir := GetConfigDir()
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	v.AddConfigPath(appConfigDir)
	configFilePath := GetConfigFilePath()

	// Pre-populate UserConfig in memory with default values
	UserConfigMu.Lock()
	UserConfig = domain.DefaultConfig()
	UserConfigMu.Unlock()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok || os.IsNotExist(err) {
			UserConfigMu.Lock()
			UserConfig.Favorites = GetInitialFavorites()
			// Any key missing in config.yaml will remain set to its default value from domain.DefaultConfig()
			v.Set("favorites", UserConfig.Favorites)
			v.Set("version", UserConfig.Version)
			v.Set("compression", UserConfig.Compression)
			v.Set("updates", UserConfig.Updates)
			v.Set("system", UserConfig.System)
			UserConfigMu.Unlock()
			if writeErr := v.WriteConfigAs(configFilePath); writeErr != nil {
				return fmt.Errorf("failed to write initial config file: %w", writeErr)
			}
		} else {
			return fmt.Errorf("error reading config file: %w", err)
		}
	}

	UserConfigMu.Lock()
	if err := v.Unmarshal(&UserConfig); err != nil {
		UserConfigMu.Unlock()
		return fmt.Errorf("unable to decode config: %w", err)
	}

	FavoritesMu.Lock()
	Favorites = UserConfig.Favorites
	if len(Favorites) == 0 {
		Favorites = GetInitialFavorites()
		UserConfig.Favorites = Favorites
		v.Set("favorites", Favorites)
		_ = v.WriteConfig()
	}
	FavoritesMu.Unlock()
	UserConfigMu.Unlock()

	return nil
}

// SaveConfig safely persists in-memory state (UserConfig and Favorites) to config.yaml.
func SaveConfig() error {
	if v == nil {
		return nil
	}
	FavoritesMu.Lock()
	favsCopy := make([]domain.FavoriteItem, len(Favorites))
	copy(favsCopy, Favorites)
	FavoritesMu.Unlock()

	UserConfigMu.Lock()
	UserConfig.Favorites = favsCopy
	v.Set("favorites", UserConfig.Favorites)
	v.Set("compression", UserConfig.Compression)
	v.Set("updates", UserConfig.Updates)
	v.Set("system", UserConfig.System)
	UserConfigMu.Unlock()

	return v.WriteConfig()
}
