package flashpi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"testing"
)

func readForm(t *testing.T, request recorded) (map[string]string, []*multipart.Part) {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
	}
	reader := multipart.NewReader(bytes.NewReader(request.Body), params["boundary"])
	values := map[string]string{}
	var parts []*multipart.Part
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(part)
		values[part.FormName()] = string(content)
		parts = append(parts, part)
	}
	return values, parts
}

func TestUploadACertificate(t *testing.T) {
	client, transport, _ := testClient(t, failNetwork("reset"), respond(202, `{"id":"cert-1","status":"processing"}`))
	certificate, err := client.Issuers.Certificates.Upload(context.Background(), "issuer-1", CertificateUpload{
		File:     strings.NewReader("p12 bytes \x00\xff"),
		Password: "pässword",
	})
	if err != nil {
		t.Fatal(err)
	}
	if certificate.ID != "cert-1" {
		t.Errorf("ID = %s", certificate.ID)
	}
	requests := transport.all()
	if len(requests) != 2 || !bytes.Equal(requests[0].Body, requests[1].Body) || requests[0].Header.Get("Content-Type") != requests[1].Header.Get("Content-Type") {
		t.Fatal("the retry did not send the same body")
	}
	values, parts := readForm(t, requests[1])
	if values["password"] != "pässword" || values["file"] != "p12 bytes \x00\xff" {
		t.Errorf("values = %q", values)
	}
	if parts[0].FormName() != "password" || parts[1].FileName() != "certificate.p12" || parts[1].Header.Get("Content-Type") != "application/x-pkcs12" {
		t.Errorf("parts = %v, %v", parts[0].Header, parts[1].Header)
	}
	if requests[1].Header.Get("Idempotency-Key") != "key-1" || requests[1].URL != testBaseURL+"/v1/issuers/issuer-1/certificates" {
		t.Errorf("got %s with %q", requests[1].URL, requests[1].Header.Get("Idempotency-Key"))
	}
}

func TestStartAnOnboardingWithAFilename(t *testing.T) {
	client, transport, _ := testClient(t, respond(202, `{"id":"onb-1","status":"verifying"}`))
	onboarding, err := client.IssuerOnboardings.Create(context.Background(), CertificateUpload{
		File:     bytes.NewReader([]byte("p12")),
		Password: "",
		Filename: `firma "2026".p12`,
	})
	if err != nil || onboarding.Status != IssuerOnboardingStatusVerifying {
		t.Fatalf("got %v %v", onboarding, err)
	}
	request := transport.only(t)
	values, parts := readForm(t, request)
	if values["password"] != "" || parts[1].FileName() != `firma "2026".p12` {
		t.Errorf("got %q and %q", values, parts[1].FileName())
	}
	if request.URL != testBaseURL+"/v1/issuer-onboardings" {
		t.Errorf("URL = %s", request.URL)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk error") }

// countingReader counts the bytes read from it.
type countingReader struct {
	remaining int
	read      int
}

func (r *countingReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.remaining)
	r.remaining -= n
	r.read += n
	return n, nil
}

func TestBadCertificatesAreRefusedBeforeSending(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	cases := []struct {
		upload  CertificateUpload
		message string
	}{
		{CertificateUpload{Password: "x"}, "needs its File"},
		{CertificateUpload{File: strings.NewReader(""), Password: "x"}, "is empty"},
		{CertificateUpload{File: bytes.NewReader(make([]byte, MaxCertificateBytes+1)), Password: "x"}, "more than 1048576 bytes"},
		{CertificateUpload{File: failingReader{}, Password: "x"}, "could not be read: disk error"},
		{CertificateUpload{File: strings.NewReader("p12"), Password: "x", Filename: "a\r\nX-Injected: 1"}, "control character"},
	}
	for _, tc := range cases {
		_, err := client.IssuerOnboardings.Create(ctx, tc.upload)
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, tc.message) {
			t.Errorf("message %q does not say %q", sdkErr.Message, tc.message)
		}
	}
	if _, err := client.Issuers.Certificates.Upload(ctx, "", p12()); err == nil {
		t.Error("an upload without its issuer was sent")
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
	big := &countingReader{remaining: 50 * MaxCertificateBytes}
	_, err := client.IssuerOnboardings.Create(ctx, CertificateUpload{File: big, Password: "x"})
	if err == nil || big.read > MaxCertificateBytes+1 {
		t.Errorf("read %d bytes of a big file: %v", big.read, err)
	}
}

func TestACertificateOfExactlyTheLimit(t *testing.T) {
	client, transport, _ := testClient(t, respond(202, `{}`))
	_, err := client.IssuerOnboardings.Create(context.Background(), CertificateUpload{File: bytes.NewReader(make([]byte, MaxCertificateBytes)), Password: "x"})
	if err != nil {
		t.Fatal(err)
	}
	values, _ := readForm(t, transport.only(t))
	if len(values["file"]) != MaxCertificateBytes {
		t.Errorf("sent %d bytes", len(values["file"]))
	}
}
