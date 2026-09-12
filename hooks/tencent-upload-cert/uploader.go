package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tchttp "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/http"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
)

const sslAPIVersion = "2019-12-05"

// uploadRequestTimeout bounds a request made by the real Tencent SDK client.
// A failed request is returned to the hook so the caller can retry on the next
// renewal run; the hook must not wait indefinitely for the remote API.
const uploadRequestTimeout = 30 * time.Second

type UploadRequest struct {
	CertificatePEM string
	PrivateKeyPEM  string
	Alias          string
}

type UploadResponse struct {
	CertificateID string
	RepeatCertID  string
	RequestID     string
}

type uploadCertificateAPI interface {
	Send(request tchttp.Request, response tchttp.Response) error
}

type TencentUploader struct {
	SecretID  string
	SecretKey string
	Client    uploadCertificateAPI
}

func (u *TencentUploader) Upload(req UploadRequest) (UploadResponse, error) {
	if err := validatePEMPair(req.CertificatePEM, req.PrivateKeyPEM); err != nil {
		return UploadResponse{}, err
	}

	client := u.Client
	if client == nil {
		client = newUploadClient(u.SecretID, u.SecretKey)
	}

	request := tchttp.NewCommonRequest("ssl", sslAPIVersion, "UploadCertificate")
	if err := request.SetActionParameters(map[string]interface{}{
		"CertificatePublicKey":  req.CertificatePEM,
		"CertificatePrivateKey": req.PrivateKeyPEM,
		"CertificateType":       "SVR",
		"Alias":                 req.Alias,
		"Repeatable":            false,
	}); err != nil {
		return UploadResponse{}, fmt.Errorf("build request: %w", err)
	}

	response := tchttp.NewCommonResponse()
	if err := client.Send(request, response); err != nil {
		return UploadResponse{}, fmt.Errorf("upload certificate request: %w", err)
	}

	var envelope struct {
		Response struct {
			CertificateID string `json:"CertificateId"`
			RepeatCertID  string `json:"RepeatCertId"`
			RequestID     string `json:"RequestId"`
			Error         *struct {
				Code    string `json:"Code"`
				Message string `json:"Message"`
			} `json:"Error"`
		} `json:"Response"`
	}

	if err := json.Unmarshal(response.GetBody(), &envelope); err != nil {
		return UploadResponse{}, fmt.Errorf("decode response: %w", err)
	}

	if envelope.Response.Error != nil {
		apiError := envelope.Response.Error
		if apiError.Code == "" {
			return UploadResponse{}, fmt.Errorf("Tencent Cloud UploadCertificate failed: %s", apiError.Message)
		}
		return UploadResponse{}, fmt.Errorf("Tencent Cloud UploadCertificate failed (%s): %s", apiError.Code, apiError.Message)
	}

	// Tencent returns CertificateId for a new upload and RepeatCertId when
	// Repeatable=false detects an existing certificate. Treat either as the
	// canonical ID so callers do not upload the same certificate repeatedly.
	certificateID := envelope.Response.CertificateID
	if certificateID == "" {
		certificateID = envelope.Response.RepeatCertID
	}
	if certificateID == "" {
		return UploadResponse{}, errors.New("empty certificate id in Tencent Cloud response")
	}

	return UploadResponse{
		CertificateID: certificateID,
		RepeatCertID:  envelope.Response.RepeatCertID,
		RequestID:     envelope.Response.RequestID,
	}, nil
}

func newUploadClient(secretID, secretKey string) uploadCertificateAPI {
	cred := tccommon.NewCredential(secretID, secretKey)
	cpf := newUploadClientProfile()

	return tccommon.NewCommonClient(cred, "", cpf)
}

func newUploadClientProfile() *profile.ClientProfile {
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "ssl.tencentcloudapi.com"
	cpf.HttpProfile.ReqTimeout = int(uploadRequestTimeout / time.Second)
	cpf.DisableRegionBreaker = true

	return cpf
}

func resolveCertificateMaterial(cfg Config) (string, string, error) {
	certPath, keyPath := cfg.CertPath, cfg.KeyPath
	if cfg.Domain != "" {
		certPath = filepath.Join(cfg.CertDir, cfg.Domain+".crt")
		keyPath = filepath.Join(cfg.CertDir, cfg.Domain+".key")
	}

	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", fmt.Errorf("read cert file: %w", err)
	}

	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return "", "", fmt.Errorf("read key file: %w", err)
	}

	certificatePEM := string(certBytes)
	privateKeyPEM := string(keyBytes)

	if err := validatePEMPair(certificatePEM, privateKeyPEM); err != nil {
		return "", "", err
	}

	return certificatePEM, privateKeyPEM, nil
}

func validatePEMPair(certificatePEM, privateKeyPEM string) error {
	if certificatePEM == "" || privateKeyPEM == "" {
		return errors.New("certificate and private key must not be empty")
	}

	if _, err := tls.X509KeyPair([]byte(certificatePEM), []byte(privateKeyPEM)); err != nil {
		return fmt.Errorf("invalid certificate/key pair: %w", err)
	}

	return nil
}
