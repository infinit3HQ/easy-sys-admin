package fleet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/netguard"
	"github.com/nirajdilshan/easy-sys-admin/apps/esa/internal/remote"
)

// HTTPStatusError is a non-2xx answer from a notification endpoint.
type HTTPStatusError struct{ Code int }

func (e *HTTPStatusError) Error() string { return "HTTP " + strconv.Itoa(e.Code) }

// netCtx applies the account-independent address policy to SSH dials made
// with ctx.
func (s *Service) netCtx(ctx context.Context) context.Context {
	return remote.WithAllowPrivate(ctx, s.allowPrivate)
}

// netErrorText turns a connection error into a short category that is safe
// to show: it does not echo arbitrary text from the remote side or from
// esa's internals. Callers log the full error.
func netErrorText(err error) string {
	var (
		blocked  *netguard.BlockedError
		status   *HTTPStatusError
		smtpErr  *textproto.Error
		dnsErr   *net.DNSError
		mismatch *remote.HostKeyMismatchError
		certErr  *tls.CertificateVerificationError
		hostErr  x509.HostnameError
		unknown  x509.UnknownAuthorityError
		invalid  x509.CertificateInvalidError
		recErr   tls.RecordHeaderError
		alertErr tls.AlertError
	)
	switch {
	case err == nil:
		return ""
	case errors.As(err, &blocked):
		return blocked.Error()
	case errors.As(err, &mismatch):
		return mismatch.Error()
	case errors.As(err, &status):
		return status.Error()
	case errors.As(err, &smtpErr):
		return "SMTP " + strconv.Itoa(smtpErr.Code)
	case errors.Is(err, remote.ErrAuthFailed):
		return "authentication failed"
	case errors.As(err, &dnsErr):
		return "host not found"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "host unreachable"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded), isTimeout(err):
		return "timed out"
	case errors.As(err, &hostErr):
		return "TLS error: certificate is not valid for this hostname"
	case errors.As(err, &unknown):
		return "TLS error: certificate signed by an unknown authority"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "TLS error: certificate has expired or is not yet valid"
		}
		return "TLS error: certificate is not valid"
	case errors.As(err, &certErr), errors.As(err, &recErr), errors.As(err, &alertErr), strings.Contains(err.Error(), "tls:"):
		return "TLS error"
	case strings.Contains(err.Error(), "ssh:"), strings.Contains(err.Error(), "read host key"):
		return "SSH handshake failed"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, net.ErrClosed), errors.Is(err, os.ErrClosed):
		return "connection closed"
	}
	return "connection failed"
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
