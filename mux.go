package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/lnproxy/lnc"
)

func replyJson(res http.ResponseWriter, value any) {
	jsonData, _ := json.Marshal(value)
	res.Header().Set("Content-Type", "application/json")
	res.Header().Set("Content-Length", strconv.Itoa(len(jsonData)))
	res.Write(jsonData)
}

func openIconFileAndConvertToPngBase64(iconFilePath string) (string, error) {
	imageFile, err := os.Open(iconFilePath)
	if err != nil {
		return "", fmt.Errorf("unable to open icon_file at %q: %w", iconFilePath, err)
	}
	defer imageFile.Close()

	icon, _, err := image.Decode(imageFile)
	if err != nil {
		return "", fmt.Errorf("invalid icon_file content: %w", err)
	}

	pngData := new(bytes.Buffer)
	if err := png.Encode(pngData, icon); err != nil {
		return "", err
	}
	pngBase64 := base64.StdEncoding.EncodeToString(pngData.Bytes())

	return pngBase64, nil
}

func metadataArray(lightningAddress, shortDesc, pngBase64 string) json.RawMessage {
	array := [][2]string{
		{"text/identifier", lightningAddress},
		{"text/plain", shortDesc},
		{"image/png;base64", pngBase64},
	}

	jsonArray, _ := json.Marshal(array)
	return json.RawMessage(jsonArray)
}

func createLndClient(cfg *LndConfig) (*lnc.Lnd, error) {
	tlsCertContent, err := os.ReadFile(cfg.TlsCertFile)
	if err != nil {
		return nil, fmt.Errorf("error reading cert file %q: %w", cfg.TlsCertFile, err)
	}
	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(tlsCertContent)
	tlsConfig := &tls.Config{RootCAs: certPool}

	macaroon, err := os.ReadFile(cfg.MacaroonFile)
	if err != nil {
		return nil, fmt.Errorf("error reading macaroon file %q: %w", cfg.MacaroonFile, err)
	}

	lnd := &lnc.Lnd{
		Host: &url.URL{Scheme: "https", Host: cfg.Host},
		Client: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
			Timeout:   15 * time.Second,
		},
		TlsConfig: tlsConfig,
		Macaroon:  hex.EncodeToString(macaroon),
	}
	return lnd, nil
}

func parseDomainName(authority string) (string, error) {
	decoded, err := url.Parse(authority)
	if err != nil {
		return "", err
	}
	return decoded.Host, nil
}

func logRequest(req *http.Request) {
	log.Printf("%s %s", req.Method, req.URL.Path)
}

func CreateMux(cfg *Config) (*http.ServeMux, error) {
	pngBase64, err := openIconFileAndConvertToPngBase64(cfg.Lnurl.IconFile)
	if err != nil {
		return nil, err
	}

	lnd, err := createLndClient(&cfg.Lnd)
	if err != nil {
		return nil, err
	}

	domainName, err := parseDomainName(cfg.Lnurl.UrlAuthority)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()

	for _, username := range cfg.LightningAddressUsernames {
		lightningAddress := username + "@" + domainName
		staticMetadataArray := metadataArray(lightningAddress, cfg.Lnurl.ShortDescription, pngBase64)
		staticMetadataArrayHash := sha256.Sum256([]byte(staticMetadataArray))

		mux.HandleFunc(
			"GET /pay/callback/"+username,
			func(res http.ResponseWriter, req *http.Request) {
				logRequest(req)

				millisatAmount, err := strconv.ParseUint(req.URL.Query().Get("amount"), 10, 64)
				if err != nil {
					res.WriteHeader(http.StatusBadRequest)
					replyJson(res, map[string]string{
						"status": "ERROR",
						"reason": fmt.Sprintf("cannot parse amount: %s", err),
					})
					return
				}

				if millisatAmount > cfg.Lnurl.MaxPayRequestSats*1000 ||
					millisatAmount < cfg.Lnurl.MinPayRequestSats*1000 {
					res.WriteHeader(http.StatusBadRequest)
					replyJson(res, map[string]string{
						"status": "ERROR",
						"reason": "amount is out of acceptable range",
					})
					return
				}

				invoice, err := lnd.AddInvoice(lnc.InvoiceParameters{
					ValueMsat:       millisatAmount,
					DescriptionHash: staticMetadataArrayHash[:],
					Expiry:          uint64(cfg.Lnurl.InvoiceExpiry.Seconds()),
				})
				if err != nil {
					res.WriteHeader(http.StatusInternalServerError)
					replyJson(res, map[string]string{
						"status": "ERROR",
						"reason": fmt.Sprintf("error constructing invoice: %s", err),
					})
					return
				}

				replyJson(res, map[string]any{
					"pr":     invoice,
					"routes": []string{},
				})
			},
		)

		mux.HandleFunc(
			"GET /.well-known/lnurlp/"+username,
			func(res http.ResponseWriter, req *http.Request) {
				logRequest(req)
				replyJson(res, map[string]any{
					"callback":    cfg.Lnurl.UrlAuthority + "/pay/callback/" + username,
					"maxSendable": cfg.Lnurl.MaxPayRequestSats * 1000,
					"minSendable": cfg.Lnurl.MinPayRequestSats * 1000,
					"metadata":    string(staticMetadataArray),
					"tag":         "payRequest",
				})
			},
		)
	}

	return mux, nil
}
