#!/bin/bash
set -e

echo "Building pako-telegram..."
make build

echo "Installing binary to /usr/local/bin..."
sudo cp bin/pako-telegram /usr/local/bin/pako-telegram

echo "Copying commands to ~/.config/pako-telegram/commands..."
mkdir -p ~/.config/pako-telegram/commands
cp bin/commands/*.yaml ~/.config/pako-telegram/commands/

echo "Restarting launchd service..."
launchctl stop com.pako-telegram 2>/dev/null || true
launchctl start com.pako-telegram

echo "Done! Checking logs..."
sleep 1
tail -5 ~/.config/pako-telegram/stdout.log
