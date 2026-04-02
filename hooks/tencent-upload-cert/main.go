package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse config: %v\n", err)
		os.Exit(2)
	}

	certificatePEM, privateKeyPEM, err := resolveCertificateMaterial(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve certificate: %v\n", err)
		os.Exit(2)
	}

	uploader := &TencentUploader{
		SecretID:  cfg.SecretID,
		SecretKey: cfg.SecretKey,
	}

	resp, err := uploader.Upload(UploadRequest{
		CertificatePEM: certificatePEM,
		PrivateKeyPEM:  privateKeyPEM,
		Alias:          cfg.Alias,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "upload certificate: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("uploaded certificate_id=%s request_id=%s\n", resp.CertificateID, resp.RequestID)
}

type Config struct {
	CertPath  string
	KeyPath   string
	Domain    string
	CertDir   string
	SecretID  string
	SecretKey string
	Alias     string
}

func parseConfig(args []string) (Config, error) {
	fs := flag.NewFlagSet("tencent-upload-cert", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	var cfg Config
	fs.StringVar(&cfg.CertPath, "cert", "", "certificate PEM file path")
	fs.StringVar(&cfg.KeyPath, "key", "", "private key PEM file path")
	fs.StringVar(&cfg.Domain, "domain", "", "domain name used to derive certificate file names")
	fs.StringVar(&cfg.CertDir, "cert-dir", "", "certificate directory used with --domain")
	fs.StringVar(&cfg.SecretID, "secret-id", "", "Tencent Cloud secret id")
	fs.StringVar(&cfg.SecretKey, "secret-key", "", "Tencent Cloud secret key")
	fs.StringVar(&cfg.Alias, "alias", "", "certificate alias")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if cfg.SecretID == "" {
		cfg.SecretID = os.Getenv("TENCENTCLOUD_SECRET_ID")
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = os.Getenv("TENCENTCLOUD_SECRET_KEY")
	}

	if cfg.SecretID == "" || cfg.SecretKey == "" {
		return Config{}, errors.New("secret id/key is required via flags or environment")
	}

	explicitPaths := cfg.CertPath != "" || cfg.KeyPath != ""
	domainMode := cfg.Domain != "" || cfg.CertDir != ""

	switch {
	case explicitPaths && domainMode:
		return Config{}, errors.New("use either --cert/--key or --domain/--cert-dir, not both")
	case explicitPaths:
		if cfg.CertPath == "" || cfg.KeyPath == "" {
			return Config{}, errors.New("--cert and --key must be provided together")
		}
	case domainMode:
		if cfg.Domain == "" || cfg.CertDir == "" {
			return Config{}, errors.New("--domain and --cert-dir must be provided together")
		}
	default:
		return Config{}, errors.New("either --cert/--key or --domain/--cert-dir is required")
	}

	if cfg.Alias == "" {
		switch {
		case cfg.Domain != "":
			cfg.Alias = cfg.Domain
		case cfg.CertPath != "":
			cfg.Alias = filepath.Base(cfg.CertPath)
		}
	}

	return cfg, nil
}
