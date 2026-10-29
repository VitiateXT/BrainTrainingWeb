# BrainTrainingWeb

The three mini-games of the [BrainTraining](https://github.com/VitiateXT/BrainTraining)
Android app as a small web app for the home network: one password, HTTPS,
reachable from any browser on the LAN.

- **Calculator Mode**: arithmetic up to 1000 (+, −, ×, exact ÷).
- **Remembering Mode**: five digits fade in one after another; type them back
  in order.
- **Colour Mode**: a Stroop test. Tap the ink colour, not the written word.

Each mode counts a score and a streak, as in the app. Nothing is saved: scores
start at zero each time a mode is opened.

It works on phones (upright and sideways), tablets, laptops and desktops, in
light and dark mode, and a whole round fits on the screen without scrolling.
With a keyboard, Enter answers or starts the next round, the keys 1–4 pick a
colour (buttons counted left to right, top to bottom), and Escape goes back to
the start page.

## Setup

Requirements: Linux with systemd, Go 1.22 or newer, and
[mkcert](https://github.com/FiloSottile/mkcert). On Debian or Ubuntu:
`sudo apt install golang-go mkcert libnss3-tools`.

```bash
./setup.sh
```

The script builds the program, creates the HTTPS certificate with mkcert, asks
for a password and installs a user service that starts at boot. It offers to
open port 8444 in ufw for your home network, then prints the address to open,
`https://<hostname>.local:8444`. Only the mkcert and ufw steps use sudo.

Running it again is safe: it keeps the certificate and password, rebuilds the
program and restarts the service.

## Trust the certificate on your devices

Browsers warn until a device trusts your mkcert authority. A device that
already trusts it, for example for [HomeStream](https://github.com/VitiateXT/HomeStream),
needs nothing more. Otherwise copy `rootCA.pem` from the folder printed by
`mkcert -CAROOT` to the device and install it as a CA certificate. Never copy
`rootCA-key.pem`.

- Android: Settings > Security > Encryption & credentials > Install a certificate > CA certificate.
- iPhone and iPad: open the file and install the profile, then turn it on under
  Settings > General > About > Certificate Trust Settings. If iOS will not open
  a `.pem` file, rename it to `rootCA.crt`.
- Firefox: Settings > Privacy & Security > Certificates > View Certificates >
  Authorities > Import.

## Configuration

Environment variables. The service reads them from `~/.config/braintraining/env`,
one `NAME=value` per line.

| Variable      | Meaning                 | Default                            |
|---------------|-------------------------|------------------------------------|
| `BT_PASSWORD` | Login password          | required                           |
| `BT_ADDR`     | Listen address and port | `:8444`                            |
| `BT_CERT`     | TLS certificate         | `~/.config/braintraining/cert.pem` |
| `BT_KEY`      | TLS private key         | `~/.config/braintraining/key.pem`  |

To change the password, edit the env file and run
`systemctl --user restart braintraining`.

## Updating

The web pages are compiled into the program, so any change, including one to
`index.html`, needs a rebuild and a restart:

```bash
git pull
./setup.sh        # or: go build -trimpath -ldflags="-s -w" -o braintraining && systemctl --user restart braintraining
```

A restart logs every device out.

## How it works

One Go program with no dependencies beyond the standard library. It serves
three routes over HTTPS:

| Route          | Does                                                           |
|----------------|----------------------------------------------------------------|
| `GET /`        | The games (`index.html`) with a valid login, else `login.html` |
| `POST /login`  | Checks the password, sets the session cookie                   |
| `POST /logout` | Deletes the session cookie                                     |

The games run entirely in the browser; the server only guards the page. The
open screen lives in the URL hash (`#/calc`, `#/mem`, `#/colour`), so the
browser's back button returns to the start page.

## Security

The same design as HomeStream:

- LAN only, in three layers: the firewall rule, a check in the server that
  refuses clients without a private (LAN) address, and no port forwarding on
  the router. Do not forward port 8444.
- Everything goes over TLS. Without a valid login the server sends only the
  login page.
- One shared password. A wrong one is delayed by one second, attempts are
  handled one at a time, and each is logged with the device's address
  (`journalctl --user -u braintraining`).
- Login and sign-out only work from the app's own page. Requests that the
  browser marks as coming from another website or another web app on the same
  PC are refused.
- A login lasts 30 days in a signed cookie named `bt`. Restarting the server
  logs everyone out.
- The password sits in plain text in the env file, which only you can read.

## Development

```bash
go vet ./... && go test ./...                        # server tests
BT_PASSWORD=test ./braintraining                     # run without the service (stop it first, or set BT_ADDR)
```

| File                    | Purpose                                                  |
|-------------------------|----------------------------------------------------------|
| `main.go`               | The whole server: routes, login, sessions, LAN check     |
| `index.html`            | The games: markup, CSS and JavaScript in one file        |
| `login.html`            | The password form                                        |
| `main_test.go`          | Tests for login, sessions, LAN and cross-site requests   |
| `setup.sh`              | First-time setup and updates                             |
| `braintraining.service` | systemd user service, installed by `setup.sh`            |

Where to change things in `index.html`:

- Task ranges: `calcTask()`, a port of `GameGenerators.kt` from the Android app.
- Digit count and timing of Remembering Mode: the `#memstart` click handler.
- Colours: the CSS variables at the top (from `BrainTrainingTheme.kt`) and
  `INK` for Colour Mode.
- Titles and hints of the modes: `MODES`.

## License

GNU General Public License v3, like the Android app. See [LICENSE](LICENSE).
Inspired by [brainjogging](https://github.com/fkropfhamer/brainjogging).
