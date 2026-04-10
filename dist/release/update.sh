#!/usr/bin/env bash

# Move to script's directory
cd "$(dirname "$0")"

echo "Starting update..."

# Export DESTDIR so sub-scripts see it
export DESTDIR

bash ./uninstall.sh
bash ./install.sh

echo "Update finished!"