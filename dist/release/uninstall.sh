#!/usr/bin/env bash

NAME="7z-GUI-Linux"
EXEC="7z-GUI-Linux"
ICON="7z-GUI-Linux.png"

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

echo "Uninstallation complete!"