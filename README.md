# BrainTrainingWeb

The three mini-games of the [BrainTraining](../BrainTraining) Android app as a
small web app for the home network: one password, HTTPS, reachable from any
browser on the LAN.

- **Calculator Mode**: arithmetic up to 1000 (+, −, ×, exact ÷).
- **Remembering Mode**: five digits fade in one after another; type them back
  in order.
- **Colour Mode**: a Stroop test. Tap the ink colour, not the written word.

Each mode counts a score and a streak, as in the app. Nothing is saved: the
games run in the browser, and scores start at zero each time a mode is opened.

It is one 5 MB program built with the Go standard library. The web pages are
compiled into it. It has no database and no settings beyond the password.

## Setup

Requirements: Go 1.22 or newer, [mkcert](https://github.com/FiloSottile/mkcert)
and systemd. On Debian or Ubuntu: `sudo apt install golang-go mkcert libnss3-tools`.

```bash
./setup.sh
```

The script builds the program and creates the certificate with mkcert. It asks
for a password, installs a user service that starts at boot, and offers to add
the firewall rule for port 8444. It then prints the address to open, here
`https://betula-papyrifera.local:8444`.

Running it again is safe. It keeps the certificate and password, rebuilds and
restarts the service, which logs every device out.

To try it without the service: `BT_PASSWORD=test ./braintraining` (after
`setup.sh` has created the certificate).

## Trust the certificate on your devices

The certificate comes from the same mkcert authority as HomeStream, so a device
that already opens HomeStream without a warning needs nothing more. Otherwise,
copy `rootCA.pem` from the folder printed by `mkcert -CAROOT` to the device and
install it as a CA certificate. Never copy `rootCA-key.pem`.

- Android: Settings > Security > Encryption & credentials > Install a certificate > CA certificate.
- iPhone and iPad: open the file and install the profile. Then turn it on under
  Settings > General > About > Certificate Trust Settings. If iOS will not open
  a `.pem` file, rename it to `rootCA.crt`.
- Firefox: Settings > Privacy & Security > Certificates > View Certificates >
  Authorities > Import.

## Configuration

Environment variables. The service reads them from `~/.config/braintraining/env`,
one `NAME=value` per line.

| Variable      | Meaning                 | Default                             |
|---------------|-------------------------|-------------------------------------|
| `BT_PASSWORD` | Login password          | required                            |
| `BT_ADDR`     | Listen address and port | `:8444`                             |
| `BT_CERT`     | TLS certificate         | `~/.config/braintraining/cert.pem`  |
| `BT_KEY`      | TLS private key         | `~/.config/braintraining/key.pem`   |

To change the password, edit the env file and run
`systemctl --user restart braintraining`.

## Security

The same design as HomeStream:

- LAN only, in three layers: the firewall rule, a check in the server that
  refuses any client without a private (LAN) address, and no port forwarding
  on the router. Do not forward port 8444.
- Everything is encrypted with TLS (HTTPS). Without a valid login the server
  sends only the login page.
- One shared password. A wrong one is delayed by one second, and attempts are
  handled one at a time. Each one is logged with the device's address
  (`journalctl --user -u braintraining`).
- Login and sign-out only work from the app's own page. A request that the
  browser marks as coming from another website or another web app on this PC
  is refused.
- A login lasts 30 days in a signed cookie named `bt`. Restarting the server
  logs everyone out.
- The password sits in plain text in the env file, which only you can read.

## Development

```bash
go vet ./...
go test ./...
```

| File                    | Purpose                                         |
|-------------------------|-------------------------------------------------|
| `main.go`               | The whole server: login, session, LAN check     |
| `index.html`            | The games, built into the binary                |
| `login.html`            | The password form, built into the binary        |
| `main_test.go`          | Tests for login, sessions, LAN and cross-site   |
| `braintraining.service` | systemd user service                            |
| `setup.sh`              | First-time setup on a new PC                    |

The task generators in `index.html` are ports of `GameGenerators.kt` from the
Android app and use the same ranges.

## License

GNU General Public License v3, like the Android app. See [LICENSE](LICENSE).
Inspired by [brainjogging](https://github.com/fkropfhamer/brainjogging).
