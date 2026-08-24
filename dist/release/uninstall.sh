#!/usr/bin/env bash

NAME="7z-GUI-Linux"
EXEC="7z-GUI-Linux"
BIN7Z="7zzs"
ICON="7z-GUI-Linux.png"

APP_DIR_NAME="7z-gui-linux"
FYNE_APP_ID="com.softorage.7gl"
LEGACY_APP_NAME="7-zip-gui"

PURGE_CONFIG=false

# Parse CLI arguments
while [ "$#" -gt 0 ]; do
    case "$1" in
        -p|--purge)
            PURGE_CONFIG=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -p, --purge        Remove binaries, desktop entries, caches, AND all configuration/Fyne storage"
            echo "  -k, --keep-config  Remove binaries and caches but retain user configuration (default)"
            echo "  -h, --help         Display this help message"
            exit 0
            ;;
        *)
            shift
            ;;
    esac
done

REAL_USER="${SUDO_USER:-$USER}"
USER_HOME=$(getent passwd "$REAL_USER" | cut -d: -f6)
[ -z "$USER_HOME" ] && USER_HOME="$HOME"

# Determine Desktop folder (handling internationalization)
if [ "$EUID" -eq 0 ] && [ -n "$SUDO_USER" ]; then
    DESKTOP_FOLDER=$(sudo -u "$SUDO_USER" xdg-user-dir DESKTOP 2>/dev/null)
else
    DESKTOP_FOLDER=$(xdg-user-dir DESKTOP 2>/dev/null)
fi
# Fallback
if [ -z "$DESKTOP_FOLDER" ]; then
    DESKTOP_FOLDER="$USER_HOME/Desktop"
fi

# Determine if systems install or local
if [ "$EUID" -ne 0 ]; then
    BASE_PREFIX="$HOME/.local"
    ICON_SUBDIR="share/icons"
else
    if [ -d "/usr/local" ]; then P_FIX="/usr/local"; else P_FIX="/usr"; fi
    BASE_PREFIX="$P_FIX"
    ICON_SUBDIR="share/pixmaps"
fi

BIN_DIR="${DESTDIR}${BASE_PREFIX}/bin"
APP_DIR="${DESTDIR}${BASE_PREFIX}/share/applications"
ICON_DIR="${DESTDIR}${BASE_PREFIX}/${ICON_SUBDIR}"

echo "Removing files from: ${DESTDIR:-/}"
rm -f "$BIN_DIR/$EXEC"
rm -f "$BIN_DIR/$BIN7Z"
rm -f "$APP_DIR/$NAME.desktop"
rm -f "$ICON_DIR/$ICON"

USER_DESKTOP_FILE="$DESKTOP_FOLDER/${NAME}.desktop"

if [ -f "$USER_DESKTOP_FILE" ]; then
  echo "Removing desktop shortcut at $USER_DESKTOP_FILE..."
  rm -f "$USER_DESKTOP_FILE"
fi

# Unconditionally purge disk cache and orphan temporary staging files
echo "Cleaning application cache and temporary workspaces..."
USER_CACHE_DIR="${XDG_CACHE_HOME:-$USER_HOME/.cache}"
rm -rf "$USER_CACHE_DIR/$APP_DIR_NAME"
rm -rf "$USER_CACHE_DIR/$LEGACY_APP_NAME"
rm -rf "$USER_CACHE_DIR/fyne/$FYNE_APP_ID"
rm -rf "$USER_CACHE_DIR/$FYNE_APP_ID"

if [ "$EUID" -eq 0 ] && [ -d "/root/.cache" ]; then
    rm -rf "/root/.cache/$APP_DIR_NAME" "/root/.cache/$LEGACY_APP_NAME" "/root/.cache/fyne/$FYNE_APP_ID" "/root/.cache/$FYNE_APP_ID"
fi

rm -rf /tmp/7gl-* /dev/shm/7gl-* "/dev/shm/$APP_DIR_NAME" 2>/dev/null || true

# Handle user preferences, Fyne metadata, and local data removal
USER_CONFIG_DIR="${XDG_CONFIG_HOME:-$USER_HOME/.config}"
USER_DATA_DIR="${XDG_DATA_HOME:-$USER_HOME/.local/share}"

if [ "$PURGE_CONFIG" = true ]; then
    echo "Purging configuration files, Fyne preferences, and local data..."
    rm -rf "$USER_CONFIG_DIR/$APP_DIR_NAME"
    rm -rf "$USER_CONFIG_DIR/fyne/$FYNE_APP_ID"
    rm -rf "$USER_CONFIG_DIR/$FYNE_APP_ID"
    rm -rf "$USER_DATA_DIR/$FYNE_APP_ID"

    if [ "$EUID" -eq 0 ] && [ -d "/root" ]; then
        rm -rf "/root/.config/$APP_DIR_NAME"
        rm -rf "/root/.config/fyne/$FYNE_APP_ID"
        rm -rf "/root/.config/$FYNE_APP_ID"
        rm -rf "/root/.local/share/$FYNE_APP_ID"
    fi
else
    if [ -d "$USER_CONFIG_DIR/$APP_DIR_NAME" ]; then
        echo "Preserved configuration at: $USER_CONFIG_DIR/$APP_DIR_NAME (use --purge to remove)"
    fi
fi

echo "Uninstallation complete!"

# Send notification on successful uninstall
TITLE="Uninstall Complete"
MSG="7z GUI Linux uninstalled successfully."

if command -v notify-send &> /dev/null; then
    # Linux
    if [ "$EUID" -eq 0 ] && [ -n "$SUDO_USER" ]; then
        # If run via sudo, find the original user's DBUS session to send the notification
        USER_UID=$(id -u "$SUDO_USER")
        sudo -u "$SUDO_USER" DISPLAY=:0 DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$USER_UID/bus notify-send "$TITLE" "$MSG" -i emblem-default
    else
        # Run as normal user
        notify-send "$TITLE" "$MSG" -i emblem-default
    fi
else
    echo "Notification tool not found, but uninstall is complete."
fi
