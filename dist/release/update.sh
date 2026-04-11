#!/usr/bin/env bash

set -e

# Move to script's directory
cd "$(dirname "$0")"

echo "Starting update..."

# Export DESTDIR so sub-scripts see it
export DESTDIR

bash ./uninstall.sh
bash ./install.sh

echo "Update finished!"

# Send notification on successful update
TITLE="Update Complete"
MSG="7z GUI Linux updated successfully."

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
    echo "Notification tool not found, but update is complete."
fi