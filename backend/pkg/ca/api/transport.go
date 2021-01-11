package api

import (
	"context"
	"encoding/json"
	"enroller/pkg/enroller/auth"
	"errors"
	"net/http"

	"github.com/go-kit/kit/log"

	stdjwt "github.com/dgrijalva/jwt-go"
	"github.com/go-kit/kit/auth/jwt"
	"github.com/go-kit/kit/transport"
	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gorilla/mux"
)

type errorer interface {
	error() error
}

var (
	errCAName = errors.New("ca name not provided")
)

var claims = &auth.KeycloakClaims{}

func MakeHTTPHandler(s Service, logger log.Logger, auth auth.Auth) http.Handler {
	r := mux.NewRouter()
	e := MakeServerEndpoints(s)
	options := []httptransport.ServerOption{
		httptransport.ServerErrorHandler(transport.NewLogErrorHandler(logger)),
		httptransport.ServerErrorEncoder(encodeError),
		httptransport.ServerBefore(jwt.HTTPToContext()),
	}

	r.Methods("GET").Path("/v1/health").Handler(httptransport.NewServer(
		e.HealthEndpoint,
		decodeHealthRequest,
		encodeResponse,
		options...,
	))

	r.Methods("GET").Path("/v1/cas").Handler(httptransport.NewServer(
		jwt.NewParser(auth.Kf, stdjwt.SigningMethodRS256, auth.KeycloakClaimsFactory)(e.GetCAsEndpoint),
		decodeGetCAsRequest,
		encodeResponse,
		options...,
	))

	r.Methods("GET").Path("/v1/cas/{ca}").Handler(httptransport.NewServer(
		jwt.NewParser(auth.Kf, stdjwt.SigningMethodRS256, auth.KeycloakClaimsFactory)(e.GetCAInfoEndpoint),
		decodeGetCAInfoRequest,
		encodeResponse,
		options...,
	))

	r.Methods("DELETE").Path("/v1/cas/{ca}").Handler(httptransport.NewServer(
		jwt.NewParser(auth.Kf, stdjwt.SigningMethodRS256, auth.KeycloakClaimsFactory)(e.DeleteCAEndpoint),
		decodeDeleteCARequest,
		encodeResponse,
		options...,
	))

	return r
}

func decodeHealthRequest(ctx context.Context, r *http.Request) (request interface{}, err error) {
	var req healthRequest
	return req, nil
}

func decodeGetCAsRequest(ctx context.Context, r *http.Request) (request interface{}, err error) {
	var req getCAsRequest
	return req, nil
}

func decodeGetCAInfoRequest(ctx context.Context, r *http.Request) (request interface{}, err error) {
	vars := mux.Vars(r)
	CA, ok := vars["ca"]
	if !ok {
		return nil, errCAName
	}
	return getCAInfoRequest{CA: CA}, nil
}

func decodeDeleteCARequest(ctx context.Context, r *http.Request) (request interface{}, err error) {
	vars := mux.Vars(r)
	CA, ok := vars["ca"]
	if !ok {
		return nil, errCAName
	}
	return deleteCARequest{CA: CA}, nil
}

func encodeResponse(ctx context.Context, w http.ResponseWriter, response interface{}) error {
	if e, ok := response.(errorer); ok && e.error() != nil {
		// Not a Go kit transport error, but a business-logic error.
		// Provide those as HTTP errors.
		encodeError(ctx, e.error(), w)

		return nil
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	return json.NewEncoder(w).Encode(response)
}

func encodeError(_ context.Context, err error, w http.ResponseWriter) {
	if err == nil {
		panic("encodeError with nil error")
	}
	http.Error(w, err.Error(), codeFrom(err))
}

func codeFrom(err error) int {
	switch err {
	case errCAName:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}
