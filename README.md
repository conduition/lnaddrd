# lnaddrd

A self-hosted server to provide yourself with [a Lightning Address](https://lightningaddress.com/), by generating invoices from a remote [LND](https://github.com/lightningnetwork/lnd) instance.

## Install

First, install the [Golang compiler](https://go.dev).

```sh
curl -o - -sL https://go.dev/dl/go1.22.2.linux-amd64.tar.gz | tar xz -C /tmp
sudo cp -r /tmp/go /usr/local
echo 'export PATH="$PATH:/usr/local/go/bin"' >> ~/.bashrc
export PATH="$PATH:/usr/local/go/bin"
go version # to confirm install success
```

Now you can install `lnaddrd` from source:

```sh
go install github.com/conduition/lnaddrd@latest
```

## Usage

`lnaddrd` is quite dumb. It operates with no backend database or state beyond LND itself. It is a simple webserver intended solely to furnish LNURL clients with BOLT11 invoices.

To configure `lnaddrd`, create a YAML file:

```yaml
# This configures how the webserver will bind and expose its HTTP stack.
# By default it serves unencrypted HTTP. Specify a TLS cert+key to serve
# clients over HTTPS instead.
webserver:
  bind_address: 127.0.0.1:3441              # required
  # tls_cert_file: /path/to/server.tls.cert # optional
  # tls_key_file: /path/to/server.tls.key   # optional

lnurl:
  # This must be the base URL of your server.
  url_authority: https://conduition.io # required

  # Both of these will be included in the pay request metadata array.
  # The icon_file can be either a PNG or a JPEG file.
  short_description: "Donation to conduition" # optional
  icon_file: /path/to/icon.png                # required

  # Determines the range of acceptable payment amounts.
  max_pay_request_sats: 5_000_000 # required
  min_pay_request_sats: 100       # required

  # Determines the expiry time of BOLT11 invoices we create.
  # Defaults to whatever the remote LND instance uses by default.
  invoice_expiry: "1h" # optional
  # invoice_expiry: "20m"
  # invoice_expiry: "100s"

# Accept lightning address requests for the following usernames.
lightning_address_usernames: # optional
  - conduition

# Configure a connection to LND's REST API.
#
# You can find invoices.macaroon in:     ~/.lnd/data/chain/bitcoin/mainnet/invoices.macaroon
# You can find LND's TLS certificate in: ~/.lnd/tls.cert
#
# Note that your LND certificate MUST have the 'host' field listed as a SAN.
# (hint: use the 'tlsextradomain' option in lnd.conf)
lnd:
  host: conduition.io:8080                  # required
  macaroon_file: /path/to/invoices.macaroon # required
  tls_cert_file: /path/to/lnd.tls.cert      # required
```

Launch `lnaddrd` by pointing it at the config file.

```
# lnaddrd /path/to/lnaddrd.yaml
2024/04/21 02:17:00.560533 starting server on 127.0.0.1:3441
```

Lightning Address HTTP requests will come in at `/.well-known/lnurlp/:username`. The server will only serve responses for usernames which are explicitly listed in the config file.

The client will be given a callback URL pointing to `<url_authority>/pay/callback/:username`. A request to `<url_authority>/pay/callback/:username?amount=<amount>` will cause `lnaddrd` to fetch an invoice from the remote LND instance, which is handed over to the client according to standard LNURL protocols.

That's it.
