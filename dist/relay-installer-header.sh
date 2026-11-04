#!/usr/bin/env bash
# =====================================================================
#  BRAHM Drop Relay  —  VPS installer (self-contained)
#  Brahm InnerBrain Technologies Pvt. Ltd.  |  Developed by Sandeep Kushwah
#
#  Run this on your VPS so devices can connect from anywhere by a code:
#      bash install-brahm-relay.sh             # listens on TCP 53333
#      PORT=9000 bash install-brahm-relay.sh   # custom port
#
#  Then in the BRAHM Drop app (Remote tab -> Internet), set the relay to:
#      <this-server-ip>:53333
# =====================================================================
set -e

PORT="${PORT:-53333}"

if [ "$(id -u)" = "0" ]; then
  BIN=/usr/local/bin/brahmrelay
else
  BIN="$HOME/.local/bin/brahmrelay"
  mkdir -p "$HOME/.local/bin"
fi

echo ">> Installing BRAHM Drop Relay to $BIN (TCP port $PORT)"
LINE=$(awk '/^__BRAHMRELAY_BINARY__$/{print NR+1; exit}' "$0")
tail -n +"$LINE" "$0" | base64 -d > "$BIN"
chmod +x "$BIN"
echo ">> Binary installed ($(du -h "$BIN" | cut -f1))"

has_systemd() { [ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1; }

if has_systemd && [ "$(id -u)" = "0" ]; then
  systemctl stop brahmrelay 2>/dev/null || true
fi
pkill -f "$BIN" 2>/dev/null || true

installed_service=0
if has_systemd && [ "$(id -u)" = "0" ]; then
  cat >/etc/systemd/system/brahmrelay.service <<SERVICE
[Unit]
Description=BRAHM Drop Relay
After=network.target

[Service]
ExecStart=$BIN --port $PORT
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
SERVICE
  systemctl daemon-reload || true
  systemctl enable brahmrelay >/dev/null 2>&1 || true
  if systemctl restart brahmrelay; then
    installed_service=1
    echo ">> Installed as a systemd service (auto-starts on boot)."
  fi
fi

if [ "$installed_service" != "1" ]; then
  if command -v setsid >/dev/null 2>&1; then
    setsid "$BIN" --port "$PORT" </dev/null >/tmp/brahmrelay.log 2>&1 &
  else
    nohup "$BIN" --port "$PORT" </dev/null >/tmp/brahmrelay.log 2>&1 &
  fi
  disown 2>/dev/null || true
  echo ">> Started in the background. Log: /tmp/brahmrelay.log"
fi

IP=$(hostname -I 2>/dev/null | awk '{print $1}'); [ -z "$IP" ] && IP="<server-ip>"
echo ""
echo "============================================================"
echo "  BRAHM Drop Relay is running."
echo "  In the app set the relay server to:   $IP:$PORT"
echo "  (Open TCP port $PORT in your firewall / security group.)"
echo "============================================================"
if [ "$installed_service" = "1" ]; then
  echo "  Manage:  systemctl status|stop|restart brahmrelay"
else
  echo "  Stop:    pkill -f brahmrelay"
fi
echo "============================================================"
exit 0

__BRAHMRELAY_BINARY__
