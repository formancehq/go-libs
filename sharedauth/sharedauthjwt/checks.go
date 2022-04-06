package sharedauthjwt

import (
	"github.com/golang-jwt/jwt"
	"github.com/pkg/errors"
	"net/http"
	"strings"
)

var (
	ErrAuthorizationHeaderNotFound = errors.New("missing_authorization_header")
	ErrAccessDenied                = errors.New("access_denied")
)

func CheckTokenWithAuth(client *http.Client, authBaseUrl string, req *http.Request) error {
	token := req.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return ErrAuthorizationHeaderNotFound
	}
	authUrl := authBaseUrl + "/authenticate/check"
	authRequest, err := http.NewRequest("GET", authUrl, nil)
	if err != nil {
		return errors.Wrap(err, "building request")
	}
	authRequest = authRequest.WithContext(req.Context())
	authRequest.Header.Add("Authorization", token)
	response, err := client.Do(authRequest)
	if err != nil {
		return errors.Wrap(err, "doing request")
	}
	if response.StatusCode != 200 {
		return ErrAccessDenied
	}
	return nil
}

func CheckLedgerAccess(req *http.Request, name string) error {
	jwtString := req.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(jwtString), "bearer ") {
		return ErrAuthorizationHeaderNotFound
	}
	tokenString := jwtString[len("bearer "):]

	payload, _, err := new(jwt.Parser).ParseUnverified(tokenString, &ClaimStruct{})
	if err != nil {
		return errors.Wrap(err, "parsing jwt token")
	}
	for _, s := range payload.Claims.(*ClaimStruct).Organizations {
		for _, l := range s.Ledgers {
			if l.Slug == name {
				return nil
			}
		}
	}
	return ErrAccessDenied
}

func CheckOrganizationAccess(req *http.Request, id string) error {
	jwtString := req.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(jwtString), "bearer ") {
		return ErrAuthorizationHeaderNotFound
	}
	tokenString := jwtString[len("bearer "):]

	payload, _, err := new(jwt.Parser).ParseUnverified(tokenString, &ClaimStruct{})
	if err != nil {
		return errors.Wrap(err, "parsing jwt token")
	}
	for _, s := range payload.Claims.(*ClaimStruct).Organizations {
		if s.ID == id {
			return nil
		}
	}
	return ErrAccessDenied
}
