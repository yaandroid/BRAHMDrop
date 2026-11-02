#!/usr/bin/env bash
# =====================================================================
#  BRAHM Drop  v1.1  —  Linux / VPS installer  (self-contained)
#  Brahm InnerBrain Technologies Pvt. Ltd.  |  Developed by Sandeep Kushwah
#
#  The app binary is embedded inside this script. Just run it:
#      bash install-brahmdrop.sh              # installs on port 53330
#      PORT=8080 bash install-brahmdrop.sh    # custom port
#
#  On a VPS it is installed as a background service (systemd when available)
#  and served on a PORT (no subdomain). Open:  http://<server-ip>:<port>
# =====================================================================
set -e

PORT="${PORT:-53330}"
NAME="${DEVICE_NAME:-$(hostname)}"

if [ "$(id -u)" = "0" ]; then
  BIN=/usr/local/bin/brahmdrop
  DATA=/var/lib/brahmdrop
else
  BIN="$HOME/.local/bin/brahmdrop"
  DATA="$HOME/.local/share/brahmdrop"
  mkdir -p "$HOME/.local/bin"
fi
mkdir -p "$DATA"

echo ">> Installing BRAHM Drop to $BIN (port $PORT)"

# ---- extract the embedded binary ----
LINE=$(awk '/^__BRAHMDROP_BINARY__$/{print NR+1; exit}' "$0")
tail -n +"$LINE" "$0" | base64 -d > "$BIN"
chmod +x "$BIN"
echo ">> Binary installed ($(du -h "$BIN" | cut -f1))"

# systemd is only usable if it is actually the init system (not just installed).
has_systemd() { [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; }

# ---- stop any old copy ----
if has_systemd && [ "$(id -u)" = "0" ]; then
  systemctl stop brahmdrop 2>/dev/null || true
fi
pkill -f "$BIN" 2>/dev/null || true

start_nohup() {
  # Fully detach (new session) so it keeps running after you log out of SSH.
  if command -v setsid >/dev/null 2>&1; then
    setsid "$BIN" --no-browser --port "$PORT" --name "$NAME" --dir "$DATA/received" \
        </dev/null >"$DATA/brahmdrop.log" 2>&1 &
  else
    nohup "$BIN" --no-browser --port "$PORT" --name "$NAME" --dir "$DATA/received" \
        </dev/null >"$DATA/brahmdrop.log" 2>&1 &
  fi
  disown 2>/dev/null || true
  sleep 1
  echo ">> Started in the background. Log: $DATA/brahmdrop.log"
}

installed_service=0
if has_systemd && [ "$(id -u)" = "0" ]; then
  cat >/etc/systemd/system/brahmdrop.service <<SERVICE
[Unit]
Description=BRAHM Drop file transfer
After=network.target

[Service]
ExecStart=$BIN --no-browser --port $PORT --name $NAME --dir $DATA/received
Restart=always
RestartSec=3
Environment=HOME=$DATA

[Install]
WantedBy=multi-user.target
SERVICE
  systemctl daemon-reload || true
  systemctl enable brahmdrop >/dev/null 2>&1 || true
  if systemctl restart brahmdrop; then
    installed_service=1
    echo ">> Installed as a systemd service (auto-starts on boot)."
  else
    echo ">> systemd unavailable — starting directly instead."
    start_nohup
  fi
else
  start_nohup
fi

# ---- show access info ----
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -z "$IP" ] && IP="<server-ip>"
echo ""
echo "============================================================"
echo "  BRAHM Drop is running."
echo "  Open in a browser:   http://$IP:$PORT"
echo "  Received files:      $DATA/received"
echo "============================================================"
if [ "$installed_service" = "1" ]; then
  echo "  Manage:  systemctl status|stop|restart brahmdrop"
else
  echo "  Stop:    pkill -f brahmdrop        Log: $DATA/brahmdrop.log"
fi
echo "============================================================"
exit 0

__BRAHMDROP_BINARY__
