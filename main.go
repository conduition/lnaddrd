package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"gitlab.com/starius/encrypt-autocert-cache"
	"golang.org/x/crypto/acme/autocert"
	"gopkg.in/yaml.v3"
)

type LndConfig struct {
	Host                 string `yaml:"host"`
	TlsCertFile          string `yaml:"tls_cert_file"`
	MacaroonFile         string `yaml:"macaroon_file"`
	UnsafeAllowPlaintext bool   `yaml:"unsafe_allow_plaintext"`
}

func (cfg *LndConfig) Validate() error {
	if cfg.Host == "" {
		return errors.New("missing 'lnd.host' in config")
	} else if cfg.TlsCertFile == "" && !cfg.UnsafeAllowPlaintext {
		return errors.New(
			"missing 'lnd.tls_cert_file' in config; unsafe_allow_plaintext=true is required for plaintext transport",
		)
	} else if cfg.MacaroonFile == "" {
		return errors.New("missing 'lnd.macaroon_file' in config")
	}
	return nil
}

type WebserverConfig struct {
	BindAddress string `yaml:"bind_address"`
	TlsCertFile string `yaml:"tls_cert_file"`
	TlsKeyFile  string `yaml:"tls_key_file"`

	// If specified, don't specify any bind_address, tls_cert_file, tls_key_file.
	AutocertDomains []string `yaml:"autocert_domains"`
	AutocertDir     string   `yaml:"autocert_dir"`
}

func (cfg *WebserverConfig) Validate() error {
	if (cfg.AutocertDir != "") != (len(cfg.AutocertDomains) != 0) {
		return errors.New("specify autocert_dir when autocert_domains is specified")
	}

	if len(cfg.AutocertDomains) != 0 {
		if cfg.BindAddress != "" {
			return errors.New("don't combine autocert_domains and bind_address")
		}
		if cfg.TlsCertFile != "" {
			return errors.New("don't combine autocert_domains and tls_cert_file")
		}
		if cfg.TlsKeyFile != "" {
			return errors.New("don't combine autocert_domains and tls_key_file")
		}
	} else if cfg.BindAddress == "" {
		return errors.New("missing 'bind_address' in config")
	}

	return nil
}

type Config struct {
	Webserver WebserverConfig `yaml:"webserver"`
	Lnurl     struct {
		UrlAuthority      string        `yaml:"url_authority"`
		IconFile          string        `yaml:"icon_file"`
		ShortDescription  string        `yaml:"short_description"`
		MaxPayRequestSats uint64        `yaml:"max_pay_request_sats"`
		MinPayRequestSats uint64        `yaml:"min_pay_request_sats"`
		InvoiceExpiry     time.Duration `yaml:"invoice_expiry"`
	} `yaml:"lnurl"`
	LightningAddressUsernames []string  `yaml:"lightning_address_usernames"`
	Lnd                       LndConfig `yaml:"lnd"`
}

func (cfg *Config) Validate() error {
	if err := cfg.Webserver.Validate(); err != nil {
		return err
	}

	if cfg.Lnurl.UrlAuthority == "" {
		return errors.New("missing 'url_authority' in config")
	} else if cfg.Lnurl.IconFile == "" {
		return errors.New("missing 'icon_file' in config")
	} else if cfg.Lnurl.MaxPayRequestSats == 0 {
		return errors.New("missing 'max_pay_request_sats' in config")
	} else if cfg.Lnurl.MinPayRequestSats == 0 {
		return errors.New("missing 'min_pay_request_sats' in config")
	}

	if err := cfg.Lnd.Validate(); err != nil {
		return err
	}

	return nil
}

func ReadConfigFile(cfgPath string) (*Config, error) {
	file, err := os.Open(cfgPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func redirectToHTTPS(w http.ResponseWriter, r *http.Request) {
	url := "https://" + r.Host + r.URL.Path
	if len(r.URL.RawQuery) > 0 {
		url += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, url, http.StatusPermanentRedirect)
}

func StartServer(cfg *WebserverConfig, mux http.Handler) error {
	server := http.Server{
		Addr:           cfg.BindAddress,
		Handler:        mux,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1mb
	}

	if len(cfg.AutocertDomains) != 0 {
		log.Printf("Starting server on ports 443 and 80, using autocert domains %v.", cfg.AutocertDomains)

		cache := autocert.DirCache(cfg.AutocertDir)
		manager := &autocert.Manager{
			Cache:      cache,
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(cfg.AutocertDomains...),
		}

		certHandler := manager.HTTPHandler(http.HandlerFunc(redirectToHTTPS))
		tlsConfig := manager.TLSConfig()
		tlsConfig.MinVersion = tls.VersionTLS12
		tcpLis, err := net.Listen("tcp", ":443")
		if err != nil {
			return fmt.Errorf("failed to listen on port 443: %w", err)
		}
		lis, err := encrypt.NewListener(tlsConfig, tcpLis)
		if err != nil {
			return fmt.Errorf("failed to create listener: %w", err)
		}

		errChan := make(chan error)
		go func() {
			errChan <- http.ListenAndServe(":80", certHandler)
		}()
		go func() {
			errChan <- server.Serve(lis)
		}()

		return <-errChan
	}

	log.Printf("starting server on %s", cfg.BindAddress)

	if cfg.TlsCertFile == "" {
		return server.ListenAndServe()
	} else {
		return server.ListenAndServeTLS(cfg.TlsCertFile, cfg.TlsKeyFile)
	}
}

func main() {
	log.SetFlags(log.LUTC | log.Ldate | log.Ltime | log.Lmicroseconds)
	flag.Parse()

	if len(flag.Args()) != 1 {
		log.Println("Please provide path to YAML config file.")
		os.Exit(1)
	}

	cfgPath := flag.Arg(0)
	cfg, err := ReadConfigFile(cfgPath)
	if err != nil {
		log.Printf("Error reading config file at %s: %s", cfgPath, err)
		os.Exit(1)
	}

	mux, err := CreateMux(cfg)
	if err != nil {
		log.Printf("Error setting up mux: %s", err)
		os.Exit(1)
	}

	if err := StartServer(&cfg.Webserver, mux); err != nil {
		log.Printf("SERVER FATAL ERROR: %s", err)
		os.Exit(1)
	}
}
