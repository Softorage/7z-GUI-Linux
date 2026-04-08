#!/usr/bin/env bash

# Move to script's directory
cd "$(dirname "$0")"

NAME="7z-GUI-Linux"
EXEC="7z-GUI-Linux"
ICON="7z-GUI-Linux.png"

# Detect paths
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

echo "Installation complete!"