#!/usr/bin/env bash

NAME="7z-GUI-Linux"
EXEC="7z-GUI-Linux"
ICON="7z-GUI-Linux.png"

REAL_USER="${SUDO_USER:-$USER}"
USER_HOME=$(getent passwd "$REAL_USER" | cut -d: -f6)

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
rm -f "$APP_DIR/$NAME.desktop"
rm -f "$ICON_DIR/$ICON"

USER_DESKTOP_FILE="$DESKTOP_FOLDER/${NAME}.desktop"

if [ -f "$USER_DESKTOP_FILE" ]; then
  echo "Removing desktop shortcut at $USER_DESKTOP_FILE..."
  rm -f "$USER_DESKTOP_FILE"
fi

echo "Uninstallation complete!"