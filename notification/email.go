package notification

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/resend/resend-go/v2"
)

type EmailService struct {
	client    *resend.Client
	fromEmail string
	fromName  string
	toEmails  []string
	enabled   bool
}

type NotificationData struct {
	Domains    []string
	Success    bool
	Error      string
	Timestamp  time.Time
	CertExpiry *time.Time
	Remaining  *time.Duration
}

func NewEmailService(apiKey, fromEmail, fromName string, toEmails []string, enabled bool) *EmailService {
	if !enabled || apiKey == "" {
		return &EmailService{enabled: false}
	}

	return &EmailService{
		client:    resend.NewClient(apiKey),
		fromEmail: fromEmail,
		fromName:  fromName,
		toEmails:  toEmails,
		enabled:   enabled,
	}
}

func (e *EmailService) IsEnabled() bool {
	return e.enabled
}

func (e *EmailService) SendSuccessNotification(data NotificationData) error {
	if !e.enabled {
		return nil
	}

	subject := fmt.Sprintf("✅ 证书续期成功 - %s", strings.Join(data.Domains, ", "))

	htmlContent := e.buildSuccessHTML(data)
	textContent := e.buildSuccessText(data)

	return e.sendEmail(subject, htmlContent, textContent)
}

func (e *EmailService) SendFailureNotification(data NotificationData) error {
	if !e.enabled {
		return nil
	}

	subject := fmt.Sprintf("❌ 证书续期失败 - %s", strings.Join(data.Domains, ", "))

	htmlContent := e.buildFailureHTML(data)
	textContent := e.buildFailureText(data)

	return e.sendEmail(subject, htmlContent, textContent)
}

func (e *EmailService) SendExpiryWarningNotification(data NotificationData) error {
	if !e.enabled {
		return nil
	}

	subject := fmt.Sprintf("⚠️ 证书即将到期 - %s", strings.Join(data.Domains, ", "))

	htmlContent := e.buildExpiryHTML(data)
	textContent := e.buildExpiryText(data)

	return e.sendEmail(subject, htmlContent, textContent)
}

func (e *EmailService) sendEmail(subject, htmlContent, textContent string) error {
	if len(e.toEmails) == 0 {
		return fmt.Errorf("没有配置收件人邮箱")
	}

	fromAddress := e.fromEmail
	if e.fromName != "" {
		fromAddress = fmt.Sprintf("%s <%s>", e.fromName, e.fromEmail)
	}

	params := &resend.SendEmailRequest{
		From:    fromAddress,
		To:      e.toEmails,
		Subject: subject,
		Html:    htmlContent,
		Text:    textContent,
	}

	sent, err := e.client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("发送邮件失败: %v", err)
	}

	log.Printf("邮件发送成功，ID: %s，收件人: %s", sent.Id, strings.Join(e.toEmails, ", "))
	return nil
}

func (e *EmailService) buildSuccessHTML(data NotificationData) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>证书续期成功通知</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #4CAF50; color: white; padding: 20px; text-align: center; border-radius: 5px 5px 0 0; }
        .content { background-color: #f9f9f9; padding: 20px; border-radius: 0 0 5px 5px; }
        .success { color: #4CAF50; font-weight: bold; }
        .info-table { width: 100%%; border-collapse: collapse; margin-top: 15px; }
        .info-table th, .info-table td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
        .info-table th { background-color: #f2f2f2; }
        .footer { margin-top: 20px; font-size: 12px; color: #666; text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>✅ 证书续期成功</h1>
        </div>
        <div class="content">
            <p>您好，</p>
            <p class="success">以下域名的 SSL/TLS 证书已成功续期：</p>
            <ul>
                %s
            </ul>
            <table class="info-table">
                <tr><th>续期时间</th><td>%s</td></tr>
                %s
                %s
            </table>
            <p>证书已更新并保存到指定目录，相关服务已重新加载配置。</p>
        </div>
        <div class="footer">
            <p>此邮件由 ACME4 证书管理工具自动发送</p>
        </div>
    </div>
</body>
</html>`,
		e.formatDomainList(data.Domains),
		data.Timestamp.Format("2006-01-02 15:04:05"),
		e.formatCertExpiryRow(data.CertExpiry),
		e.formatRemainingRow(data.Remaining),
	)
}

func (e *EmailService) buildSuccessText(data NotificationData) string {
	text := fmt.Sprintf(`证书续期成功通知

域名: %s
续期时间: %s`,
		strings.Join(data.Domains, ", "),
		data.Timestamp.Format("2006-01-02 15:04:05"),
	)

	if data.CertExpiry != nil {
		text += fmt.Sprintf("\n新证书到期时间: %s", data.CertExpiry.Format("2006-01-02 15:04:05"))
	}

	if data.Remaining != nil {
		text += fmt.Sprintf("\n新证书有效期: %v", *data.Remaining)
	}

	text += "\n\n证书已更新并保存到指定目录，相关服务已重新加载配置。\n\n此邮件由 ACME4 证书管理工具自动发送。"

	return text
}

func (e *EmailService) buildFailureHTML(data NotificationData) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>证书续期失败通知</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #f44336; color: white; padding: 20px; text-align: center; border-radius: 5px 5px 0 0; }
        .content { background-color: #f9f9f9; padding: 20px; border-radius: 0 0 5px 5px; }
        .error { color: #f44336; font-weight: bold; }
        .error-box { background-color: #ffebee; border-left: 4px solid #f44336; padding: 15px; margin: 15px 0; }
        .info-table { width: 100%%; border-collapse: collapse; margin-top: 15px; }
        .info-table th, .info-table td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
        .info-table th { background-color: #f2f2f2; }
        .footer { margin-top: 20px; font-size: 12px; color: #666; text-align: center; }
        .action { background-color: #fff3cd; border: 1px solid #ffeaa7; padding: 15px; margin: 15px 0; border-radius: 5px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>❌ 证书续期失败</h1>
        </div>
        <div class="content">
            <p>您好，</p>
            <p class="error">以下域名的 SSL/TLS 证书续期失败：</p>
            <ul>
                %s
            </ul>
            <table class="info-table">
                <tr><th>失败时间</th><td>%s</td></tr>
            </table>
            <div class="error-box">
                <h3>错误信息：</h3>
                <pre>%s</pre>
            </div>
            <div class="action">
                <h3>建议操作：</h3>
                <ul>
                    <li>检查 DNS 配置是否正确</li>
                    <li>验证 Provider 凭证是否有效</li>
                    <li>确认网络连通性</li>
                    <li>查看详细日志获取更多信息</li>
                </ul>
            </div>
        </div>
        <div class="footer">
            <p>此邮件由 ACME4 证书管理工具自动发送</p>
        </div>
    </div>
</body>
</html>`,
		e.formatDomainList(data.Domains),
		data.Timestamp.Format("2006-01-02 15:04:05"),
		data.Error,
	)
}

func (e *EmailService) buildFailureText(data NotificationData) string {
	return fmt.Sprintf(`证书续期失败通知

域名: %s
失败时间: %s

错误信息: %s

建议操作:
- 检查 DNS 配置是否正确
- 验证 Provider 凭证是否有效
- 确认网络连通性
- 查看详细日志获取更多信息

此邮件由 ACME4 证书管理工具自动发送。`,
		strings.Join(data.Domains, ", "),
		data.Timestamp.Format("2006-01-02 15:04:05"),
		data.Error,
	)
}

func (e *EmailService) buildExpiryHTML(data NotificationData) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>证书即将到期通知</title>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #ff9800; color: white; padding: 20px; text-align: center; border-radius: 5px 5px 0 0; }
        .content { background-color: #f9f9f9; padding: 20px; border-radius: 0 0 5px 5px; }
        .warning { color: #ff9800; font-weight: bold; }
        .info-table { width: 100%%; border-collapse: collapse; margin-top: 15px; }
        .info-table th, .info-table td { padding: 10px; text-align: left; border-bottom: 1px solid #ddd; }
        .info-table th { background-color: #f2f2f2; }
        .footer { margin-top: 20px; font-size: 12px; color: #666; text-align: center; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>⚠️ 证书即将到期</h1>
        </div>
        <div class="content">
            <p>您好，</p>
            <p class="warning">以下域名的 SSL/TLS 证书即将到期：</p>
            <ul>
                %s
            </ul>
            <table class="info-table">
                <tr><th>检查时间</th><td>%s</td></tr>
                %s
                %s
            </table>
            <p>请确保 ACME4 自动续期功能正常工作，或手动进行证书续期。</p>
        </div>
        <div class="footer">
            <p>此邮件由 ACME4 证书管理工具自动发送</p>
        </div>
    </div>
</body>
</html>`,
		e.formatDomainList(data.Domains),
		data.Timestamp.Format("2006-01-02 15:04:05"),
		e.formatCertExpiryRow(data.CertExpiry),
		e.formatRemainingRow(data.Remaining),
	)
}

func (e *EmailService) buildExpiryText(data NotificationData) string {
	text := fmt.Sprintf(`证书即将到期通知

域名: %s
检查时间: %s`,
		strings.Join(data.Domains, ", "),
		data.Timestamp.Format("2006-01-02 15:04:05"),
	)

	if data.CertExpiry != nil {
		text += fmt.Sprintf("\n到期时间: %s", data.CertExpiry.Format("2006-01-02 15:04:05"))
	}

	if data.Remaining != nil {
		text += fmt.Sprintf("\n剩余有效期: %v", *data.Remaining)
	}

	text += "\n\n请确保 ACME4 自动续期功能正常工作，或手动进行证书续期。\n\n此邮件由 ACME4 证书管理工具自动发送。"

	return text
}

func (e *EmailService) formatDomainList(domains []string) string {
	var items []string
	for _, domain := range domains {
		items = append(items, fmt.Sprintf("<li>%s</li>", domain))
	}
	return strings.Join(items, "\n                ")
}

func (e *EmailService) formatCertExpiryRow(expiry *time.Time) string {
	if expiry == nil {
		return ""
	}
	return fmt.Sprintf("<tr><th>证书到期时间</th><td>%s</td></tr>", expiry.Format("2006-01-02 15:04:05"))
}

func (e *EmailService) formatRemainingRow(remaining *time.Duration) string {
	if remaining == nil {
		return ""
	}
	return fmt.Sprintf("<tr><th>剩余有效期</th><td>%v</td></tr>", *remaining)
}
