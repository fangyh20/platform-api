package services

import (
	"fmt"
	"net/smtp"
	"strings"

	"github.com/rapidbuildapp/rapidbuild/config"
)

type EmailService struct {
	Config *config.Config
}

func NewEmailService(cfg *config.Config) *EmailService {
	return &EmailService{Config: cfg}
}

// SendVerificationEmail sends email verification link
func (s *EmailService) SendVerificationEmail(email, fullName, token string) error {
	verificationURL := fmt.Sprintf("%s/auth/verify-email?token=%s", s.Config.FrontendURL, token)

	subject := "Verify your RapidBuild account"
	body := fmt.Sprintf(`
		<html>
		<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
			<div style="max-width: 600px; margin: 0 auto; padding: 20px;">
				<h2 style="color: #3B82F6;">Welcome to RapidBuild!</h2>
				<p>Hi %s,</p>
				<p>Thank you for signing up! Please verify your email address by clicking the button below:</p>
				<div style="margin: 30px 0;">
					<a href="%s" style="background-color: #3B82F6; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; display: inline-block;">
						Verify Email Address
					</a>
				</div>
				<p>Or copy and paste this link into your browser:</p>
				<p style="color: #666; word-break: break-all;">%s</p>
				<p>This link will expire in 24 hours.</p>
				<hr style="border: none; border-top: 1px solid #eee; margin: 30px 0;">
				<p style="color: #999; font-size: 12px;">
					If you didn't create an account with RapidBuild, please ignore this email.
				</p>
			</div>
		</body>
		</html>
	`, fullName, verificationURL, verificationURL)

	return s.sendEmail(email, subject, body)
}

// SendPasswordResetEmail sends password reset link
func (s *EmailService) SendPasswordResetEmail(email, fullName, token string) error {
	resetURL := fmt.Sprintf("%s/auth/reset-password?token=%s", s.Config.FrontendURL, token)

	subject := "Reset your RapidBuild password"
	body := fmt.Sprintf(`
		<html>
		<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333;">
			<div style="max-width: 600px; margin: 0 auto; padding: 20px;">
				<h2 style="color: #3B82F6;">Password Reset Request</h2>
				<p>Hi %s,</p>
				<p>We received a request to reset your password. Click the button below to create a new password:</p>
				<div style="margin: 30px 0;">
					<a href="%s" style="background-color: #3B82F6; color: white; padding: 12px 30px; text-decoration: none; border-radius: 5px; display: inline-block;">
						Reset Password
					</a>
				</div>
				<p>Or copy and paste this link into your browser:</p>
				<p style="color: #666; word-break: break-all;">%s</p>
				<p>This link will expire in 1 hour.</p>
				<hr style="border: none; border-top: 1px solid #eee; margin: 30px 0;">
				<p style="color: #999; font-size: 12px;">
					If you didn't request a password reset, please ignore this email. Your password will remain unchanged.
				</p>
			</div>
		</body>
		</html>
	`, fullName, resetURL, resetURL)

	return s.sendEmail(email, subject, body)
}

// sendEmail sends an email via SMTP
func (s *EmailService) sendEmail(to, subject, htmlBody string) error {
	from := s.Config.SMTPFrom
	password := s.Config.SMTPPassword

	// Construct email message
	msg := []byte(strings.Join([]string{
		fmt.Sprintf("From: %s", from),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n"))

	// SMTP server configuration
	smtpHost := s.Config.SMTPHost
	smtpPort := fmt.Sprintf("%d", s.Config.SMTPPort)
	smtpAddr := fmt.Sprintf("%s:%s", smtpHost, smtpPort)

	// Authentication
	auth := smtp.PlainAuth("", s.Config.SMTPUsername, password, smtpHost)

	// Send email
	err := smtp.SendMail(smtpAddr, auth, from, []string{to}, msg)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}
