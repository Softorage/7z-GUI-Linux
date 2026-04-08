#!/usr/bin/env bash

# Move to script's directory
cd "$(dirname "$0")"

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

# Detect Installation Paths
if [ "$EUID" -ne 0 ]; then
    echo "Staging for current user..."
    BASE_PREFIX="$HOME/.local"
    ICON_SUBDIR="share/icons"
    # Internal path inside .desktop file must be absolute for local install
    INTERNAL_BIN_PATH="$BASE_PREFIX/bin/$EXEC"
else
    echo "Staging for system..."
    if [ -d "/usr/local" ]; then P_FIX="/usr/local"; else P_FIX="/usr"; fi
    BASE_PREFIX="$P_FIX"
    ICON_SUBDIR="share/pixmaps"
    # For system installs, the bin dir is in the PATH
    INTERNAL_BIN_PATH="$EXEC"
fi

BIN_DIR="${DESTDIR}${BASE_PREFIX}/bin"
APP_DIR="${DESTDIR}${BASE_PREFIX}/share/applications"
ICON_DIR="${DESTDIR}${BASE_PREFIX}/${ICON_SUBDIR}"

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR"

echo "Installing to: ${DESTDIR:-/}"
install -Dm 755 "$EXEC" "$BIN_DIR/$EXEC"
install -Dm 644 "$ICON" "$ICON_DIR/$ICON"
install -Dm 644 "$NAME.desktop" "$APP_DIR/$NAME.desktop"

# Fix the Exec path inside the .desktop file for local install
if [ "$EUID" -ne 0 ]; then
    sed -i "s|Exec=$EXEC|Exec=$INTERNAL_BIN_PATH|g" "$APP_DIR/$NAME.desktop"
fi

# Get the .desktop on desktop
if [ -d "$DESKTOP_FOLDER" ]; then
    echo "Adding launcher to Desktop..."
    # Copy the already-modified desktop file from APP_DIR to the Desktop
    cp "$APP_DIR/$NAME.desktop" "$DESKTOP_FOLDER/"
    # Desktop files on the actual desktop MUST be executable to be "trusted"
    chmod +x "$DESKTOP_FOLDER/$NAME.desktop"
    # If running as root (via sudo), make sure the user owns the desktop file
    if [ "$EUID" -eq 0 ] && [ -n "$SUDO_USER" ]; then
        chown "$SUDO_USER:" "$DESKTOP_FOLDER/$NAME.desktop"
    fi
fi

echo "Installation complete!"