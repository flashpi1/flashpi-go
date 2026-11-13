package flashpi

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
)

// MaxCertificateBytes is the largest .p12 the API accepts (413 payload_too_large above it).
const MaxCertificateBytes = 1_048_576

const (
	defaultCertificateFilename = "certificate.p12"
	certificateContentType     = "application/x-pkcs12"
)

// CertificateUpload is a .p12 certificate (firma electrónica) and its password, for
// client.Issuers.Certificates.Upload and client.IssuerOnboardings.Create.
type CertificateUpload struct {
	// File is the .p12: bytes.NewReader(data), an *os.File, ... It is read once, to its end and
	// before anything is sent, so every retry sends the same bytes; a file over 1 MiB is
	// refused without reading it all. The SDK does not close it.
	File io.Reader

	// Password is the password of the .p12.
	Password string

	// Filename is the name sent with the file (default certificate.p12).
	Filename string
}

// certificateForm is the multipart body of a .p12 upload: a password field, then the file. It
// fails before anything is sent on a missing, empty or too large file.
func certificateForm(upload CertificateUpload) (*operation, error) {
	if upload.File == nil {
		return nil, &Error{Message: "The certificate upload needs its File"}
	}
	content, err := io.ReadAll(io.LimitReader(upload.File, MaxCertificateBytes+1))
	if err != nil {
		return nil, &Error{Message: "The certificate file could not be read: " + err.Error(), Err: err}
	}
	if len(content) == 0 {
		return nil, &Error{Message: "The certificate file is empty"}
	}
	if len(content) > MaxCertificateBytes {
		return nil, &Error{Message: fmt.Sprintf("The certificate file has more than %d bytes, the most the API accepts", MaxCertificateBytes)}
	}
	filename := upload.Filename
	if filename == "" {
		filename = defaultCertificateFilename
	}
	for _, r := range filename {
		if r < 0x20 || r == 0x7f {
			return nil, &Error{Message: "The certificate filename has a line break or another control character"}
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("password", upload.Password); err != nil {
		return nil, err
	}
	part := textproto.MIMEHeader{}
	part.Set("Content-Disposition", `form-data; name="file"; filename="`+quoteEscaper.Replace(filename)+`"`)
	part.Set("Content-Type", certificateContentType)
	file, err := writer.CreatePart(part)
	if err != nil {
		return nil, err
	}
	if _, err := file.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return &operation{
		method:      "POST",
		content:     body.Bytes(),
		contentType: writer.FormDataContentType(),
		accept:      jsonAccept,
	}, nil
}

// quoteEscaper escapes a file name inside a quoted header parameter, as mime/multipart does.
var quoteEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)
