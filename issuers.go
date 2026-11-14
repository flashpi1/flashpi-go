package flashpi

import (
	"context"
	"iter"
	"net/http"
)

// IssuerService manages the issuers (RUCs) of the account, with their establishments, emission
// points and .p12 certificates. It needs an account-scoped API key with read_write access, also
// to read. See client.Issuers.
type IssuerService struct {
	engine *engine

	// Establishments manages the establishments of an issuer, with their emission points.
	Establishments *EstablishmentService

	// Certificates manages the .p12 certificates of an issuer.
	Certificates *CertificateService
}

// Create creates an issuer. To start from its .p12 instead, see client.IssuerOnboardings.
func (s *IssuerService) Create(ctx context.Context, body CreateIssuer, options ...RequestOption) (*Issuer, error) {
	op, err := jsonOperation(http.MethodPost, "/v1/issuers", body)
	if err != nil {
		return nil, err
	}
	return fetch[Issuer](ctx, s.engine, op, options)
}

func (s *IssuerService) lister(params *IssuerListParams, options []RequestOption) *lister[*Issuer] {
	return newLister(s.engine, "/v1/issuers", params.values(), options, decodeStruct[Issuer])
}

// List walks the issuers, newest first, reading the pages as it goes. params may be nil.
func (s *IssuerService) List(ctx context.Context, params *IssuerListParams, options ...RequestOption) iter.Seq2[*Issuer, error] {
	return s.lister(params, options).all(ctx)
}

// ListPage reads the first page of issuers, newest first; NextPage reads the next one.
func (s *IssuerService) ListPage(ctx context.Context, params *IssuerListParams, options ...RequestOption) (*Page[*Issuer], error) {
	return s.lister(params, options).page(ctx, "")
}

// Get retrieves an issuer.
func (s *IssuerService) Get(ctx context.Context, issuerID string, options ...RequestOption) (*Issuer, error) {
	return get[Issuer](ctx, s.engine, options, "/v1/issuers/{}", issuerID)
}

// Update changes an issuer's name, tax profile, branding or defaults: only the members set are
// sent, and flashpi.Null clears an optional one.
func (s *IssuerService) Update(ctx context.Context, issuerID string, body UpdateIssuer, options ...RequestOption) (*Issuer, error) {
	return update[Issuer](ctx, s.engine, options, body, "/v1/issuers/{}", issuerID)
}

// Delete deletes an issuer that has no documents and no certificates, with its establishments,
// emission points and issuer-scoped API keys. Otherwise the API answers 409 resource_in_use:
// certificates cannot be deleted, so an issuer that ever had a .p12 (one from an issuer
// onboarding too) stays.
func (s *IssuerService) Delete(ctx context.Context, issuerID string, options ...RequestOption) error {
	return remove(ctx, s.engine, options, "/v1/issuers/{}", issuerID)
}

// EstablishmentService manages the establishments (the 3-digit series codes such as 001) of an
// issuer. See client.Issuers.Establishments.
type EstablishmentService struct {
	engine *engine

	// EmissionPoints manages the emission points of an establishment.
	EmissionPoints *EmissionPointService
}

// Create creates an establishment of an issuer.
func (s *EstablishmentService) Create(ctx context.Context, issuerID string, body CreateEstablishment, options ...RequestOption) (*Establishment, error) {
	return create[Establishment](ctx, s.engine, options, body, "/v1/issuers/{}/establishments", issuerID)
}

func (s *EstablishmentService) lister(issuerID string, params *EstablishmentListParams, options []RequestOption) (*lister[*Establishment], error) {
	path, err := pathOf("/v1/issuers/{}/establishments", issuerID)
	if err != nil {
		return nil, err
	}
	return newLister(s.engine, path, params.values(), options, decodeStruct[Establishment]), nil
}

// List walks the establishments of an issuer, by code, reading the pages as it goes. params
// may be nil.
func (s *EstablishmentService) List(ctx context.Context, issuerID string, params *EstablishmentListParams, options ...RequestOption) iter.Seq2[*Establishment, error] {
	l, err := s.lister(issuerID, params, options)
	if err != nil {
		return failedList[*Establishment](err)
	}
	return l.all(ctx)
}

// ListPage reads the first page of an issuer's establishments, by code; NextPage reads the
// next one.
func (s *EstablishmentService) ListPage(ctx context.Context, issuerID string, params *EstablishmentListParams, options ...RequestOption) (*Page[*Establishment], error) {
	l, err := s.lister(issuerID, params, options)
	if err != nil {
		return nil, err
	}
	return l.page(ctx, "")
}

// Get retrieves an establishment.
func (s *EstablishmentService) Get(ctx context.Context, issuerID, establishmentID string, options ...RequestOption) (*Establishment, error) {
	return get[Establishment](ctx, s.engine, options, "/v1/issuers/{}/establishments/{}", issuerID, establishmentID)
}

// Update changes an establishment (only the members set are sent).
func (s *EstablishmentService) Update(ctx context.Context, issuerID, establishmentID string, body UpdateEstablishment, options ...RequestOption) (*Establishment, error) {
	return update[Establishment](ctx, s.engine, options, body, "/v1/issuers/{}/establishments/{}", issuerID, establishmentID)
}

// Delete deletes an establishment.
func (s *EstablishmentService) Delete(ctx context.Context, issuerID, establishmentID string, options ...RequestOption) error {
	return remove(ctx, s.engine, options, "/v1/issuers/{}/establishments/{}", issuerID, establishmentID)
}

// EmissionPointService manages the emission points (the second 3-digit series code) of an
// establishment. See client.Issuers.Establishments.EmissionPoints.
type EmissionPointService struct{ engine *engine }

// Create creates an emission point of an establishment.
func (s *EmissionPointService) Create(ctx context.Context, issuerID, establishmentID string, body CreateEmissionPoint, options ...RequestOption) (*EmissionPoint, error) {
	return create[EmissionPoint](ctx, s.engine, options, body, "/v1/issuers/{}/establishments/{}/emission-points", issuerID, establishmentID)
}

func (s *EmissionPointService) lister(issuerID, establishmentID string, params *EmissionPointListParams, options []RequestOption) (*lister[*EmissionPoint], error) {
	path, err := pathOf("/v1/issuers/{}/establishments/{}/emission-points", issuerID, establishmentID)
	if err != nil {
		return nil, err
	}
	return newLister(s.engine, path, params.values(), options, decodeStruct[EmissionPoint]), nil
}

// List walks the emission points of an establishment, by code, reading the pages as it goes.
// params may be nil.
func (s *EmissionPointService) List(ctx context.Context, issuerID, establishmentID string, params *EmissionPointListParams, options ...RequestOption) iter.Seq2[*EmissionPoint, error] {
	l, err := s.lister(issuerID, establishmentID, params, options)
	if err != nil {
		return failedList[*EmissionPoint](err)
	}
	return l.all(ctx)
}

// ListPage reads the first page of an establishment's emission points, by code; NextPage reads
// the next one.
func (s *EmissionPointService) ListPage(ctx context.Context, issuerID, establishmentID string, params *EmissionPointListParams, options ...RequestOption) (*Page[*EmissionPoint], error) {
	l, err := s.lister(issuerID, establishmentID, params, options)
	if err != nil {
		return nil, err
	}
	return l.page(ctx, "")
}

// Get retrieves an emission point.
func (s *EmissionPointService) Get(ctx context.Context, issuerID, establishmentID, emissionPointID string, options ...RequestOption) (*EmissionPoint, error) {
	return get[EmissionPoint](ctx, s.engine, options, "/v1/issuers/{}/establishments/{}/emission-points/{}", issuerID, establishmentID, emissionPointID)
}

// Delete deletes an emission point.
func (s *EmissionPointService) Delete(ctx context.Context, issuerID, establishmentID, emissionPointID string, options ...RequestOption) error {
	return remove(ctx, s.engine, options, "/v1/issuers/{}/establishments/{}/emission-points/{}", issuerID, establishmentID, emissionPointID)
}

// CertificateService manages the .p12 certificates (firmas electrónicas) of an issuer. See
// client.Issuers.Certificates.
type CertificateService struct{ engine *engine }

// Upload uploads a .p12 for the issuer, for the mode of the API key only (test or live). To
// renew the certificate of both modes, start an issuer onboarding with it instead.
//
// Flashpi checks it in the background: the certificate starts processing, then becomes
// accepted (event certificate.verified, or certificate.pending while its revocation status
// cannot be checked) or rejected (no event: follow it with Get and read Rejection).
func (s *CertificateService) Upload(ctx context.Context, issuerID string, upload CertificateUpload, options ...RequestOption) (*Certificate, error) {
	path, err := pathOf("/v1/issuers/{}/certificates", issuerID)
	if err != nil {
		return nil, err
	}
	op, err := certificateForm(upload)
	if err != nil {
		return nil, err
	}
	op.path = path
	return fetch[Certificate](ctx, s.engine, op, options)
}

func (s *CertificateService) lister(issuerID string, params *CertificateListParams, options []RequestOption) (*lister[*Certificate], error) {
	path, err := pathOf("/v1/issuers/{}/certificates", issuerID)
	if err != nil {
		return nil, err
	}
	return newLister(s.engine, path, params.values(), options, decodeStruct[Certificate]), nil
}

// List walks the certificates of an issuer, newest first, reading the pages as it goes. params
// may be nil.
func (s *CertificateService) List(ctx context.Context, issuerID string, params *CertificateListParams, options ...RequestOption) iter.Seq2[*Certificate, error] {
	l, err := s.lister(issuerID, params, options)
	if err != nil {
		return failedList[*Certificate](err)
	}
	return l.all(ctx)
}

// ListPage reads the first page of an issuer's certificates, newest first; NextPage reads the
// next one.
func (s *CertificateService) ListPage(ctx context.Context, issuerID string, params *CertificateListParams, options ...RequestOption) (*Page[*Certificate], error) {
	l, err := s.lister(issuerID, params, options)
	if err != nil {
		return nil, err
	}
	return l.page(ctx, "")
}

// Get retrieves a certificate.
func (s *CertificateService) Get(ctx context.Context, issuerID, certificateID string, options ...RequestOption) (*Certificate, error) {
	return get[Certificate](ctx, s.engine, options, "/v1/issuers/{}/certificates/{}", issuerID, certificateID)
}

// get reads a resource.
func get[T any](ctx context.Context, e *engine, options []RequestOption, template string, ids ...string) (*T, error) {
	path, err := pathOf(template, ids...)
	if err != nil {
		return nil, err
	}
	return fetch[T](ctx, e, &operation{method: http.MethodGet, path: path, accept: jsonAccept}, options)
}

// create POSTs a JSON body to a collection.
func create[T any](ctx context.Context, e *engine, options []RequestOption, body any, template string, ids ...string) (*T, error) {
	path, err := pathOf(template, ids...)
	if err != nil {
		return nil, err
	}
	op, err := jsonOperation(http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return fetch[T](ctx, e, op, options)
}

// update PATCHes a resource with a JSON body.
func update[T any](ctx context.Context, e *engine, options []RequestOption, body any, template string, ids ...string) (*T, error) {
	path, err := pathOf(template, ids...)
	if err != nil {
		return nil, err
	}
	op, err := jsonOperation(http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	return fetch[T](ctx, e, op, options)
}

// remove DELETEs a resource. A retry after a lost answer that finds it gone counts as done.
func remove(ctx context.Context, e *engine, options []RequestOption, template string, ids ...string) error {
	path, err := pathOf(template, ids...)
	if err != nil {
		return err
	}
	return e.do(ctx, &operation{method: http.MethodDelete, path: path, accept: jsonAccept}, options, nil)
}
