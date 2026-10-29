#!/usr/bin/env bash
# First-time setup of BrainTrainingWeb on this PC.
#
# It builds the program, creates the HTTPS certificate, asks for a password and
# installs a service that starts at boot. Only two steps use sudo, and both say
# so first: mkcert adding its certificate authority to this PC (skipped when it
# is already there), and the optional firewall rule.
#
# Running it again is safe. It keeps the existing certificate and password,
# rebuilds the program and restarts the service (which logs every device out).
set -euo pipefail

cd "$(dirname "$(readlink -f "$0")")"
repo=$PWD
cfg=$HOME/.config/braintraining
port=8444

say() { printf '\n== %s\n' "$*"; }
die() { printf 'Error: %s\n' "$*" >&2; exit 1; }

# Requirements. Nothing is installed for you.
for tool in go mkcert; do
  command -v "$tool" >/dev/null || die "'$tool' is not installed. On Debian or Ubuntu: sudo apt install golang-go mkcert libnss3-tools"
done
command -v systemctl >/dev/null || die "systemd is needed to run BrainTrainingWeb as a service"

say "Building"
go build -trimpath -ldflags="-s -w" -o braintraining

mkdir -p "$cfg"
chmod 700 "$cfg"

# The address other devices use to reach this PC. It goes into the certificate.
ip=$(ip -4 route get 1.1.1.1 2>/dev/null | sed -n 's/.* src \([0-9.]*\).*/\1/p' || true)
[ -n "$ip" ] || read -rp "LAN address of this PC (for example 192.168.1.10): " ip

if [ -e "$cfg/cert.pem" ]; then
  say "Keeping the existing certificate (delete $cfg/cert.pem to make a new one)"
else
  say "Creating the HTTPS certificate (mkcert may ask for your sudo password)"
  # The same certificate authority as the other apps on this PC (HomeStream),
  # so devices that already trust it need nothing new.
  mkcert -install
  mkcert -cert-file "$cfg/cert.pem" -key-file "$cfg/key.pem" "$ip" "$(hostname).local" localhost
fi

if [ -e "$cfg/env" ]; then
  say "Keeping the existing password in $cfg/env"
else
  say "Password"
  # systemd reads this file, and quotes or backslashes would change the value.
  while :; do
    read -rsp "Choose a password: " pw; echo
    read -rsp "Repeat it: " pw2; echo
    case $pw in
      '' | *[\"\'\\]* | ' '* | *' ') echo "Not usable: empty, or it has a quote, a backslash or a space at either end." ;;
      "$pw2") break ;;
      *) echo "The two entries differ." ;;
    esac
  done
  (umask 077; printf 'BT_PASSWORD=%s\n' "$pw" > "$cfg/env")
fi

# The service file in this folder assumes ~/Projects/BrainTrainingWeb. Install
# a copy that points at wherever this folder really is.
say "Installing and starting the service"
mkdir -p "$HOME/.config/systemd/user"
sed "s|^ExecStart=.*|ExecStart=\"$repo/braintraining\"|" braintraining.service > "$HOME/.config/systemd/user/braintraining.service"
systemctl --user daemon-reload
systemctl --user enable braintraining
systemctl --user restart braintraining
loginctl enable-linger "$(id -un)" || echo "Could not turn on start-at-boot. Run: sudo loginctl enable-linger $(id -un)"
sleep 1
systemctl --user is-active --quiet braintraining || die "the service did not start. See: journalctl --user -u braintraining -n 20"

# Firewall: let the home network in, and nothing else.
net=$(ip -4 route show | awk -v ip="$ip" '$1 ~ "/" && $0 ~ "src " ip "( |$)" {print $1; exit}')
if command -v ufw >/dev/null && [ -n "$net" ]; then
  say "Firewall"
  read -rp "Allow $net to reach port $port through ufw now? This uses sudo. [y/N] " yn
  if [[ $yn == [yY]* ]]; then
    sudo ufw allow from "$net" to any port "$port" proto tcp
  fi
else
  echo "Firewall: if this PC has one, allow your home network to reach TCP port $port."
fi

cat <<EOF

BrainTrainingWeb is running.

  Open    https://$(hostname).local:$port  or  https://$ip:$port
  Config  $cfg

Each device has to trust the certificate once. If it already trusts
HomeStream, it is done. Otherwise copy this file to it and follow
"Trust the certificate on your devices" in README.md. Never copy the key file
that sits next to it.

  $(mkcert -CAROOT)/rootCA.pem

Do not forward port $port on your router.
EOF
